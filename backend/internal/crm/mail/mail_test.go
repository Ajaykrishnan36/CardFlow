package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"cardflow-backend/internal/crm/shared"
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

func TestBrevoSendPayload(t *testing.T) {
	var got map[string]any
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("api-key")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"x"}`))
	}))
	defer srv.Close()
	b := newBrevo(shared.Config{BrevoAPIKey: "k1", SMTPFrom: "Ajay's CRM <me@example.com>", AppName: "Ajay's CRM"})
	b.client = &http.Client{Transport: rewrite{srv.URL}}
	err := b.Send(context.Background(), Message{To: "you@example.com", Subject: "123456 is your code", Heading: "Your sign-in code", Code: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "k1" || got["subject"] != "123456 is your code" {
		t.Fatalf("key=%q body=%v", key, got)
	}
	if sender := got["sender"].(map[string]any); sender["email"] != "me@example.com" || sender["name"] != "Ajay's CRM" {
		t.Fatalf("sender=%v", sender)
	}
	if html, _ := got["htmlContent"].(string); !strings.Contains(html, "123456") {
		t.Fatal("html missing code")
	}
}

type rewrite struct{ base string }

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(rw.base)
	r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(r)
}
