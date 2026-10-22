// 结构化日志：JSONL 按日轮转 + 脱敏（规范 S3），并发布 telemetry.log 事件供 UI 消费。
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level 是日志级别，与前端 LogEntry 的 level 字段一致。
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// sensitiveKeySubstrings 命中即脱敏（规范 S3）。
var sensitiveKeySubstrings = []string{"token", "password", "passwd", "secret", "credential", "cookie"}

// Logger 写 JSONL 日志并把每行发布为 telemetry.log 事件。
type Logger struct {
	bus *Bus
	dir string

	mu       sync.Mutex
	day      string
	file     *os.File
	closed   bool
	minLevel Level
}

// LogEntry 是 telemetry.log 事件的载荷（前端 bus.LogEntry 同形）。
type LogEntry struct {
	TS      string `json:"ts"`
	Level   Level  `json:"level"`
	Module  string `json:"module"`
	Message string `json:"message"`
}

// New 在 dir 下建 logs/ 目录并构造 Logger。
func New(bus *Bus, dir string) (*Logger, error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	return &Logger{bus: bus, dir: logDir, minLevel: LevelDebug}, nil
}

// SetMinLevel 设置落盘与发布的最低级别。
func (l *Logger) SetMinLevel(level Level) {
	l.mu.Lock()
	l.minLevel = level
	l.mu.Unlock()
}

// Info / Warn / Error / Debug 是按级别的便捷入口。kv 为交替的 key,value 序列。
func (l *Logger) Info(module, message string, kv ...any)  { l.Log(LevelInfo, module, message, kv...) }
func (l *Logger) Warn(module, message string, kv ...any)  { l.Log(LevelWarn, module, message, kv...) }
func (l *Logger) Error(module, message string, kv ...any) { l.Log(LevelError, module, message, kv...) }
func (l *Logger) Debug(module, message string, kv ...any) { l.Log(LevelDebug, module, message, kv...) }

// Log 记录一条结构化日志：脱敏 → 落盘 → 发布 telemetry.log。
func (l *Logger) Log(level Level, module, message string, kv ...any) {
	l.mu.Lock()
	minLevel := l.minLevel
	l.mu.Unlock()
	if levelRank(level) < levelRank(minLevel) {
		return
	}

	entry := LogEntry{
		TS:      time.Now().Format(time.RFC3339Nano),
		Level:   level,
		Module:  module,
		Message: message,
	}

	line := map[string]any{
		"ts": entry.TS, "level": string(level), "module": module, "message": message,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		line[key] = redactValue(key, kv[i+1])
	}

	if err := l.writeLine(line); err != nil {
		// 落盘失败不阻断主流程，只降级为事件发布。
		entry.Message = message + "（日志落盘失败: " + err.Error() + "）"
	}

	if l.bus != nil {
		l.bus.Publish("telemetry.log", string(level), map[string]any{
			"ts": entry.TS, "level": string(level), "module": module, "message": entry.Message,
		})
	}
}

func (l *Logger) writeLine(line map[string]any) error {
	day := time.Now().Format("2006-01-02")
	l.mu.Lock()
	defer l.mu.Unlock()
	// Close 是终态：晚到的写只丢弃落盘，不得重新打开文件。否则关闭后仍有协程
	// 写日志时句柄会复活，调用方随后清理日志目录必然撞上「文件被占用」。
	if l.closed {
		return nil
	}
	if l.file == nil || l.day != day {
		if l.file != nil {
			_ = l.file.Close()
		}
		f, err := os.OpenFile(filepath.Join(l.dir, day+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		l.file = f
		l.day = day
	}
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = l.file.Write(data)
	return err
}

// Close 关闭当前日志文件。Close 之后再写不会重新打开文件（见 writeLine）。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func levelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 0
	case LevelInfo:
		return 1
	case LevelWarn:
		return 2
	case LevelError:
		return 3
	default:
		return 1
	}
}

func redactValue(key string, value any) any {
	lower := strings.ToLower(key)
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(lower, sub) {
			return "***"
		}
	}
	return value
}
