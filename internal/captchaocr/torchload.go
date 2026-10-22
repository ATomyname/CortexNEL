// Minimal reader for PyTorch .pth state_dict files (zip + pickle
// protocol 2, as written by torch.save). Implements just enough of the
// pickle VM to rebuild tensors: GLOBAL / REDUCE / BINPERSID / memo /
// mark-based tuples & dicts. No third-party dependencies.
package captchaocr

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Tensor is one rebuilt state_dict entry (raw storage bytes view).
type Tensor struct {
	Shape  []int
	Stride []int
	data   []byte // little-endian float32 (or int64) elements
	Elem   int    // element size in bytes
}

// Floats returns the tensor as float32 slice (row-major, contiguous).
func (t *Tensor) Floats() ([]float32, error) {
	if t.Elem != 4 {
		return nil, fmt.Errorf("tensor elem size %d != float32", t.Elem)
	}
	return bytesToFloat32(t.data), nil
}

func bytesToFloat32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		u := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(u)
	}
	return out
}

// LoadStateDict reads a torch.save archive and returns all tensors by name.
func LoadStateDict(path string) (map[string]*Tensor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadStateDictBytes(b)
}

// LoadStateDictBytes parses an in-memory torch.save archive.
func LoadStateDictBytes(b []byte) (map[string]*Tensor, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}

	var pkl *zip.File
	stores := map[string][]byte{}
	for _, f := range zr.File {
		switch {
		case strings.HasSuffix(f.Name, "data.pkl"):
			pkl = f
		case strings.Contains(f.Name, "/data/"):
			key := f.Name[strings.LastIndex(f.Name, "/")+1:]
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			stores[key] = b
		}
	}
	if pkl == nil {
		return nil, fmt.Errorf("no data.pkl in archive")
	}
	rc, err := pkl.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	p, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	vm := &pickleVM{stores: stores}
	root, err := vm.run(p)
	if err != nil {
		return nil, err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("root is %T, want dict", root)
	}
	out := map[string]*Tensor{}
	for k, v := range dict {
		t, ok := v.(*Tensor)
		if !ok {
			continue // num_batches_tracked etc. arrive as LongStorage ints
		}
		out[k] = t
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tensors parsed from archive")
	}
	return out, nil
}

// ---- pickle VM ----

const (
	opProto          = 0x80
	opNewObj         = 0x81 // unused, error loudly
	opGlobal         = 'c'
	opReduce         = 'R'
	opBinPersid      = 'Q'
	opEmptyTuple     = ')'
	opEmptyList      = ']'
	opEmptyDict      = '}'
	opMark           = '('
	opBinUnicode     = 'X'
	opShortUnicode   = 0x8c
	opBinInt1        = 'K'
	opBinInt2        = 'M'
	opBinInt4        = 'J'
	opLong1          = 0x8a
	opNone           = 'N'
	opNewTrue        = 0x88
	opNewFalse       = 0x89
	opTuple1         = 0x85
	opTuple2         = 0x86
	opTuple3         = 0x87
	opTupleAll       = 't'
	opSetItems       = 'u'
	opSetItem        = 's'
	opAppends        = 'e'
	opBInput         = 'q'
	opLInput         = 'r' // LONG_BINPUT
	opBinget         = 'h'
	opLongBinget     = 'j'
	opStop           = '.'
	opFrame          = 0x95
	opMemoize        = 0x94
	opStackGlobal    = 0x93
	opBuild          = 'b'
	opShortBinBytes  = 'C'
	opBinBytes       = 'B'
	opPopMark        = '1'
	opAddItems       = 0x6e
	opShortBinString = 'U'
)

type markType struct{}

var mark markType

type pickleVM struct {
	stack   []any
	memo    map[uint64]any
	memoLen uint64 // CPython-style sequential put counter
	stores  map[string][]byte
}

type globalRef struct{ module, name string }

type storageRef struct {
	key  string
	elem int
}

