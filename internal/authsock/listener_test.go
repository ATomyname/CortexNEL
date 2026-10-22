package authsock

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

type testLogger struct{}

func (testLogger) Info(string, string, ...any) {}
func (testLogger) Warn(string, string, ...any) {}

func writePrefixedString(w io.Writer, s string) error {
	raw := []byte(s)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(raw)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(raw)
	return err
}

func TestAuthSockSuccess(t *testing.T) {
	var capturedGameID, capturedUserID, capturedHash string
	joinFn := func(ctx context.Context, gameID, userID, hash string) error {
		capturedGameID = gameID
		capturedUserID = userID
		capturedHash = hash
		return nil
	}

	listener, err := Listen(joinFn, testLogger{})
	if err != nil {
		t.Fatalf("Listen 失败: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.inner.Addr().String())
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer conn.Close()

	// 依次写入 gameID, userID, serverHash
	if err := writePrefixedString(conn, "4688093584228529760"); err != nil {
		t.Fatal(err)
	}
	if err := writePrefixedString(conn, "12345678"); err != nil {
		t.Fatal(err)
	}
	if err := writePrefixedString(conn, "387c2b627d383b772f7a552bb7a8a10123456789"); err != nil {
		t.Fatal(err)
	}

	// 读响应码 (uint32)
	var respBuf [4]byte
	if _, err := io.ReadFull(conn, respBuf[:]); err != nil {
		t.Fatalf("读取响应码失败: %v", err)
	}
	code := binary.LittleEndian.Uint32(respBuf[:])
	if code != 0 {
		t.Fatalf("期望响应码 0，实得 %d", code)
	}

	if capturedGameID != "4688093584228529760" {
		t.Errorf("gameID = %s, 期望 4688093584228529760", capturedGameID)
	}
	if capturedUserID != "12345678" {
		t.Errorf("userID = %s, 期望 12345678", capturedUserID)
	}
	if capturedHash != "387c2b627d383b772f7a552bb7a8a10123456789" {
		t.Errorf("serverHash = %s, 期望 387c2b627d383b772f7a552bb7a8a10123456789", capturedHash)
	}
}

func TestAuthSockFailure(t *testing.T) {
	joinFn := func(ctx context.Context, gameID, userID, hash string) error {
		return errors.New("upstream failed")
	}

	listener, err := Listen(joinFn, testLogger{})
	if err != nil {
		t.Fatalf("Listen 失败: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.inner.Addr().String())
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer conn.Close()

	_ = writePrefixedString(conn, "123")
	_ = writePrefixedString(conn, "456")
	_ = writePrefixedString(conn, "abc")

	var respBuf [4]byte
	if _, err := io.ReadFull(conn, respBuf[:]); err != nil {
		t.Fatalf("读取响应码失败: %v", err)
	}
	code := binary.LittleEndian.Uint32(respBuf[:])
	if code != 1 {
		t.Fatalf("期望响应码 1，实得 %d", code)
	}
}
