package mail

import (
	"strings"
	"testing"
)

func TestMailPrefersPlainText(t *testing.T) {
	raw := strings.Join([]string{
		"MIME-Version: 1.0", "Content-Type: multipart/alternative; boundary=parts", "",
		"--parts", "Content-Type: text/html; charset=utf-8", "", "<p>Alternate</p>",
		"--parts", "Content-Type: text/plain; charset=utf-8", "", "课程通知", "--parts--", "",
	}, "\r\n")
	body, err := textBody(strings.NewReader(raw), 4)
	if err != nil || body != "课程通知" {
		t.Fatalf("got %q, %v", body, err)
	}
}
