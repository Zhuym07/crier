package ticket

import (
	"image"
	"math"
)

// iconSize is the master size the category glyphs are drawn at. The header
// scales one down to fit, exactly like the original Telefax printer did with
// its 48 dot PIL bitmaps.
const iconSize = 48

// iconImage returns the 48x48 line drawing for a ticket category. Names match
// the mail categories; anything unrecognised falls back to the envelope, which
// the original printer used for "general".
func iconImage(name string) *image.Gray {
	b := newIconBuf()
	switch name {
	case "event":
		b.strokeRoundRect(7, 9, 41, 42, 4, 3)
		b.line(15, 5, 15, 14, 3)
		b.line(33, 5, 33, 14, 3)
		b.line(8, 18, 40, 18, 3)
		b.line(15, 26, 33, 26, 3)
		b.line(15, 33, 28, 33, 3)
	case "deadline":
		b.strokeEllipse(6, 6, 42, 42, 3)
		b.polyline([][2]int{{24, 13}, {24, 25}, {33, 30}}, 3)
	case "submission":
		b.strokeEllipse(5, 5, 43, 43, 3)
		b.polyline([][2]int{{13, 24}, {21, 32}, {36, 16}}, 4)
	case "notice":
		b.polyline([][2]int{{24, 5}, {44, 41}, {4, 41}, {24, 5}}, 3)
		b.line(24, 16, 24, 28, 4)
		b.fillEllipse(22, 33, 26, 37)
	case "code":
		b.strokeEllipse(5, 9, 32, 36, 3)
		b.strokeEllipse(14, 18, 23, 27, 3)
		b.line(28, 30, 43, 15, 4)
		b.line(35, 23, 40, 28, 3)
	case "todo":
		for _, y := range []int{8, 21, 34} {
			b.strokeRect(6, y, 15, y+9, 2)
			b.line(21, y+4, 42, y+4, 2)
		}
		b.polyline([][2]int{{8, 25}, {11, 28}, {16, 22}}, 2)
	case "bill":
		b.strokeRoundRect(9, 4, 39, 44, 3, 3)
		b.line(16, 14, 32, 14, 2)
		b.line(16, 22, 32, 22, 2)
		b.line(16, 30, 28, 30, 2)
		b.line(16, 37, 26, 37, 2)
	case "ad":
		b.polyline([][2]int{{7, 18}, {31, 10}, {31, 35}, {7, 28}, {7, 18}}, 3)
		b.strokeRect(31, 14, 39, 31, 2)
		b.polyline([][2]int{{12, 29}, {20, 31}, {24, 42}, {17, 40}, {12, 29}}, 3)
		b.line(37, 8, 42, 4, 2)
		b.line(39, 22, 46, 22, 2)
		b.line(37, 35, 42, 40, 2)
	case "sender":
		b.fillEllipse(17, 5, 31, 19)
		b.fillRoundRect(7, 23, 41, 43, 9)
	default:
		b.strokeRect(5, 10, 43, 38, 3)
		b.polyline([][2]int{{6, 12}, {24, 27}, {42, 12}}, 3)
	}
	return b.img
}

// iconBuf is a white 48x48 scratch surface. Shapes are stamped in ink (black)
// with the same geometry the original PIL drawings used, so the downscaled
// header glyphs stay recognisable at 36 and 26 dots.
type iconBuf struct {
	img *image.Gray
}

func newIconBuf() *iconBuf {
	img := image.NewGray(image.Rect(0, 0, iconSize, iconSize))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	return &iconBuf{img: img}
}

func (b *iconBuf) ink(x, y int) {
	if x < 0 || y < 0 || x >= iconSize || y >= iconSize {
		return
	}
	b.img.Pix[y*b.img.Stride+x] = 0
}

