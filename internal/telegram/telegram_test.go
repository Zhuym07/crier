package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zhuym07/crier/internal/ticket"
)

func TestCursorQuotaVIPAndGrouping(t *testing.T) {
	cfg := Config{MaxPoints: 100, RechargePerSec: 1, WhitelistUserIDs: []int64{7}}
	st := cursorState{Wallets: map[string]wallet{}}
	if !consume(cfg, &st, 7, 1000) || len(st.Wallets) != 0 {
		t.Fatal("VIP must bypass and not persist quota")
	}
	if !consume(cfg, &st, 8, 25) || st.Wallets["8"].Points != 75 {
		t.Fatal("paid account quota")
	}
	encoded, _ := json.Marshal(st)
	restart, e := parseState(string(encoded))
	if e != nil || restart.Wallets["8"].Points != 75 {
		t.Fatalf("wallet cursor %s %v", encoded, e)
	}
	ev := []Event{{ID: "telegram:10:1", GroupKey: "10:1", Title: "a", Layout: &layout}, {ID: "telegram:11:2", GroupKey: "11:1", Title: "b", Layout: &layout}, {ID: "telegram:10:3", GroupKey: "10:2", Title: "c", Layout: &layout}}
	grouped := groupMessages(ev)
	if len(grouped) != 3 {
		t.Fatalf("different chats mixed: %#v", grouped)
	}
}

var layout = ticket.Layout{Blocks: []ticket.Block{{Type: "text", Text: "x"}}}

func TestLongPollUsesHTTPJSONRPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Write([]byte(`{"ok":true,"result":[{"update_id":41}]}`))
	}))
	defer srv.Close()
	c := client{cfg: Config{Token: "unused"}, http: srv.Client(), base: srv.URL}
	rows, e := c.updates(context.Background(), 40, 1)
	if e != nil || len(rows) != 1 || rows[0].UpdateID != 41 {
		t.Fatalf("updates=%#v err=%v", rows, e)
	}
}
func TestInlineQRProducesLayout(t *testing.T) {
	m := &message{Text: "Before [qr:https://example.com] after"}
	events, err := (client{}).message(context.Background(), m)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	blocks := events[0].Layout.Blocks
	if len(blocks) != 4 || blocks[2].Type != "qr" || blocks[2].Data != "https://example.com" {
		t.Fatalf("blocks=%#v", blocks)
	}
}

func TestCursorRejectsInvalid(t *testing.T) {
	for _, s := range []string{"-1", "invalid"} {
		if _, e := parseState(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
