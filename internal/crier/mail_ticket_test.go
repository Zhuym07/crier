package crier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zhuym07/crier/internal/ticket"
)

func TestMailTicketHTTPAndStructuredFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %s", r.Method)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"type\":\"code\",\"code_val\":\"123456\",\"summary\":\"omit me\"}"}}]}`)
	}))
	defer server.Close()
	e := event{Template: "mail", AI: "classify_summarize", Title: "登录", Body: "例如 123456", Fields: map[string]string{"sender": "发送者"}}
	layout, err := prepare(context.Background(), e, AIConfig{Enabled: true, URL: server.URL, Model: "local"}, true)
	if err != nil {
		t.Fatal(err)
	}
	header := layout.Blocks[0]
	if header.Title != "验证码" || header.Subtitle != "发送者" || header.Icon != "code" || header.Meta != "" {
		t.Fatalf("unexpected header: %+v", header)
	}
	if len(layout.Blocks) != 3 || layout.Blocks[2].Style != "large" || !strings.Contains(layout.Blocks[2].Text, "123456") {
		t.Fatalf("unexpected layout: %+v", layout)
	}
	if _, err := printData(layout); err != nil {
		t.Fatal(err)
	}
}

func TestMailTicketHeaderTimeAndEventFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"type\":\"event\",\"summary\":\"请按时参加\",\"time\":\"明天 14:00\",\"location\":\"B203\"}"}}]}`)
	}))
	defer server.Close()
	when := time.Date(2026, 11, 27, 10, 33, 0, 0, time.Local)
	e := event{Template: "mail", AI: "classify_summarize", Title: "产品评审会议", Body: "明天下午两点 B203", Time: when, Fields: map[string]string{"sender": "alice@example.com"}}
	layout, err := prepare(context.Background(), e, AIConfig{Enabled: true, URL: server.URL, Model: "local"}, true)
	if err != nil {
		t.Fatal(err)
	}
	header := layout.Blocks[0]
	if header.Title != "日程提醒" || header.Icon != "event" || header.Meta != "11-27 10:33" || header.Subtitle != "alice@example.com" {
		t.Fatalf("unexpected header: %+v", header)
	}
	if layout.Blocks[1].Style != "bold" || layout.Blocks[1].Text != "产品评审会议" {
		t.Fatalf("subject block = %+v", layout.Blocks[1])
	}
	var fields []string
	for _, b := range layout.Blocks {
		fields = append(fields, b.Text)
	}
	joined := strings.Join(fields, "\n")
	if !strings.Contains(joined, "时间  明天 14:00") || !strings.Contains(joined, "地点  B203") {
		t.Fatalf("fields missing: %q", joined)
	}
	if _, err := printData(layout); err != nil {
		t.Fatal(err)
	}
}

func TestMailCategoriesAllCarryIconAndLabel(t *testing.T) {
	current := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, _ := json.Marshal(map[string]string{"type": current, "summary": "摘要内容"})
		reply, _ := json.Marshal(map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"content": string(answer)}},
		}})
		w.Write(reply)
	}))
	defer server.Close()

	names := []string{"event", "deadline", "submission", "notice", "code", "todo", "bill", "ad", "general"}
	for _, name := range names {
		current = name
		e := event{Template: "mail", AI: "classify_summarize", Title: "标题", Body: "正文", Fields: map[string]string{"sender": "发送者"}}
		layout, err := prepare(context.Background(), e, AIConfig{Enabled: true, URL: server.URL, Model: "local"}, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		header := layout.Blocks[0]
		if header.Icon != name || header.Title != mailLabels[name] {
			t.Fatalf("%s: header = %+v", name, header)
		}
		// Every category must render, including its icon, on the 58 mm head.
		canvas, err := ticket.Render(layout, ticket.DefaultPaper())
		if err != nil {
			t.Fatalf("%s: render: %v", name, err)
		}
		png, err := canvas.PNG()
		if err != nil || len(png) == 0 {
			t.Fatalf("%s: empty preview: %v", name, err)
		}
	}
}

func TestMailTicketFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) }))
	defer server.Close()
	layout, err := prepare(context.Background(), event{Template: "mail", AI: "classify_summarize", Title: "会议", Body: "会议改到下午"}, AIConfig{Enabled: true, URL: server.URL, Model: "local"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Blocks[0].Icon != "general" || layout.Blocks[0].Title != "邮件摘要" {
		t.Fatalf("fallback header = %+v", layout.Blocks[0])
	}
	text := layout.Blocks[len(layout.Blocks)-1].Text
	if !strings.Contains(text, "AI 分析失败") || !strings.Contains(text, "会议改到下午") {
		t.Fatal(text)
	}
}
