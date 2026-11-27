package ticket

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/width"
)

const classicColumns = 32

// ClassicESCPos translates the built-in templates using the
// Telefax print format: raster artwork above printer-native GB18030 text.
func ClassicESCPos(layout Layout) ([]byte, error) {
	if len(layout.Blocks) == 0 || layout.Blocks[0].Type != "header" {
		return nil, fmt.Errorf("classic ticket requires a header")
	}
	header, err := render(Layout{Version: layout.Version, Blocks: layout.Blocks[:1]}, DefaultPaper(), false)
	if err != nil {
		return nil, err
	}
	data := append([]byte{27, 64}, header.ESCPOS()...)
	if layout.Blocks[0].Icon != "" {
		data = append(data, strings.Repeat("-", classicColumns)+"\n"...)
	}
	for _, block := range layout.Blocks[1:] {
		switch block.Type {
		case "divider":
			data = append(data, strings.Repeat("-", classicColumns)+"\n"...)
		case "qr":
			qr, err := Render(Layout{Blocks: []Block{block}}, DefaultPaper())
			if err != nil {
				return nil, err
			}
			data = append(data, qr.ESCPOS()...)
		case "text":
			data, err = appendClassicText(data, block)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported classic ticket block %q", block.Type)
		}
	}
	data = append(data, '\n')
	data = append(data, strings.Repeat("-", classicColumns)+"\n"...)
	return append(data, 29, 86, 66, 0), nil
}

func appendClassicText(data []byte, block Block) ([]byte, error) {
	text := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, block.Text))
	if text == "" {
		return data, nil
	}
	columns, size, bold := classicColumns, byte(0), byte(0)
	if block.Style == StyleBold || block.Style == StyleLarge {
		bold = 1
	}
	if block.Style == StyleLarge {
		columns, size = columns/2, 0x11
	}
	var wrapped strings.Builder
	cells := 0
	for _, r := range text {
		if r == '\n' {
			wrapped.WriteRune(r)
			cells = 0
			continue
		}
		n := 1
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n = 2
		}
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			n = 0
		}
		if cells+n > columns {
			wrapped.WriteByte('\n')
			cells = 0
		}
		wrapped.WriteRune(r)
		cells += n
	}
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(wrapped.String())
	if err != nil {
		return nil, fmt.Errorf("encode ticket text: %w", err)
	}
	data = append(data, 27, 'a', alignment(block.Align), 27, 'E', bold, 29, '!', size)
	data = append(data, encoded...)
	return append(data, '\n', 27, 'E', 0, 29, '!', 0, 27, 'a', 0), nil
}

// alignment maps a layout alignment to ESC/POS ESC a n.
func alignment(align string) byte {
	switch align {
	case AlignCenter:
		return 1
	case AlignRight:
		return 2
	default:
		return 0
	}
}
