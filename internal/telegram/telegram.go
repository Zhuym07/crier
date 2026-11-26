// Package telegram polls a Telegram bot and translates user messages into Crier events.
package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Zhuym07/crier/internal/ticket"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxMediaBytes = 5 << 20
	qrFixedCost   = 150
	updateLimit   = 2
)
const apiBase = "https://api.telegram.org"

type Config struct {
	Token               string    `json:"token"`
	ProxyURL            string    `json:"proxy_url"`
	BatchTimeoutSeconds int       `json:"batch_timeout_seconds"`
	MaxPoints           float64   `json:"max_points"`
	RechargePerSec      float64   `json:"recharge_per_sec"`
	WhitelistUserIDs    []int64   `json:"whitelist_user_ids"`
	LogChannelID        int64     `json:"log_channel_id"`
	TodoChannelID       int64     `json:"todo_channel_id"`
	ImageCharsPerLine   int       `json:"chars_per_line"`
	LineHeightPx        int       `json:"line_height_px"`
	Log                 io.Writer `json:"-"`
}
type Event struct {
	ID        string         `json:"id"`
	GroupKey  string         `json:"-"`
	MediaCost int            `json:"-"`
	Title     string         `json:"title"`
	Body      string         `json:"body,omitempty"`
	Template  string         `json:"template,omitempty"`
	AI        string         `json:"ai,omitempty"`
	Layout    *ticket.Layout `json:"layout,omitempty"`
}
type Result struct {
	Cursor string  `json:"cursor"`
	Events []Event `json:"events"`
}
type cursorState struct {
	Offset  int64             `json:"offset"`
	Wallets map[string]wallet `json:"wallets,omitempty"`
}
type wallet struct {
	Points  float64 `json:"points"`
	Updated int64   `json:"updated"`
}
type client struct {
	cfg  Config
	http *http.Client
	base string
}
type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
}
type message struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
	} `json:"from"`
	Text    string `json:"text"`
	Caption string `json:"caption"`
	Photo   []struct {
		FileID string `json:"file_id"`
	} `json:"photo"`
	Sticker *struct {
		FileID string `json:"file_id"`
	} `json:"sticker"`
}
type envelope[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

func Poll(ctx context.Context, cfg Config, cursor string) (Result, error) {
	if cfg.Token == "" {
		return Result{}, errors.New("token is required")
	}
	st, err := parseState(cursor)
	if err != nil {
		return Result{}, err
	}
	if st.Wallets == nil {
		st.Wallets = map[string]wallet{}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ProxyURL != "" {
		u, e := url.Parse(cfg.ProxyURL)
		if e != nil || u.Scheme == "" || u.Host == "" {
			return Result{}, errors.New("invalid proxy_url")
		}
		tr.Proxy = http.ProxyURL(u)
	}
	c := client{cfg: cfg, http: &http.Client{Timeout: 50 * time.Second, Transport: tr}, base: apiBase}
	updates, err := c.updates(ctx, st.Offset, 2)
	if err != nil {
		return Result{}, err
	}
	if len(updates) > 0 && len(updates) < updateLimit && cfg.BatchTimeoutSeconds > 0 {
		timer := time.NewTimer(time.Duration(min(cfg.BatchTimeoutSeconds, 5)) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-timer.C:
		}
		// Reuse the persisted offset. Advancing it here would acknowledge messages
		// before Crier has saved the tickets and the wallet changes.
		more, pollErr := c.updates(ctx, st.Offset, 0)
		if pollErr == nil && len(more) >= len(updates) {
			updates = more
		}
	}
	var events []Event
	for _, u := range updates {
		if u.UpdateID < st.Offset {
			continue
		}
		st.Offset = u.UpdateID + 1
		m := u.Message
		if m == nil {
			continue
		}
		if strings.HasPrefix(m.Text, "/") {
			cmd, arg, _ := strings.Cut(safe(m.Text), " ")
			cmd = strings.ToLower(strings.SplitN(cmd, "@", 2)[0])
			arg = safe(arg)
			switch cmd {
			case "/start", "/help":
				c.send(ctx, m.Chat.ID, "📠 Telefax/Crier\n/print <文本>\n/qrcode <内容>\n/todo <任务>\n/usage\n/test_printer")
				continue
			case "/usage":
				c.send(ctx, m.Chat.ID, usage(cfg, st, m.From.ID))
				continue
			case "/qrcode":
				if arg == "" {
					c.send(ctx, m.Chat.ID, "❌ 无效内容")
					continue
				}
				if !vip(cfg, m.From.ID) && !consume(cfg, &st, m.From.ID, qrFixedCost) {
					c.send(ctx, m.Chat.ID, "❌ 积分不足")
					continue
				}
				l := ticket.Layout{Blocks: []ticket.Block{{Type: "header", Title: "TELE FAX", Subtitle: user(m)}, {Type: "qr", Data: arg}}}
				events = append(events, Event{ID: msgID(m), GroupKey: groupKey(m), Title: "QR | " + arg, Layout: &l})
				c.logMessage(ctx, m, "Cmd: QR | "+user(m))
				continue
			case "/todo":
				if arg == "" {
					c.send(ctx, m.Chat.ID, "❓ 请在 /todo 后输入您的任务描述。")
					continue
				}
				if !vip(cfg, m.From.ID) {
					c.send(ctx, m.Chat.ID, "❌ 您不是白名单用户，无法使用此功能。")
					continue
				}
				events = append(events, Event{ID: msgID(m), GroupKey: groupKey(m), Title: arg, Body: arg, Template: "todo", AI: "todo"})
				if cfg.TodoChannelID != 0 {
					c.send(ctx, cfg.TodoChannelID, "✅ 新任务："+arg)
				}
				continue
			case "/test_printer":
				if !vip(cfg, m.From.ID) {
					c.send(ctx, m.Chat.ID, "❌ 您不是白名单用户，无法使用此功能。")
					continue
				}
				l := ticket.Layout{Blocks: []ticket.Block{{Type: "header", Title: "TELE FAX", Subtitle: "TEST"}, {Type: "text", Text: "打印机测试\n系统运行正常", Align: "center"}}}
				events = append(events, Event{ID: msgID(m), GroupKey: groupKey(m), Title: "Telegram 打印测试", Layout: &l})
				continue
			case "/print":
				m.Text = arg
			default:
				continue
			}
		}
		e, err := c.message(ctx, m)
		if err != nil {
			if cfg.Log != nil {
				fmt.Fprintf(cfg.Log, "crier-telegram: update %d: %v\n", u.UpdateID, err)
			}
			c.send(ctx, m.Chat.ID, "无法读取或打印这条消息的图片。")
			continue
		}
		if len(e) > 0 {
			cost := float64(len([]rune(safe(m.Text))) + len([]rune(safe(m.Caption))))
			cost += float64(e[0].MediaCost + qrFixedCost*len(inlineQR.FindAllString(safe(m.Text), -1)))
			if !vip(cfg, m.From.ID) && !consume(cfg, &st, m.From.ID, cost) {
				c.send(ctx, m.Chat.ID, "❌ 积分不足")
				continue
			}
			events = append(events, e...)
			c.logMessage(ctx, m, fmt.Sprintf("Batch | %s | Items:1 | Cost:%d", user(m), int(cost)))
		}
	}
	events = groupMessages(events)
	b, err := json.Marshal(st)
	if err != nil {
		return Result{}, err
	}
	return Result{Cursor: string(b), Events: events}, nil
}
func parseState(s string) (cursorState, error) {
	if s == "" {
		return cursorState{}, nil
	}
	var st cursorState
	if json.Unmarshal([]byte(s), &st) == nil && st.Offset >= 0 {
		return st, nil
	}
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n < 0 {
		return cursorState{}, errors.New("invalid cursor")
	}
	return cursorState{Offset: n}, nil
}
func vip(c Config, id int64) bool {
	for _, n := range c.WhitelistUserIDs {
		if n == id {
			return true
		}
	}
	return false
}
func balance(c Config, s cursorState, id int64) float64 {
	if vip(c, id) {
		return c.MaxPoints
	}
	w, ok := s.Wallets[strconv.FormatInt(id, 10)]
	if !ok {
		return c.MaxPoints
	}
	elapsed := float64(time.Now().Unix() - w.Updated)
	if elapsed < 0 {
		elapsed = 0
	}
	return min(c.MaxPoints, w.Points+elapsed*c.RechargePerSec)
}
func consume(c Config, s *cursorState, id int64, cost float64) bool {
	if vip(c, id) || c.MaxPoints <= 0 || c.RechargePerSec <= 0 {
		return true
	}
	b := balance(c, *s, id)
	if b < cost {
		return false
	}
	s.Wallets[strconv.FormatInt(id, 10)] = wallet{b - cost, time.Now().Unix()}
	return true
}
func usage(c Config, s cursorState, id int64) string {
	if vip(c, id) {
		return "👑 VIP 用户积分: ♾️ 无限"
	}
	b := balance(c, s, id)
	wait := int64(0)
	if c.RechargePerSec > 0 && b < c.MaxPoints {
		wait = int64((c.MaxPoints - b) / c.RechargePerSec)
	}
	return fmt.Sprintf("📊 积分: %d / %d\n⏳ 回满需: %d秒", int64(b), int64(c.MaxPoints), wait)
}
func groupKey(m *message) string { return fmt.Sprintf("%d:%d", m.Chat.ID, m.From.ID) }
func msgID(m *message) string    { return fmt.Sprintf("telegram:%d:%d", m.Chat.ID, m.MessageID) }
func user(m *message) string {
	n := strings.TrimSpace(m.From.FirstName + " " + m.From.LastName)
	if m.From.Username != "" {
		n += " @" + m.From.Username
	}
	return fmt.Sprintf("%s (ID:%d)", n, m.From.ID)
}
func safe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
func (c client) call(ctx context.Context, method string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.cfg.Token+"/"+method, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return errors.New("Telegram API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Telegram API returned HTTP %d", resp.StatusCode)
	}
	var env envelope[json.RawMessage]
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env) != nil {
		return errors.New("invalid Telegram API response")
	}
	if !env.OK {
		return errors.New("Telegram API: " + env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}
func (c client) updates(ctx context.Context, offset int64, wait int) ([]update, error) {
	var rows []update
	e := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": wait, "limit": updateLimit, "allowed_updates": []string{"message"}}, &rows)
	return rows, e
}
func (c client) send(ctx context.Context, chat int64, text string) {
	_ = c.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text}, nil)
}
func (c client) logMessage(ctx context.Context, m *message, text string) {
	if c.cfg.LogChannelID == 0 {
		return
	}
	c.send(ctx, c.cfg.LogChannelID, "🖨️ "+text)
	if c.cfg.LogChannelID != m.Chat.ID {
		_ = c.call(ctx, "forwardMessage", map[string]any{"chat_id": c.cfg.LogChannelID, "from_chat_id": m.Chat.ID, "message_id": m.MessageID}, nil)
	}
}
func (c client) file(ctx context.Context, id string) ([]byte, error) {
	var f struct {
		FilePath string `json:"file_path"`
	}
	if e := c.call(ctx, "getFile", map[string]any{"file_id": id}, &f); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/file/bot"+c.cfg.Token+"/"+strings.TrimLeft(f.FilePath, "/"), nil)
	if e != nil {
		return nil, e
	}
	r, e := c.http.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("Telegram file returned HTTP %d", r.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, maxMediaBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxMediaBytes {
		return nil, errors.New("media exceeds 5 MiB")
	}
	return b, nil
}
func mediaID(m *message) string {
	if m.Sticker != nil {
		return m.Sticker.FileID
	}
	if len(m.Photo) > 0 {
		return m.Photo[len(m.Photo)-1].FileID
	}
	return ""
}

var inlineQR = regexp.MustCompile(`(?i)\[qr:([^\]\r\n]+)\]`)

func (c client) message(ctx context.Context, m *message) ([]Event, error) {
	text := safe(m.Text)
	if text == "" && len(m.Photo) == 0 && m.Sticker == nil {
		return nil, nil
	}
	blocks := []ticket.Block{{Type: "header", Title: "TELE FAX", Subtitle: user(m)}}
	if cap := safe(m.Caption); cap != "" {
		blocks = append(blocks, ticket.Block{Type: "text", Text: cap, Style: "bold"})
	}
	start := 0
	for _, match := range inlineQR.FindAllStringSubmatchIndex(text, -1) {
		if match[0] > start {
			blocks = append(blocks, ticket.Block{Type: "text", Text: text[start:match[0]]})
		}
		blocks = append(blocks, ticket.Block{Type: "qr", Data: text[match[2]:match[3]]})
		start = match[1]
	}
	if start < len(text) {
		blocks = append(blocks, ticket.Block{Type: "text", Text: text[start:]})
	}
	mediaCost := 0
	if len(m.Photo) > 0 || m.Sticker != nil {
		raw, e := c.file(ctx, mediaID(m))
		if e != nil {
			return nil, e
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 8_000_000 {
			return nil, errors.New("invalid or oversized image")
		}
		im, _, e := image.Decode(bytes.NewReader(raw))
		if e != nil {
			return nil, errors.New("unsupported image format")
		}
		width := max(1, min(384, 1800*cfg.Width/cfg.Height))
		height := max(1, min(1800, width*cfg.Height/cfg.Width))
		scaled := image.NewGray(image.Rect(0, 0, 384, height))
		for i := range scaled.Pix {
			scaled.Pix[i] = 255
		}
		left := (384 - width) / 2
		draw.CatmullRom.Scale(scaled, image.Rect(left, 0, left+width, height), im, im.Bounds(), draw.Over, nil)
		chars, line := c.cfg.ImageCharsPerLine, c.cfg.LineHeightPx
		if chars <= 0 {
			chars = 16
		}
		if line <= 0 {
			line = 32
		}
		mediaCost = max(1, int(float64(cfg.Height)*384/float64(cfg.Width)*float64(chars)/float64(line)))
		var b bytes.Buffer
		if e = png.Encode(&b, scaled); e != nil {
			return nil, e
		}
		blocks = append(blocks, ticket.Block{Type: "image", Image: base64.StdEncoding.EncodeToString(b.Bytes())})
	}
	if len(blocks) == 1 {
		return nil, nil
	}
	title := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if title == "" {
		title = "Telegram media"
	}
	return []Event{{ID: msgID(m), GroupKey: groupKey(m), Title: title, MediaCost: mediaCost, Layout: &ticket.Layout{Blocks: blocks}}}, nil
}
func groupMessages(events []Event) []Event {
	var out []Event
	var blocks []ticket.Block
	var ids []string
	var key, title string
	flush := func() {
		if len(blocks) == 0 {
			return
		}
		out = append(out, Event{ID: strings.Join(ids, ","), Title: title, Layout: &ticket.Layout{Blocks: blocks}})
		blocks = nil
		ids = nil
		key = ""
		title = ""
	}
	for _, e := range events {
		if e.Layout == nil {
			flush()
			out = append(out, e)
			continue
		}
		if e.Title == "Telegram 打印测试" {
			flush()
			out = append(out, e)
			continue
		}
		k := e.GroupKey
		if len(blocks) > 0 && k != key {
			flush()
		}
		if len(blocks) == 0 {
			key = k
			title = e.Title
		}
		if len(ids) > 0 && (len(ids) >= 4 || blockBytes(blocks)+blockBytes(e.Layout.Blocks) > 3000) {
			flush()
			key = k
			title = e.Title
		}
		blocks = append(blocks, e.Layout.Blocks...)
		ids = append(ids, e.ID)
	}
	flush()
	return out
}
func blockBytes(b []ticket.Block) int {
	n := 0
	for _, x := range b {
		n += len(x.Text) + len(x.Image) + len(x.Data) + len(x.Title) + len(x.Subtitle)
	}
	return n
}
