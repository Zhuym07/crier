package ticket

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"sync"

	"github.com/skip2/go-qrcode"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// spacing between logical lines, in dots
const leadingDots = 3

// block padding and inset for boxes
const (
	boxPad      = 6
	boxTitleGap = 4
)

var renderMu sync.Mutex // opentype faces use shared mutable scratch buffers.

// Render draws a layout onto a fresh canvas at the requested paper geometry.
func Render(layout Layout, paper Paper) (*Canvas, error) {
	renderMu.Lock()
	defer renderMu.Unlock()
	if layout.Version != 0 && layout.Version != 1 {
		return nil, errors.New("ticket: unsupported layout version")
	}
	if len(layout.Blocks) == 0 {
		return nil, errors.New("ticket: layout has no blocks")
	}
	r := &renderer{c: NewCanvas(paper), width: paper.Width}
	if r.width <= 0 {
		r.width = DefaultPaper().Width
	}
	if err := r.blocks(layout.Blocks); err != nil {
		return nil, err
	}
	if r.c.ContentHeight() == 0 {
		return nil, errors.New("ticket: layout produced no ink")
	}
	return r.c, nil
}

type renderer struct {
	c      *Canvas
	width  int
	y      int
	indent int
}

func (r *renderer) blocks(blocks []Block) error {
	for i, b := range blocks {
		if err := r.block(b); err != nil {
			return fmt.Errorf("block %d (%s): %w", i, b.Type, err)
		}
		if r.y < 0 || r.y > r.c.Image().Rect.Dy() {
			return errors.New("ticket exceeds maximum height")
		}
	}
	return nil
}

func (r *renderer) block(b Block) error {
	switch b.Type {
	case "text":
		return r.text(b)
	case "kv":
		return r.kv([]Row{{Key: b.Key, Val: b.Val}}, b.Style)
	case "rows":
		return r.kv(b.Rows, b.Style)
	case "header":
		return r.header(b)
	case "divider":
		return r.divider(b.Dash)
	case "spacer":
		return r.spacer(b.Dots)
	case "qr":
		return r.qr(b.Data)
	case "image":
		return r.image(b.Image)
	case "box":
		return r.box(b)
	default:
		return fmt.Errorf("unknown block type")
	}
}

func (r *renderer) availWidth() int {
	return r.width - 2*r.indent
}

func (r *renderer) text(b Block) error {
	style := b.Style
	if style == "" {
		style = StyleNormal
	}
	face, err := Face(style)
	if err != nil {
		return err
	}
	for _, line := range wrapText(face, b.Text, r.availWidth()) {
		r.emitLine(line, face, b.Align)
	}
	return nil
}

func (r *renderer) kv(rows []Row, style string) error {
	if style == "" {
		style = StyleNormal
	}
	face, err := Face(style)
	if err != nil {
		return err
	}
	for _, row := range rows {
		keyWidth := font.MeasureString(face, row.Key).Ceil()
		valWidth := font.MeasureString(face, row.Val).Ceil()
		const gap = 8
		if row.Key == "" {
			for _, line := range wrapText(face, row.Val, r.availWidth()) {
				r.emitLine(line, face, AlignRight)
			}
			continue
		}
		if valWidth == 0 {
			r.emitLine(row.Key, face, AlignLeft)
			continue
		}
		if keyWidth+gap+valWidth <= r.availWidth() {
			r.drawAt(row.Key, face, r.indent)
			r.drawAt(row.Val, face, r.width-r.indent-valWidth)
			r.advance(face)
			continue
		}
		// The pair does not fit on one line: label first, then the wrapped value.
		r.emitLine(row.Key, face, AlignLeft)
		for _, line := range wrapText(face, row.Val, r.availWidth()) {
			r.emitLine(line, face, AlignRight)
		}
	}
	return nil
}

func (r *renderer) header(b Block) error {
	titleFace, err := Face(StyleBold)
	if err != nil {
		return err
	}
	if b.Title != "" {
		for _, line := range wrapText(titleFace, b.Title, r.availWidth()) {
			r.emitLine(line, titleFace, AlignCenter)
		}
	}
	if b.Subtitle != "" {
		subFace, err := Face(StyleDim)
		if err != nil {
			return err
		}
		for _, line := range wrapText(subFace, b.Subtitle, r.availWidth()) {
			r.emitLine(line, subFace, AlignCenter)
		}
	}
	r.spacer(4)
	return r.divider(false)
}

func (r *renderer) divider(dashed bool) error {
	const thickness = 2
	y0 := r.y + 2
	if dashed {
		for x := r.indent; x < r.width-r.indent; x += 8 {
			r.c.inkRect(x, y0, min(x+4, r.width-r.indent), y0+thickness)
		}
	} else {
		r.c.inkRect(r.indent, y0, r.width-r.indent, y0+thickness)
	}
	r.y = y0 + thickness + 6
	return nil
}

func (r *renderer) spacer(dots int) error {
	if dots <= 0 {
		dots = 8
	}
	r.y += dots
	return nil
}

func (r *renderer) qr(data string) error {
	if data == "" {
		return errors.New("qr block has no data")
	}
	code, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return err
	}
	code.DisableBorder = false
	bitmap := code.Bitmap()
	modules := len(bitmap)
	if modules == 0 {
		return errors.New("qr encoder returned an empty bitmap")
	}
	// Pick the largest integer module size that fits at most 60% of the width,
	// so a QR does not run edge to edge.
	limit := r.availWidth() * 3 / 5
	scale := limit / modules
	if scale < 1 {
		scale = 1
	}
	if scale > 8 {
		scale = 8
	}
	side := modules * scale
	x0 := r.indent + (r.availWidth()-side)/2
	for my, row := range bitmap {
		for mx, on := range row {
			if !on {
				continue
			}
			r.c.inkRect(x0+mx*scale, r.y+my*scale, x0+(mx+1)*scale, r.y+(my+1)*scale)
		}
	}
	r.y += side + 8
	return nil
}