func (b *iconBuf) line(x0, y0, x1, y1, width int) {
	r := float64(width) / 2
	loX := minInt(minInt(x0, x1)-width, 0)
	hiX := maxInt(maxInt(x0, x1)+width, iconSize-1)
	loY := minInt(minInt(y0, y1)-width, 0)
	hiY := maxInt(maxInt(y0, y1)+width, iconSize-1)
	for y := loY; y <= hiY; y++ {
		for x := loX; x <= hiX; x++ {
			if distanceToSegment(float64(x)+0.5, float64(y)+0.5, float64(x0), float64(y0), float64(x1), float64(y1)) <= r {
				b.ink(x, y)
			}
		}
	}
}

func (b *iconBuf) polyline(points [][2]int, width int) {
	for i := 1; i < len(points); i++ {
		b.line(points[i-1][0], points[i-1][1], points[i][0], points[i][1], width)
	}
}

func (b *iconBuf) strokeRect(x0, y0, x1, y1, width int) {
	b.walk(x0, y0, x1, y1, func(px, py float64) bool {
		return inRect(px, py, x0, y0, x1, y1) && !inRect(px, py, x0+width, y0+width, x1-width, y1-width)
	})
}

func (b *iconBuf) strokeRoundRect(x0, y0, x1, y1, radius, width int) {
	b.walk(x0, y0, x1, y1, func(px, py float64) bool {
		return inRoundRect(px, py, x0, y0, x1, y1, radius) &&
			!inRoundRect(px, py, x0+width, y0+width, x1-width, y1-width, radius-width)
	})
}

func (b *iconBuf) fillRoundRect(x0, y0, x1, y1, radius int) {
	b.walk(x0, y0, x1, y1, func(px, py float64) bool {
		return inRoundRect(px, py, x0, y0, x1, y1, radius)
	})
}

func (b *iconBuf) strokeEllipse(x0, y0, x1, y1, width int) {
	b.walk(x0, y0, x1, y1, func(px, py float64) bool {
		return inEllipse(px, py, x0, y0, x1, y1) && !inEllipse(px, py, x0+width, y0+width, x1-width, y1-width)
	})
}

func (b *iconBuf) fillEllipse(x0, y0, x1, y1 int) {
	b.walk(x0, y0, x1, y1, func(px, py float64) bool {
		return inEllipse(px, py, x0, y0, x1, y1)
	})
}

func (b *iconBuf) walk(x0, y0, x1, y1 int, inside func(px, py float64) bool) {
	for y := maxInt(y0, 0); y <= minInt(y1, iconSize-1); y++ {
		for x := maxInt(x0, 0); x <= minInt(x1, iconSize-1); x++ {
			if inside(float64(x)+0.5, float64(y)+0.5) {
				b.ink(x, y)
			}
		}
	}
}

func inRect(px, py float64, x0, y0, x1, y1 int) bool {
	return px >= float64(x0) && px <= float64(x1) && py >= float64(y0) && py <= float64(y1)
}

func inEllipse(px, py float64, x0, y0, x1, y1 int) bool {
	cx := float64(x0+x1) / 2
	cy := float64(y0+y1) / 2
	rx := float64(x1-x0) / 2
	ry := float64(y1-y0) / 2
	if rx <= 0 || ry <= 0 {
		return false
	}
	dx := (px - cx) / rx
	dy := (py - cy) / ry
	return dx*dx+dy*dy <= 1
}

func inRoundRect(px, py float64, x0, y0, x1, y1, radius int) bool {
	if !inRect(px, py, x0, y0, x1, y1) {
		return false
	}
	if radius <= 0 {
		return true
	}
	coreX0, coreX1 := float64(x0+radius), float64(x1-radius)
	coreY0, coreY1 := float64(y0+radius), float64(y1-radius)
	cx := math.Min(math.Max(px, coreX0), coreX1)
	cy := math.Min(math.Max(py, coreY0), coreY1)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= float64(radius)*float64(radius)
}

func distanceToSegment(px, py, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	lengthSquared := dx*dx + dy*dy
	if lengthSquared == 0 {
		return math.Hypot(px-x0, py-y0)
	}
	t := ((px-x0)*dx + (py-y0)*dy) / lengthSquared
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(x0+t*dx), py-(y0+t*dy))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}