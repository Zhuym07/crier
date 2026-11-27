package crier

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTelegramReceiptsAndTodoRetry(t *testing.T) {
	s := &Service{dir: t.TempDir(), queue: queue{Items: []queueItem{{Entry: Entry{ID: "task", Title: "交作业", State: "submitted"}, TelegramBot: "telegram:1", TelegramChat: 42, TodoText: "重要且紧急\n• 检查附件"}}}}
	var messages []string
	rejectChannel := true
	send := func(chat int64, text string) error {
		if chat == -100 && rejectChannel {
			return errors.New("channel unavailable")
		}
		messages = append(messages, text)
		return nil
	}
	flush := func() error { return s.flushTelegramReceipts(context.Background(), "telegram:1", -100, send) }
	if err := flush(); err == nil {
		t.Fatal("expected channel error")
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "已提交到打印队列") {
		t.Fatalf("unexpected messages: %v", messages)
	}
	rejectChannel = false
	if err := flush(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || !strings.Contains(messages[1], "检查附件") {
		t.Fatalf("unexpected retry: %v", messages)
	}
	s.queue.Items[0].State = "printed"
	if err := flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Acknowledge("task"); err != nil {
		t.Fatal(err)
	}
	if err := flush(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || !strings.Contains(messages[2], "打印驱动报告已打印") || !strings.Contains(messages[3], "确认收到") {
		t.Fatalf("unexpected transitions: %v", messages)
	}
}
