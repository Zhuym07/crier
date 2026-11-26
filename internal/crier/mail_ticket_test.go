package crier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if layout.Blocks[0].Title != "验证码" || layout.Blocks[0].Subtitle != "发送者" || len(layout.Blocks) != 3 || layout.Blocks[2].Style != "large" || !strings.Contains(layout.Blocks[2].Text, "123456") {
		t.Fatalf("unexpected layout: %+v", layout)
	}
	if _, err := printData(layout); err != nil {
		t.Fatal(err)
	}
}

func TestMailTicketFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) }))
	defer server.Close()
	layout, err := prepare(context.Background(), event{Template: "mail", AI: "classify_summarize", Title: "会议", Body: "会议改到下午"}, AIConfig{Enabled: true, URL: server.URL, Model: "local"}, true)
	if err != nil {
		t.Fatal(err)
	}
	text := layout.Blocks[len(layout.Blocks)-1].Text
	if !strings.Contains(text, "AI 分析失败") || !strings.Contains(text, "会议改到下午") {
		t.Fatal(text)
	}
}
