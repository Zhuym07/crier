package ticket

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// A dot darker than this becomes ink. The renderer antialiases text, so the
// threshold is what makes the result printable on a one-bit head.
const inkThreshold = 128

// Canvas is the drawing surface for one ticket.
type Canvas struct {
	img *image.Gray
}

// NewCanvas allocates a white surface of the given paper geometry.
func NewCanvas(paper Paper) *Canvas {
	if paper.Width <= 0 {
		paper.Width = DefaultPaper().Width
	}
	if paper.MaxHeight <= 0 {
		paper.MaxHeight = DefaultPaper().MaxHeight
	}
	img := image.NewGray(image.Rect(0, 0, paper.Width, paper.MaxHeight))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	return &Canvas{img: img}
}

// Image exposes the underlying surface for drawing.
func (c *Canvas) Image() *image.Gray { return c.img }

// Width is the printable width in dots.
func (c *Canvas) Width() int { return c.img.Rect.Dx() }

// ContentHeight is the bottom of the last inked row; 0 means blank.
func (c *Canvas) ContentHeight() int {
	for y := c.img.Rect.Dy() - 1; y >= 0; y-- {
		row := c.img.Pix[y*c.img.Stride : y*c.img.Stride+c.img.Rect.Dx()]
		for _, v := range row {
			if v < inkThreshold {
				return y + 1
			}
		}
	}
	return 0
}

// ESCPOS encodes the inked area as a GS v 0 raster image command body.
// The caller frames it with printer control bytes; see Profile.
func (c *Canvas) ESCPOS() []byte {
	h := c.ContentHeight()
	if h == 0 {
		return nil
	}
	w := c.img.Rect.Dx()
	rowBytes := (w + 7) / 8
	buf := bytes.NewBuffer(make([]byte, 0, 8+rowBytes*h))
	buf.Write([]byte{0x1d, 0x76, 0x30, 0x00})
	buf.Write([]byte{byte(rowBytes & 0xff), byte(rowBytes >> 8 & 0xff), byte(h & 0xff), byte(h >> 8 & 0xff)})
	for y := 0; y < h; y++ {
		row := c.img.Pix[y*c.img.Stride:]
		for bx := 0; bx < rowBytes; bx++ {
			var b byte
			for bit := 0; bit < 8; bit++ {
				x := bx*8 + bit
				if x < w && row[x] < inkThreshold {
					b |= 1 << (7 - bit)
				}
			}
			buf.WriteByte(b)
		}
	}
	return buf.Bytes()
}

// PNG writes the inked area as a pure black and white image, which is what the
// operator sees on paper. It exists for previewing and for tests.
func (c *Canvas) PNG() ([]byte, error) {
	h := c.ContentHeight()
	if h == 0 {
		return nil, errors.New("ticket: nothing inked to render")
	}
	out := image.NewGray(image.Rect(0, 0, c.Width(), h))
	for y := 0; y < h; y++ {
		for x := 0; x < c.Width(); x++ {
			if c.img.Pix[y*c.img.Stride+x] < inkThreshold {
				out.Pix[y*out.Stride+x] = 0
			} else {
				out.Pix[y*out.Stride+x] = 0xff
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Font styles a layout block may ask for.
const (
	StyleNormal = "normal"
	StyleBold   = "bold"
	StyleLarge  = "large"
	StyleDim    = "dim"
)

// Sizes follow the Telefax/ESC/POS grid: normal body text is 24 dots tall (one
// full-width character per 24 dots), which is what the original 58 mm tickets
// printed at; large is the doubled headline size and dim is the small date
// face. A 384 dot head therefore carries 16 full-width characters per line.
var styleSize = map[string]int{
	StyleNormal: 24,
	StyleBold:   24,
	StyleLarge:  48,
	StyleDim:    18,
}

// Candidates are tried in order; the first readable file wins. Microsoft YaHei
// covers both Chinese and Latin, so one family serves the whole ticket.
var (
	regularFontFiles = []string{
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\msyh.ttf`,
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\Deng.ttf`,
	}
	boldFontFiles = []string{
		`C:\Windows\Fonts\msyhbd.ttc`,
		`C:\Windows\Fonts\msyhbd.ttf`,
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\Deng.ttf`,
	}
)

var (
	faceMu    sync.Mutex
	faceCache = map[string]font.Face{}
)

// Face returns the font face for a style, loading it on first use.
func Face(style string) (font.Face, error) {
	px, ok := styleSize[style]
	if !ok {
		return nil, fmt.Errorf("ticket: unknown text style %q", style)
	}
	return faceAt(px, style == StyleBold || style == StyleLarge)
}

// faceAt returns a face at an explicit pixel size. Header typography uses sizes
// that are not part of the four body styles (a large label, a smaller date and
// a bold sender line), so they share this loader and its cache.
func faceAt(px int, bold bool) (font.Face, error) {
	files := regularFontFiles
	if bold {
		files = boldFontFiles
	}
	key := fmt.Sprintf("%d/%t", px, bold)
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f, nil
	}
	f, err := loadFace(files, px)
	if err != nil {
		return nil, err
	}
	faceCache[key] = f
	return f, nil
}

func loadFace(files []string, px int) (font.Face, error) {
	var lastErr error
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		face, err := faceFromFontData(data, px)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", path, err)
			continue
		}
		return face, nil
	}
	return nil, fmt.Errorf("ticket: no usable system font: %w", lastErr)
}

func faceFromFontData(data []byte, px int) (font.Face, error) {
	var parsed *sfnt.Font
	var err error
	if len(data) >= 4 && string(data[:4]) == "ttcf" {
		var coll *sfnt.Collection
		coll, err = sfnt.ParseCollection(data)
		if err != nil {
			return nil, err
		}
		parsed, err = coll.Font(0)
	} else {
		parsed, err = sfnt.Parse(data)
	}
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    float64(px),
		DPI:     72, // one point is one dot, so Size is the pixel height
		Hinting: font.HintingFull,
	})
}

// pasteIcon scales one master-size category glyph into the surface. The icons
// are authored at 48 dots and shrunk with the same smooth filter the original
// printer used, which keeps their stroke weight balanced against the text.
func (c *Canvas) pasteIcon(icon *image.Gray, x, y, size int) {
	dst := image.Rect(x, y, x+size, y+size).Intersect(c.img.Rect)
	if dst.Empty() {
		return
	}
	draw.CatmullRom.Scale(c.img, dst, icon, icon.Bounds(), draw.Over, nil)
}

// inkRect fills a rectangle with ink, clipped to the surface.
func (c *Canvas) inkRect(x0, y0, x1, y1 int) {
	r := image.Rect(x0, y0, x1, y1).Intersect(c.img.Rect)
	if r.Empty() {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := c.img.Pix[y*c.img.Stride:]
		for x := r.Min.X; x < r.Max.X; x++ {
			row[x] = 0
		}
	}
}

// inkStyle is used when a block needs a solid color for the draw package.
var inkStyle = image.NewUniform(color.Black)
