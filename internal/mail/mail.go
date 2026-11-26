// Package mail turns new messages in an IMAP mailbox into crier events.
//
// One plugin instance owns one mailbox on one account. It remembers the last
// message it handed over in a cursor, so a message is printed once and mail
// that was already in the mailbox is never printed at all.
package mail

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	// maxMessagesPerPoll bounds one poll, so a mailbox left alone for a week
	// drains over several polls instead of one huge fetch.
	maxMessagesPerPoll = 10
	// maxMessageBytes bounds the message downloaded for one event.
	maxMessageBytes = 1 << 20
	// maxBodyRunes bounds the printed text of one event.
	maxBodyRunes = 4000

	dialTimeout = 20 * time.Second
)

// Every email event is a reminder for crier to summarise.
const (
	source   = "email"
	template = "mail"
	ai       = "classify_summarize"
)

// ErrCursor reports a cursor that was not produced by an earlier poll.
var ErrCursor = errors.New("invalid cursor")

// Config is one mailbox to poll.
type Config struct {
	// Address is the IMAP server as host:port. The connection uses implicit
	// TLS with the system roots.
	Address  string
	Username string
	Secret   string // password or app password
	// Mailbox is the mailbox name, INBOX when empty.
	Mailbox string
	// Log receives non-fatal diagnostics. It never receives credentials, and
	// may be nil.
	Log io.Writer
}

// Event is one mail message as crier sees it.
type Event struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Source   string            `json:"source"`
	Time     string            `json:"time"`
	Template string            `json:"template"`
	AI       string            `json:"ai"`
	Fields   map[string]string `json:"fields,omitempty"`
}

// Result is one poll: the mail that arrived since the cursor, and the cursor
// to pass to the next poll.
type Result struct {
	Cursor string  `json:"cursor"`
	Events []Event `json:"events"`
}

// Test checks that the server accepts the credentials and the mailbox can be
// selected. It returns the selected mailbox name.
func Test(cfg Config) (string, error) {
	client, err := dialLogin(cfg)
	if err != nil {
		return "", err
	}
	defer client.Close()

	mailbox := cfg.mailboxName()
	if _, err := client.Select(mailbox, nil).Wait(); err != nil {
		return "", fmt.Errorf("select %s: %w", mailbox, err)
	}
	return mailbox, nil
}

// Poll returns the mail that arrived since cursor, and the cursor to use next
// time. An empty cursor only records the current end of the mailbox, so the
// mail that is already there is not printed.
func Poll(cfg Config, cursorText string) (Result, error) {
	prev, err := parseCursor(cursorText)
	if err != nil {
		return Result{}, err
	}

	client, err := dialLogin(cfg)
	if err != nil {
		return Result{}, err
	}
	defer client.Close()

	p := &poller{client: client, mailbox: cfg.mailboxName(), log: cfg.Log}
	return p.poll(prev, time.Now())
}

