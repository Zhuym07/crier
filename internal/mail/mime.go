package mail

import (
	"html"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message/mail"
)

// textBody returns the readable text of a message, preferring the text/plain
// part and falling back to text/html. Attachments are ignored. The result is
// bounded to maxRunes characters.
func textBody(r io.Reader, maxRunes int) (string, error) {
	reader, err := mail.CreateReader(r)
	if reader == nil {
		return "", err
	}
	defer reader.Close()

	var plain, markup string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if part == nil {
			// The message is unreadable beyond this part; keep what we have.
			return plain, err
		}

		header, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue // an attachment, not text
		}
		contentType, _, _ := header.ContentType()
		switch contentType {
		case "text/plain":
			if plain == "" {
				plain, _ = readText(part.Body, maxRunes)
			}
		case "text/html":
			if markup == "" {
				markup, _ = readText(part.Body, maxRunes)
			}
		}
	}

	if plain != "" {
		return plain, nil
	}
	return htmlText(markup), nil
}

// readText reads at most maxRunes characters, so a huge part is not copied.
func readText(r io.Reader, maxRunes int) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, int64(maxRunes)*utf8.UTFMax))
	text := strings.TrimSpace(string(raw))
	if runes := []rune(text); len(runes) > maxRunes {
		text = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return text, err
}

var htmlTag = regexp.MustCompile(`(?s)<[^>]*>`)

// htmlText reduces an HTML part to the text a receipt can print.
func htmlText(markup string) string {
	text := htmlTag.ReplaceAllString(markup, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}