func (r *renderer) image(encoded string) error {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("image block is not valid base64: %w", err)
	}
	geometry, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || geometry.Width <= 0 || geometry.Height <= 0 || int64(geometry.Width)*int64(geometry.Height) > 8_000_000 {
		return errors.New("image block has invalid dimensions or exceeds 8 megapixels")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("image block is not a decodable image: %w", err)
	}
	b := src.Bounds()
	target := r.availWidth()
	height := b.Dy() * target / b.Dx()
	if height <= 0 || height > r.c.Image().Rect.Dy()-r.y {
		return errors.New("image block exceeds remaining ticket height")
	}
	dst := image.Rect(r.indent, r.y, r.indent+target, r.y+height)
	draw.CatmullRom.Scale(r.c.Image(), dst, src, b, draw.Over, nil)
	r.y += height + 8
	return nil
}

// box draws a frame around its own blocks. Children render into a scratch
// canvas first so the frame can be sized to the content.
func (r *renderer) box(b Block) error {
	innerWidth := r.availWidth() - 2*boxPad
	if innerWidth < 32 {
		return errors.New("box has too little room to draw")
	}
	scratch := &renderer{c: NewCanvas(Paper{Width: innerWidth, MaxHeight: r.c.Image().Rect.Dy()}), width: innerWidth}
	if err := scratch.blocks(b.Blocks); err != nil {
		return err
	}
	content := scratch.c.ContentHeight()
	if content == 0 {
		content = 1
	}
	titleFace, err := Face(StyleBold)
	if err != nil {
		return err
	}
	titleHeight := 0
	if b.Title != "" {
		titleHeight = lineHeight(titleFace) + boxTitleGap
	}
	top := r.y
	bottom := top + content + 2*boxPad + titleHeight
	left := r.indent
	right := r.width - r.indent
	const stroke = 2
	r.c.inkRect(left, top, right, top+stroke)
	r.c.inkRect(left, bottom-stroke, right, bottom)
	r.c.inkRect(left, top, left+stroke, bottom)
	r.c.inkRect(right-stroke, top, right, bottom)
	if b.Title != "" {
		label := " " + b.Title + " "
		labelWidth := font.MeasureString(titleFace, label).Ceil()
		lx := left + (right-left-labelWidth)/2
		// clear the frame where the label sits, then draw the label
		r.eraseRect(lx, top, lx+labelWidth, top+stroke)
		r.drawAt(label, titleFace, lx)
	}
	dst := image.Rect(left+stroke+boxPad, top+boxPad+titleHeight, left+stroke+boxPad+innerWidth, top+boxPad+titleHeight+content)
	draw.Draw(r.c.Image(), dst, scratch.c.Image(), image.Pt(0, 0), draw.Over)
	r.y = bottom + 8
	return nil
}

// eraseRect paints white, used to cut a gap in a frame for a box label.
func (r *renderer) eraseRect(x0, y0, x1, y1 int) {
	img := r.c.Image()
	rect := image.Rect(x0, y0, x1, y1).Intersect(img.Rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		row := img.Pix[y*img.Stride:]
		for x := rect.Min.X; x < rect.Max.X; x++ {
			row[x] = 0xff
		}
	}
}

// Alignment values a block may request.
const (
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

func (r *renderer) emitLine(text string, face font.Face, align string) {
	width := font.MeasureString(face, text).Ceil()
	x := r.indent
	switch align {
	case AlignCenter:
		x = r.indent + (r.availWidth()-width)/2
	case AlignRight:
		x = r.width - r.indent - width
	}
	r.drawAt(text, face, x)
	r.advance(face)
}

// drawAt writes one line with its left edge at x, without moving the cursor.
func (r *renderer) drawAt(text string, face font.Face, x int) {
	m := face.Metrics()
	baseline := r.y + m.Ascent.Ceil()
	d := &font.Drawer{
		Dst:  r.c.Image(),
		Src:  inkStyle,
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	d.DrawString(text)
}

func (r *renderer) advance(face font.Face) {
	r.y += lineHeight(face)
}

func lineHeight(face font.Face) int {
	m := face.Metrics()
	return (m.Ascent + m.Descent).Ceil() + leadingDots
}

// wrapText breaks a string into lines that fit the width. Paragraph breaks are
// honoured; inside a paragraph the last space is preferred, which keeps Latin
// words intact, and CJK breaks between characters.
func wrapText(face font.Face, text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var cur []rune
		flush := func() {
			out = append(out, strings.TrimRight(string(cur), " "))
			cur = nil
		}
		for _, r := range []rune(para) {
			next := append(append([]rune{}, cur...), r)
			if len(cur) == 0 || font.MeasureString(face, string(next)).Ceil() <= width {
				cur = next
				continue
			}
			cut := -1
			for i := len(cur) - 1; i > 0; i-- {
				if cur[i] == ' ' {
					cut = i
					break
				}
			}
			if cut > 0 {
				out = append(out, string(cur[:cut]))
				cur = append(append([]rune{}, cur[cut+1:]...), r)
			} else {
				out = append(out, string(cur))
				cur = []rune{r}
			}
		}
		flush()
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
