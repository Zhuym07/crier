package crier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodoTicketWithOptionalAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"summary\":\"重要且紧急\",\"todo_items\":[\"核对材料\",\"提交申请\"]}"}}]}`)
	}))
	defer server.Close()
	for _, tt := range []struct {
		name    string
		enabled bool
		want    []string
	}{
		{"enabled", true, []string{"重要且紧急", "核对材料", "提交申请"}},
		{"disabled", false, []string{"今天提交申请"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := event{Template: "todo", AI: "todo", Body: "今天提交申请"}
			layout, err := prepare(context.Background(), e, AIConfig{Enabled: tt.enabled, URL: server.URL, Model: "local"}, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(layout.Blocks) != len(tt.want)+1 || layout.Blocks[0].Title != "待办清单" {
				t.Fatalf("unexpected layout: %+v", layout)
			}
			for i, text := range tt.want {
				if !strings.Contains(layout.Blocks[i+1].Text, text) {
					t.Fatalf("block %d: %q", i, layout.Blocks[i+1].Text)
				}
			}
			if _, err := printData(layout); err != nil {
				t.Fatal(err)
			}
		})
	}
}
