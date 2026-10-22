package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerRedactsSensitiveKeys(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus()
	logger, err := New(bus, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	var eventMessage string
	bus.Subscribe("telemetry.log", func(e Event) {
		eventMessage, _ = e.Fields["message"].(string)
	})

	logger.Info("test", "登录完成", "userToken", "SUPERSECRET", "accountRef", "user-1")

	// 日志文件里不得出现明文敏感值（规范 S3）。
	logDir := filepath.Join(dir, "logs")
	entries, _ := os.ReadDir(logDir)
	var content strings.Builder
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
		content.Write(data)
	}
	if strings.Contains(content.String(), "SUPERSECRET") {
		t.Fatal("敏感值泄漏进日志文件")
	}
	if eventMessage != "登录完成" {
		t.Fatalf("telemetry.log message = %q", eventMessage)
	}
}

func TestLoggerMinLevelSuppressesDebug(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(NewBus(), dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.SetMinLevel(LevelInfo)
	logger.Debug("test", "不应出现")

	logDir := filepath.Join(dir, "logs")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 0 {
		t.Fatalf("debug 仍被写入: %d 个文件", len(entries))
	}
}

// TestLoggerCloseIsTerminal Close 之后的写不得重新打开日志文件。
//
// 这是实测故障：调用方按「先 Close 再清理日志目录」收尾，其间只要有后台协程写一条
// 日志，writeLine 就把已关闭的文件重新打开，随后 RemoveAll 撞上「文件被占用」。
// 契约：Close 后只丢落盘，事件照发（沿用「落盘失败降级为事件发布」的语义）。
func TestLoggerCloseIsTerminal(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus()
	logger, err := New(bus, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var gotMessage string
	bus.Subscribe("telemetry.log", func(e Event) {
		gotMessage, _ = e.Fields["message"].(string)
	})

	logDir := filepath.Join(dir, "logs")
	logPath := filepath.Join(logDir, time.Now().Format("2006-01-02")+".jsonl")

	logger.Info("test", "关闭前")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	logger.Info("test", "关闭后")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "关闭后") {
		t.Fatal("Close 之后又写了日志文件：Close 不是终态")
	}
	if !strings.Contains(string(data), "关闭前") {
		t.Fatal("Close 前的内容应保留")
	}
	if gotMessage != "关闭后" {
		t.Fatalf("Close 后事件仍应照发，实得 %q", gotMessage)
	}

	// 真正的回归点：日志目录必须能被删掉，说明没有句柄复活。
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("Close 后日志目录删不掉，句柄被复活: %v", err)
	}
}