func (vm *pickleVM) run(p []byte) (any, error) {
	vm.memo = map[uint64]any{}
	pos := 0
	read := func(n int) ([]byte, error) {
		if pos+n > len(p) {
			return nil, fmt.Errorf("truncated at %d (+%d)", pos, n)
		}
		b := p[pos : pos+n]
		pos += n
		return b, nil
	}
	push := func(v any) { vm.stack = append(vm.stack, v) }
	pop := func() (any, error) {
		if len(vm.stack) == 0 {
			return nil, fmt.Errorf("stack underflow")
		}
		v := vm.stack[len(vm.stack)-1]
		vm.stack = vm.stack[:len(vm.stack)-1]
		return v, nil
	}
	markPos := func() (int, error) {
		for i := len(vm.stack) - 1; i >= 0; i-- {
			if _, ok := vm.stack[i].(markType); ok {
				return i, nil
			}
		}
		return -1, fmt.Errorf("no MARK on stack")
	}
	setMemo := func(idx uint64) {
		if n := len(vm.stack); n > 0 {
			vm.memo[idx] = vm.stack[n-1]
			if idx+1 > vm.memoLen {
				vm.memoLen = idx + 1
			}
		}
	}

	for pos < len(p) {
		op := p[pos]
		pos++
		switch op {
		case opProto, opFrame:
			pos++ // skip arg byte(s)
		case opGlobal:
			line1, err := readLine(p, &pos)
			if err != nil {
				return nil, err
			}
			line2, err := readLine(p, &pos)
			if err != nil {
				return nil, err
			}
			push(globalRef{module: line1, name: line2})
		case opStackGlobal:
			name, err := pop()
			if err != nil {
				return nil, err
			}
			mod, err := pop()
			if err != nil {
				return nil, err
			}
			push(globalRef{module: mod.(string), name: name.(string)})
		case opReduce:
			argsV, err := pop()
			if err != nil {
				return nil, err
			}
			fnV, err := pop()
			if err != nil {
				return nil, err
			}
			fn := fnV.(globalRef)
			args := argsV.([]any)
			v, err := vm.reduce(fn, args)
			if err != nil {
				return nil, err
			}
			push(v)
		case opBinPersid:
			pid, err := pop()
			if err != nil {
				return nil, err
			}
			v, err := vm.persistentLoad(pid.([]any))
			if err != nil {
				return nil, err
			}
			push(v)
		case opMark:
			push(mark)
		case opEmptyTuple:
			push([]any{})
		case opTuple1:
			a, _ := pop()
			push([]any{a})
		case opTuple2:
			b, _ := pop()
			a, _ := pop()
			push([]any{a, b})
		case opTuple3:
			c, _ := pop()
			b, _ := pop()
			a, _ := pop()
			push([]any{a, b, c})
		case opTupleAll:
			i, err := markPos()
			if err != nil {
				return nil, err
			}
			items := append([]any{}, vm.stack[i+1:]...)
			vm.stack = vm.stack[:i]
			push(items)
		case opEmptyDict:
			push(map[string]any{})
		case opEmptyList:
			push([]any{})
		case opSetItems:
			i, err := markPos()
			if err != nil {
				return nil, err
			}
			items := vm.stack[i+1:]
			dict := vm.stack[i-1].(map[string]any)
			for j := 0; j+1 < len(items); j += 2 {
				dict[items[j].(string)] = items[j+1]
			}
			vm.stack = vm.stack[:i]
		case opSetItem:
			// SETITEM: value = pop, key = pop; dict is new top of stack.
			v, err := pop()
			if err != nil {
				return nil, err
			}
			k, err := pop()
			if err != nil {
				return nil, err
			}
			if len(vm.stack) == 0 {
				return nil, fmt.Errorf("SETITEM with empty stack")
			}
			dict, ok := vm.stack[len(vm.stack)-1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("SETITEM target %T", vm.stack[len(vm.stack)-1])
			}
			dict[k.(string)] = v
		case opAppends:
			i, err := markPos()
			if err != nil {
				return nil, err
			}
			list := vm.stack[i-1].([]any)
			list = append(list, vm.stack[i+1:]...)
			vm.stack[i-1] = list
			vm.stack = vm.stack[:i]
		case opAddItems:
			i, err := markPos()
			if err != nil {
				return nil, err
			}
			items := vm.stack[i+1:]
			dict := vm.stack[i-1].(map[string]any)
			for _, it := range items {
				kv := it.([]any)
				dict[kv[0].(string)] = kv[1]
			}
			vm.stack = vm.stack[:i]
		case opBuild:
			state, _ := pop()
			objV, _ := pop()
			switch o := objV.(type) {
			case map[string]any:
				if st, ok := state.(map[string]any); ok {
					for k, v := range st {
						o[k] = v
					}
				}
				push(o)
			default:
				push(objV)
			}
		case opBinUnicode:
			nb, err := read(4)
			if err != nil {
				return nil, err
			}
			s, err := read(int(binary.LittleEndian.Uint32(nb)))
			if err != nil {
				return nil, err
			}
			push(string(s))
		case opShortUnicode, opShortBinString:
			n := int(p[pos])
			pos++
			s, err := read(n)
			if err != nil {
				return nil, err
			}
			push(string(s))
		case opBinBytes:
			nb, err := read(4)
			if err != nil {
				return nil, err
			}
			s, err := read(int(binary.LittleEndian.Uint32(nb)))
			if err != nil {
				return nil, err
			}
			push(string(s))
		case opBinInt1:
			push(int(p[pos]))
			pos++
		case opBinInt2:
			push(int(binary.LittleEndian.Uint16(p[pos:])))
			pos += 2
		case opBinInt4:
			push(int(int32(binary.LittleEndian.Uint32(p[pos:]))))
			pos += 4
		case opLong1:
			n := int(p[pos])
			pos++
			b, err := read(n)
			if err != nil {
				return nil, err
			}
			var v int64
			for i := 0; i < n && i < 8; i++ {
				v |= int64(b[i]) << (8 * i)
			}
			push(int(v))
		case opNone:
			push(nil)
		case opNewTrue:
			push(true)
		case opNewFalse:
			push(false)
		case opBInput:
			setMemo(uint64(p[pos]))
			pos++
		case opLInput:
			setMemo(uint64(binary.LittleEndian.Uint32(p[pos:])))
			pos += 4
		case opBinget:
			push(vm.memo[uint64(p[pos])])
			pos++
		case opLongBinget:
			idx := binary.LittleEndian.Uint32(p[pos:])
			pos += 4
			push(vm.memo[uint64(idx)])
		case opMemoize:
			if n := len(vm.stack); n > 0 {
				vm.memo[vm.memoLen] = vm.stack[n-1]
				vm.memoLen++
			}
		case opPopMark:
			i, err := markPos()
			if err != nil {
				return nil, err
			}
			items := append([]any{}, vm.stack[i+1:]...)
			vm.stack = vm.stack[:i]
			push(items)
		case opStop:
			return pop()
		default:
			return nil, fmt.Errorf("unsupported opcode %#x at %d", op, pos-1)
		}
	}
	return nil, fmt.Errorf("stream ended without STOP")
}

