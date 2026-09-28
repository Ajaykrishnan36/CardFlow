package mail

import (
	"strings"
	"testing"
)

func TestHTMLEmailRendersAllParts(t *testing.T) {
	html, err := renderHTML(Message{
		Subject: "Test", Heading: "Reset your password", Lines: []string{"Hi Ajay <script>"}, Code: "123456",
		Button: &Button{Label: "Choose a new password", URL: "https://example.com/r?token=a&b=c"}, Footer: "Expires soon.",
	}, "Ajay's CRM")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Reset your password", "123456", "Choose a new password", "https://example.com/r?token=a&amp;b=c", "Expires soon.", "&lt;script&gt;"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
