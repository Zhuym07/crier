package ticket

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
)

func renderPNG(t *testing.T, layout Layout) (image.Image, []byte) {
	t.Helper()
	canvas, err := Render(layout, DefaultPaper())
	if err != nil {
		t.Fatal(err)
	}
	data, err := canvas.PNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img, data
}

func inkBounds(img image.Image) (minX, minY, maxX, maxY int) {
	b := img.Bounds()
	minX, minY, maxX, maxY = b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r < 0x8000 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	return minX, minY, maxX, maxY
}

func inkPixels(img image.Image, r image.Rectangle) int {
	r = r.Intersect(img.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			red, _, _, _ := img.At(x, y).RGBA()
			if red < 0x8000 {
				n++
			}
		}
	}
	return n
}

func TestMailHeaderIconMetaAndSender(t *testing.T) {
	layout := Layout{Blocks: []Block{
		{Type: "header", Title: "日程提醒", Icon: "event", Subtitle: "alice@example.com", Meta: "01-02 15:04"},
		{Type: "text", Style: "bold", Text: "产品评审"},
		{Type: "text", Text: "时间  明天 14:00"},
		{Type: "divider", Dash: true},
		{Type: "text", Text: "下午两点在 B203 评审新版本。"},
	}}
	img, _ := renderPNG(t, layout)
	if got := img.Bounds().Dx(); got != 384 {
		t.Fatalf("paper width = %d, want 384", got)
	}
	// The category glyph sits in the top-left corner of the header.
	if n := inkPixels(img, image.Rect(0, 0, 42, 52)); n < 40 {
		t.Errorf("category icon ink = %d, want a recognisable glyph", n)
	}
	// The label starts beside the icon and is big bold text.
	if n := inkPixels(img, image.Rect(44, 0, 300, 50)); n < 60 {
		t.Errorf("header label ink = %d, want large title", n)
	}
	// The timestamp is flush to the right edge on the header line.
	if n := inkPixels(img, image.Rect(384-140, 0, 384, 50)); n < 20 {
		t.Errorf("header meta ink = %d, want right aligned timestamp", n)
	}
	// The sender glyph sits below the icon.
	if n := inkPixels(img, image.Rect(0, 52, 34, 84)); n < 30 {
		t.Errorf("sender icon ink = %d, want person glyph", n)
	}
}

func TestHeaderWithoutIconStaysCentred(t *testing.T) {
	img, _ := renderPNG(t, Layout{Blocks: []Block{{Type: "header", Title: "Crier", Subtitle: "热敏打印测试"}}})
	// The header block ends with a full-width rule, so measure only rows that
	// carry text.
	minX, _, maxX, _ := inkTextBounds(img)
	if minX < 20 || 384-1-maxX < 20 {
		t.Fatalf("header ink spans %d..%d, want a centred block", minX, maxX)
	}
	if center := (minX + maxX) / 2; center < 175 || center > 209 {
		t.Fatalf("header ink centre = %d, want near 192", center)
	}
}

