package crier

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Zhuym07/crier/internal/ticket"
)

func TestTestTicket(t *testing.T) {
	data, err := printData(testLayout())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{27, 64, 29, 118, 48, 0, 48, 0}) {
		t.Fatalf("invalid ESC/POS header: %x", data[:min(len(data), 10)])
	}
	h := int(data[8]) + int(data[9])*256
	if h == 0 || len(data) != 10+48*h+3 {
		t.Fatalf("inconsistent raster height %d for %d bytes", h, len(data))
	}
}

func TestPluginControlsAI(t *testing.T) {
	cfg := AIConfig{Enabled: true}
	layout := ticket.Layout{Blocks: []ticket.Block{{Type: "text", Text: "plugin layout"}}}
	got, err := prepare(context.Background(), event{Layout: &layout, AI: "summarize"}, cfg, true)
	if err != nil || got.Blocks[0].Text != "plugin layout" {
		t.Fatalf("plugin layout: %+v, %v", got, err)
	}
	got, err = prepare(context.Background(), event{Template: "reminder", Title: "title", Body: "original", AI: "summarize"}, cfg, false)
	if err != nil || !strings.Contains(got.Blocks[1].Text, "original") {
		t.Fatalf("template without AI permission: %+v, %v", got, err)
	}
}
