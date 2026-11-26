// Package ticket turns a declarative layout into one-bit print data.
//
// A plugin describes what its receipt looks like; this package owns how the
// dots are produced. Nothing outside this package may emit raster data, so
// every ticket keeps the same paper width, font set and ink threshold.
package ticket

// Layout is one complete receipt body. Version allows later schema changes
// without silently misreading an older plugin's output.
type Layout struct {
	Version int     `json:"version,omitempty"`
	Blocks  []Block `json:"blocks"`
}

// Block is one element of a layout. A block's meaning is chosen by Type; the
// fields a type ignores stay empty.
type Block struct {
	Type string `json:"type"`

	// text
	Text  string `json:"text,omitempty"`
	Style string `json:"style,omitempty"` // normal | bold | large | dim
	Align string `json:"align,omitempty"` // left | center | right

	// kv (one label/value row), rows (many of them)
	Key  string `json:"key,omitempty"`
	Val  string `json:"val,omitempty"`
	Rows []Row  `json:"rows,omitempty"`

	// divider
	Dash bool `json:"dash,omitempty"`

	// spacer
	Dots int `json:"dots,omitempty"`

	// header
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`

	// qr
	Data string `json:"data,omitempty"`

	// image: base64-encoded PNG or JPEG
	Image string `json:"image,omitempty"`

	// box
	Blocks []Block `json:"blocks,omitempty"`
}

// Row is a label/value pair inside a kv block.
type Row struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

// Paper is the printable geometry of one roll.
type Paper struct {
	// Width is the printable width in dots. A 58 mm head is 384.
	Width int
	// MaxHeight bounds the canvas; content beyond it is clipped.
	MaxHeight int
}

// DefaultPaper matches the 58 mm printers this project targets.
func DefaultPaper() Paper {
	return Paper{Width: 384, MaxHeight: 4096}
}