// memoSeq tracks MEMOIZE indices exactly (sequential per stream).
func (vm *pickleVM) reduce(fn globalRef, args []any) (any, error) {
	switch fn.name {
	case "_rebuild_tensor_v2", "_rebuild_tensor":
		return vm.rebuildTensor(args)
	case "OrderedDict":
		return map[string]any{}, nil
	case "FloatStorage":
		return storageRef{elem: 4}, nil
	case "LongStorage":
		return storageRef{elem: 8}, nil
	default:
		return map[string]any{}, nil // generic object placeholder
	}
}

func (vm *pickleVM) persistentLoad(pid []any) (any, error) {
	if len(pid) >= 3 {
		if kind, _ := pid[0].(string); kind == "storage" {
			elem := 0
			switch cls := pid[1].(type) {
			case storageRef:
				elem = cls.elem // already reduced (defensive)
			case globalRef:
				switch cls.name {
				case "FloatStorage", "HalfStorage", "BFloat16Storage":
					elem = 4
				case "LongStorage", "DoubleStorage":
					elem = 8
				default:
					return nil, fmt.Errorf("unsupported storage type %s", cls.name)
				}
			default:
				return nil, fmt.Errorf("pid[1] is %T", pid[1])
			}
			key, _ := pid[2].(string)
			return storageRef{key: key, elem: elem}, nil
		}
	}
	return nil, fmt.Errorf("unsupported persistent id %v", pid)
}

func (vm *pickleVM) rebuildTensor(args []any) (any, error) {
	st, ok := args[0].(storageRef)
	if !ok {
		return nil, fmt.Errorf("_rebuild_tensor_v2 arg0 = %T", args[0])
	}
	raw, ok := vm.stores[st.key]
	if !ok {
		return nil, fmt.Errorf("missing storage data/%s", st.key)
	}
	offset := asInt(args[1])
	size := asInts(args[2])
	stride := asInts(args[3])

	t := &Tensor{Shape: size, Stride: stride, Elem: st.elem, data: raw}
	// materialize contiguous row-major view honoring offset+stride
	total := 1
	for _, d := range size {
		total *= d
	}
	out := make([]byte, total*st.elem)
	copyContiguous(out, raw, offset, size, stride, st.elem)
	t.data = out
	t.Stride = rowMajor(size)
	return t, nil
}

func copyContiguous(dst, src []byte, srcElemOff int, size, stride []int, elem int) {
	// All indices below are in ELEMENT units; only the leaf converts
	// to a byte slice.
	var walk func(dim, dstOff, srcIdx int)
	walk = func(dim, dstOff, srcIdx int) {
		if dim == len(size) {
			s := srcIdx * elem
			copy(dst[dstOff*elem:(dstOff+1)*elem], src[s:s+elem])
			return
		}
		block := 1
		for _, d := range size[dim+1:] {
			block *= d
		}
		for i := 0; i < size[dim]; i++ {
			walk(dim+1, dstOff+i*block, srcIdx+i*stride[dim])
		}
	}
	walk(0, 0, srcElemOff)
}

func rowMajor(size []int) []int {
	st := make([]int, len(size))
	acc := 1
	for i := len(size) - 1; i >= 0; i-- {
		st[i] = acc
		acc *= size[i]
	}
	return st
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case []any:
		if len(x) == 1 {
			return asInt(x[0])
		}
	}
	return 0
}

func asInts(v any) []int {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int, len(l))
	for i, e := range l {
		out[i] = asInt(e)
	}
	return out
}

func readLine(p []byte, pos *int) (string, error) {
	if *pos >= len(p) {
		return "", fmt.Errorf("readLine EOF")
	}
	end := bytes.IndexByte(p[*pos:], '\n')
	if end < 0 {
		return "", fmt.Errorf("readLine no newline")
	}
	s := strings.TrimRight(string(p[*pos:*pos+end]), "\r")
	*pos += end + 1
	return s, nil
}