// inkTextBounds is inkBounds with near-full-width rows (dividers) ignored.
func inkTextBounds(img image.Image) (minX, minY, maxX, maxY int) {
	b := img.Bounds()
	minX, minY, maxX, maxY = b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		count := 0
		for x := b.Min.X; x < b.Max.X; x++ {
			red, _, _, _ := img.At(x, y).RGBA()
			if red < 0x8000 {
				count++
			}
		}
		if count > b.Dx()*3/4 {
			continue
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			red, _, _, _ := img.At(x, y).RGBA()
			if red >= 0x8000 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, minY, maxX, maxY
}

func TestEveryCategoryIconRenders(t *testing.T) {
	names := []string{"event", "deadline", "submission", "notice", "code", "todo", "bill", "ad", "general", "sender", "unknown-category"}
	distinct := map[uint64]string{}
	for _, name := range names {
		img, _ := renderPNG(t, Layout{Blocks: []Block{{Type: "header", Title: "测试", Icon: name}}})
		glyph := inkPixels(img, image.Rect(0, 0, 42, 52))
		if glyph < 30 {
			t.Errorf("%s: icon ink = %d, want a drawn glyph", name, glyph)
		}
		// A cheap fingerprint of the icon area catches accidentally identical
		// categories (for example a silent fallback for every name).
		var hash uint64 = 1469598103934665603
		for y := 0; y < 52; y++ {
			for x := 0; x < 42; x++ {
				red, _, _, _ := img.At(x, y).RGBA()
				hash ^= uint64(red)
				hash *= 1099511628211
			}
		}
		if other, ok := distinct[hash]; ok && name != "unknown-category" {
			t.Errorf("%s and %s render the same icon", name, other)
		}
		distinct[hash] = name
	}
}

func TestTypographySizesFollowESCOSGrid(t *testing.T) {
	render := func(style string) (width, height int) {
		img, _ := renderPNG(t, Layout{Blocks: []Block{{Type: "text", Style: style, Text: "国"}}})
		return img.Bounds().Dx(), img.Bounds().Dy()
	}
	normalW, normalH := render(StyleNormal)
	largeW, largeH := render(StyleLarge)
	dimW, dimH := render(StyleDim)
	if normalW <= 0 || largeW <= 0 || dimW <= 0 {
		t.Fatal("empty preview")
	}
	if normalH < 22 {
		t.Fatalf("normal glyph height = %d, want >= 22 dots (24 dot font)", normalH)
	}
	if largeH < 42 || largeH < normalH*3/2 {
		t.Fatalf("large glyph height = %d (normal %d), want a doubled headline", largeH, normalH)
	}
	if dimH >= normalH {
		t.Fatalf("dim glyph height = %d, want smaller than normal %d", dimH, normalH)
	}
}

func TestWrapTextNeverExceedsPaper(t *testing.T) {
	face, err := Face(StyleNormal)
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{
		"下午两点在 B203 评审新版本，请带上本周的进度报告和风险清单。",
		"https://example.com/very/long/path/that/keeps/going?with=query&and=more",
		"订单号 202511270001 已发货，预计明天送达，请保持电话畅通。",
	}
	for _, width := range []int{120, 240, 384} {
		for _, text := range texts {
			for _, line := range wrapText(face, text, width) {
				if got := measure(face, line); got > width {
					t.Fatalf("width %d: line %q measures %d", width, line, got)
				}
			}
		}
	}
}

func measure(face font.Face, line string) int {
	return font.MeasureString(face, line).Ceil()
}

func TestESCPOSHeaderDescribesInk(t *testing.T) {
	canvas, err := Render(Layout{Blocks: []Block{{Type: "header", Title: "账单通知", Icon: "bill", Meta: "01-02 15:04"}}}, DefaultPaper())
	if err != nil {
		t.Fatal(err)
	}
	data := canvas.ESCPOS()
	if len(data) < 8 || data[0] != 0x1d || data[1] != 0x76 || data[2] != 0x30 || data[3] != 0x00 {
		t.Fatalf("raster header = % x", data[:min(8, len(data))])
	}
	rowBytes := int(data[4]) | int(data[5])<<8
	height := int(data[6]) | int(data[7])<<8
	if rowBytes != (canvas.Width()+7)/8 {
		t.Fatalf("row bytes = %d, want %d", rowBytes, (canvas.Width()+7)/8)
	}
	if height != canvas.ContentHeight() || height == 0 {
		t.Fatalf("header raster height = %d, want content height %d", height, canvas.ContentHeight())
	}
	if len(data) != 8+rowBytes*height {
		t.Fatalf("raster bytes = %d, want %d", len(data), 8+rowBytes*height)
	}
}

// TestPreviewArtifacts writes PNGs the operator can eyeball; it only runs when
// TICKET_PREVIEW_DIR is set, so normal test runs stay pure.
func TestPreviewArtifacts(t *testing.T) {
	dir := os.Getenv("TICKET_PREVIEW_DIR")
	if dir == "" {
		t.Skip("TICKET_PREVIEW_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("preview: %s", path)
	}

	icons := NewCanvas(Paper{Width: 384, MaxHeight: 384})
	names := []string{"event", "deadline", "submission", "notice", "code", "todo", "bill", "ad", "sender", "general"}
	for i, name := range names {
		icons.pasteIcon(iconImage(name), 20+(i%5)*72, 10+(i/5)*72, 48)
	}
	iconPNG, err := icons.PNG()
	if err != nil {
		t.Fatal(err)
	}
	write("icons.png", iconPNG)

	layouts := map[string]Layout{
		"mail-code.png": {Blocks: []Block{
			{Type: "header", Title: "验证码", Icon: "code", Subtitle: "security@example.com", Meta: "11-27 10:33"},
			{Type: "text", Style: "bold", Text: "登录验证码"},
			{Type: "text", Style: "large", Text: "验证码  482913"},
			{Type: "divider", Dash: true},
			{Type: "text", Text: "10 分钟内有效，请勿转发给他人。"},
		}},
		"mail-notice.png": {Blocks: []Block{
			{Type: "header", Title: "公告通知", Icon: "notice", Subtitle: "校长办公室 <office@university.edu.cn>", Meta: "11-27 08:00"},
			{Type: "text", Style: "bold", Text: "关于调整本周课程安排的通知"},
			{Type: "text", Text: "• 周一上午的课程调整到周三下午。"},
			{Type: "text", Text: "• 实验室开放时间延长至 21:00。"},
			{Type: "text", Text: "• 请各班班长及时转达。"},
			{Type: "spacer", Dots: 4},
			{Type: "divider", Dash: true},
			{Type: "text", Text: "请同学们按时参加，如有疑问请联系教务处。"},
		}},
		"mail-bill.png": {Blocks: []Block{
			{Type: "header", Title: "账单通知", Icon: "bill", Subtitle: "billing@example.com", Meta: "11-27 09:12"},
			{Type: "text", Style: "bold", Text: "11 月账单已生成"},
			{Type: "text", Text: "商户  云服务"},
			{Type: "text", Text: "金额  RMB 128.00"},
			{Type: "divider", Dash: true},
			{Type: "text", Text: "请在 12 月 5 日前完成支付。"},
		}},
		"mail-event.png": {Blocks: []Block{
			{Type: "header", Title: "日程提醒", Icon: "event", Subtitle: "alice@example.com", Meta: "11-27 10:33"},
			{Type: "text", Style: "bold", Text: "产品评审会议"},
			{Type: "text", Text: "时间  明天 14:00"},
			{Type: "text", Text: "地点  B203 会议室"},
			{Type: "divider", Dash: true},
			{Type: "text", Text: "请带上本周的进度报告和风险清单。"},
		}},
		"mail-general.png": {Blocks: []Block{
			{Type: "header", Title: "邮件摘要", Icon: "general", Subtitle: "news@example.com", Meta: "11-27 07:45"},
			{Type: "text", Style: "bold", Text: "本周产品更新速览"},
			{Type: "divider", Dash: true},
			{Type: "text", Text: "分类不确定\n本周更新了打印预览、AI 摘要和插件管理。"},
		}},
		"header-telegram.png": {Blocks: []Block{
			{Type: "header", Title: "TELE FAX", Subtitle: "user@telegram"},
			{Type: "text", Text: "打印机测试"},
			{Type: "text", Text: "系统运行正常，中英文混排 normal text 也要清晰。"},
		}},
	}
	for name, layout := range layouts {
		canvas, err := Render(layout, DefaultPaper())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, err := canvas.PNG()
		if err != nil {
			t.Fatal(err)
		}
		write(name, data)
	}
}