func dialLogin(cfg Config) (*imapclient.Client, error) {
	client, err := imapclient.DialTLS(cfg.Address, &imapclient.Options{
		Dialer: &net.Dialer{Timeout: dialTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", cfg.Address, err)
	}
	if err := client.Login(cfg.Username, cfg.Secret).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login %s: %w", cfg.Username, err)
	}
	return client, nil
}

// poller polls one selected mailbox of an open client.
type poller struct {
	client   *imapclient.Client
	mailbox  string
	log      io.Writer
	validity uint32
}

func (p *poller) poll(prev *cursor, now time.Time) (Result, error) {
	sel, err := p.client.Select(p.mailbox, nil).Wait()
	if err != nil {
		return Result{}, fmt.Errorf("select %s: %w", p.mailbox, err)
	}
	p.validity = sel.UIDValidity

	if prev == nil || prev.uidValidity != sel.UIDValidity {
		// First poll, or the mailbox was recreated and its UIDs reset: what is
		// in there now is history, not news.
		return Result{Cursor: formatCursor(p.validity, lastUID(sel.UIDNext)), Events: []Event{}}, nil
	}

	uids, err := p.newUIDs(prev.uid, sel.UIDNext)
	if err != nil {
		return Result{}, err
	}
	next := prev.uid
	if len(uids) > 0 {
		next = uids[len(uids)-1]
	}
	events, err := p.events(uids, now)
	if err != nil {
		return Result{}, err
	}
	return Result{Cursor: formatCursor(p.validity, next), Events: events}, nil
}

// newUIDs lists the UIDs above after, oldest first, capped at one poll.
func (p *poller) newUIDs(after, uidNext imap.UID) ([]imap.UID, error) {
	if uidNext <= after+1 {
		return nil, nil
	}
	first, last := after+1, uidNext-1

	data, err := p.client.UIDSearch(&imap.SearchCriteria{
		UID: []imap.UIDSet{{{Start: first, Stop: last}}},
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search UID %d:%d: %w", first, last, err)
	}

	uids := data.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > maxMessagesPerPoll {
		uids = uids[:maxMessagesPerPoll]
	}
	return uids, nil
}

func (p *poller) events(uids []imap.UID, now time.Time) ([]Event, error) {
	events := []Event{}
	if len(uids) == 0 {
		return events, nil
	}

	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(uid)
	}
	messages, err := p.client.Fetch(set, &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		RFC822Size:   true,
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch %d message(s): %w", len(uids), err)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].UID < messages[j].UID })

	for _, msg := range messages {
		body := ""
		if msg.RFC822Size > maxMessageBytes {
			p.logf("uid %d is %d bytes, body skipped", msg.UID, msg.RFC822Size)
		} else {
			var err error
			body, err = p.body(msg.UID)
			if err != nil {
				return nil, err
			}
		}
		events = append(events, eventFor(p.validity, msg, body, now))
	}
	return events, nil
}

// body downloads and decodes the text of one message.
func (p *poller) body(uid imap.UID) (string, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	var set imap.UIDSet
	set.AddNum(uid)

	buffers, err := p.client.Fetch(set, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return "", fmt.Errorf("fetch message %d: %w", uid, err)
	}
	if len(buffers) == 0 {
		return "", nil
	}

	text, err := textBody(bytes.NewReader(buffers[0].FindBodySection(section)), maxBodyRunes)
	if err != nil {
		// A part crier cannot decode must not cost the whole message.
		p.logf("uid %d: %v", uid, err)
	}
	return text, nil
}

func (p *poller) logf(format string, args ...any) {
	if p.log == nil {
		return
	}
	fmt.Fprintf(p.log, "crier-email: "+format+"\n", args...)
}

func eventFor(validity uint32, msg *imapclient.FetchMessageBuffer, body string, now time.Time) Event {
	event := Event{
		ID:       fmt.Sprintf("%d:%d", validity, msg.UID),
		Title:    "(no subject)",
		Body:     body,
		Source:   source,
		Template: template,
		AI:       ai,
	}

	when := msg.InternalDate
	if env := msg.Envelope; env != nil {
		if subject := strings.TrimSpace(env.Subject); subject != "" {
			event.Title = subject
		}
		if len(env.From) > 0 {
			sender := env.From[0]
			label := sender.Name
			if label == "" && sender.Host != "" {
				label = sender.Mailbox + "@" + sender.Host
			}
			event.Fields = map[string]string{"sender": label}
		}
		if !env.Date.IsZero() {
			when = env.Date
		}
	}
	if when.IsZero() {
		when = now
	}
	event.Time = when.Format(time.RFC3339)
	return event
}

// cursor is the position of the last delivered message.
type cursor struct {
	uidValidity uint32
	uid         imap.UID
}

func parseCursor(text string) (*cursor, error) {
	if text == "" {
		return nil, nil
	}
	validityText, uidText, ok := strings.Cut(text, "-")
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrCursor, text)
	}
	validity, err := strconv.ParseUint(validityText, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("%w %q", ErrCursor, text)
	}
	uid, err := strconv.ParseUint(uidText, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("%w %q", ErrCursor, text)
	}
	return &cursor{uidValidity: uint32(validity), uid: imap.UID(uid)}, nil
}

func formatCursor(validity uint32, uid imap.UID) string {
	return fmt.Sprintf("%d-%d", validity, uid)
}

// lastUID is the highest UID that can exist in a mailbox announcing uidNext.
func lastUID(uidNext imap.UID) imap.UID {
	if uidNext == 0 {
		return 0
	}
	return uidNext - 1
}

// mailboxName is the configured mailbox, INBOX by default.
func (c Config) mailboxName() string {
	if c.Mailbox == "" {
		return "INBOX"
	}
	return c.Mailbox
}
