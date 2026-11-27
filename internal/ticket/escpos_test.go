package ticket

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestClassicESCPosUsesNativeTextAfterRasterHeader(t *testing.T) {
	data, err := ClassicESCPos(Layout{Blocks: []Block{
		{Type: "header", Icon: "code", Title: "验证码"},
		{Type: "text", Style: StyleBold, Text: strings.Repeat("中", 17) + "\x1b\x00\n尾"},
		{Type: "divider", Dash: true},
		{Type: "text", Style: StyleLarge, Text: "验证码  123456"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{27, 64, 29, 118, 48, 0, 48, 0}) {
		t.Fatalf("unexpected raster header: % x", data[:min(10, len(data))])
	}
	rows := int(data[8]) | int(data[9])<<8
	body := data[10+48*rows:]
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(strings.Repeat("中", 16) + "\n中\n尾\n")
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(strings.Repeat("-", 32)+"\n"), 27, 'a', 0, 27, 'E', 1, 29, '!', 0)
	want = append(want, encoded...)
	if !bytes.HasPrefix(body, want) || !bytes.Contains(body, []byte{29, '!', 0x11}) || !bytes.HasSuffix(body, []byte{29, 86, 66, 0}) {
		t.Fatalf("unexpected native body: % x", body)
	}
}
