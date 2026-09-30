package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
)

// Message is one transactional email. Text is the plain-text body; when Heading is
// set the SMTP mailer also sends a branded HTML version (multipart/alternative).
type Message struct {
	To      string
	Subject string
	Text    string

	Heading string   // big title in the HTML version
	Lines   []string // paragraphs above the button
	Button  *Button  // primary call to action
	Code    string   // a one-time code shown large (OTP)
	Footer  string   // small print under the button

	// HTML is a ready-made body (CRM emails, campaigns); it replaces the branded layout.
	HTML     string
	FromName string            // display name instead of the configured sender's
	ReplyTo  string            // where replies go
	Headers  map[string]string // extra headers (List-Unsubscribe …)
}

type Button struct {
	Label string
	URL   string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
	// Mode is "smtp" or "console" (emails only written to the log).
	Mode() string
	From() string
}

// New picks the Brevo HTTPS mailer when CRM_BREVO_API_KEY is set, else the SMTP mailer
// when CRM_SMTP_HOST is set, else the console mailer, which logs the whole message so
// reset links are usable in local dev (D-16).
func New(cfg shared.Config) Mailer {
	if cfg.BrevoAPIKey != "" {
		return newBrevo(cfg)
	}
	if cfg.SMTPHost == "" {
		return consoleMailer{from: cfg.SMTPFrom}
	}
	// Google shows app passwords as "abcd efgh ijkl mnop"; the spaces aren't part of it.
	cfg.SMTPPass = strings.ReplaceAll(cfg.SMTPPass, " ", "")
	return smtpMailer{cfg: cfg}
}

type consoleMailer struct{ from string }

func (consoleMailer) Mode() string   { return "console" }
func (c consoleMailer) From() string { return c.from }

func (consoleMailer) Send(_ context.Context, m Message) error {
	slog.Info("CRM email (console mailer — set CRM_SMTP_HOST to send for real)", "to", m.To, "subject", m.Subject, "body", plainText(m))
	return nil
}

type smtpMailer struct{ cfg shared.Config }

func (smtpMailer) Mode() string { return "smtp" }

// CheckSMTP dials the SMTP server once so a blocked port shows up in the logs at startup
// instead of as silently missing emails.
func CheckSMTP(cfg shared.Config) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort), 8*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
func (s smtpMailer) From() string { return s.cfg.SMTPFrom }

func (s smtpMailer) Send(ctx context.Context, m Message) error {
	from, err := netmail.ParseAddress(s.cfg.SMTPFrom)
	if err != nil {
		from = &netmail.Address{Address: extractAddress(s.cfg.SMTPFrom)}
	}
	raw, err := build(from, m, s.cfg.AppName)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}
	done := make(chan error, 1)
	go func() {
		// SendMail upgrades to TLS (STARTTLS) when the server offers it, as Gmail does on 587.
		done <- smtp.SendMail(addr, auth, from.Address, []string{m.To}, raw)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func plainText(m Message) string {
	if m.Text != "" {
		return m.Text
	}
	var b strings.Builder
	for _, l := range m.Lines {
		b.WriteString(l + "\n\n")
	}
	if m.Code != "" {
		b.WriteString("Your code: " + m.Code + "\n\n")
	}
	if m.Button != nil {
		b.WriteString(m.Button.Label + ": " + m.Button.URL + "\n\n")
	}
	if m.Footer != "" {
		b.WriteString(m.Footer + "\n")
	}
	return b.String()
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func qp(s string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.String()
}

func build(from *netmail.Address, m Message, appName string) ([]byte, error) {
	domain := "localhost"
	if i := strings.LastIndex(from.Address, "@"); i >= 0 {
		domain = from.Address[i+1:]
	}
	var buf bytes.Buffer
	h := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	sender := *from
	if m.FromName != "" {
		sender.Name = m.FromName
	}
	h("From", sender.String())
	h("To", m.To)
	if m.ReplyTo != "" {
		h("Reply-To", m.ReplyTo)
	}
	for k, v := range m.Headers {
		if !strings.ContainsAny(k+v, "\r\n") {
			h(k, v)
		}
	}
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("Message-ID", "<"+randomID()+"@"+domain+">")
	h("MIME-Version", "1.0")

	text := plainText(m)
	if m.Heading == "" && m.HTML == "" {
		h("Content-Type", "text/plain; charset=UTF-8")
		h("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n" + qp(text))
		return buf.Bytes(), nil
	}
	html := m.HTML
	if html == "" {
		var err error
		if html, err = renderHTML(m, appName); err != nil {
			return nil, err
		}
	}
	boundary := "crm-" + randomID()
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	buf.WriteString("\r\n")
	for _, part := range []struct{ ctype, body string }{{"text/plain", text}, {"text/html", html}} {
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: " + part.ctype + "; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(qp(part.body) + "\r\n")
	}
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes(), nil
}

// Layout uses tables + inline styles so it renders the same in Gmail, Outlook and Apple
// Mail. The <style> block only adds progressive enhancements (animation, dark mode) in
// clients that support it (Apple Mail / iOS); others ignore it and keep the static look.
var htmlTemplate = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark"><meta name="supported-color-schemes" content="light dark">
<title>{{.M.Subject}}</title>
<style>
  @keyframes rise { from { opacity:0; transform:translateY(14px) } to { opacity:1; transform:none } }
  @keyframes shine { 0% { background-position:-180px 0 } 60%,100% { background-position:340px 0 } }
  @keyframes pop { 0% { transform:scale(.6); opacity:0 } 70% { transform:scale(1.06) } 100% { transform:scale(1); opacity:1 } }
  .card { animation: rise .7s cubic-bezier(.16,1,.3,1) both }
  .badge { animation: pop .6s .15s cubic-bezier(.34,1.56,.64,1) both }
  .btn { background-image:linear-gradient(110deg,rgba(255,255,255,0) 30%,rgba(255,255,255,.35) 50%,rgba(255,255,255,0) 70%) !important;
         background-repeat:no-repeat !important; background-size:180px 100% !important; animation: shine 2.8s 1s ease-in-out infinite }
  .code { animation: rise .6s .25s cubic-bezier(.16,1,.3,1) both }
  @media (prefers-reduced-motion: reduce) { .card,.badge,.btn,.code { animation:none !important } }
  @media (prefers-color-scheme: dark) {
    .bg { background:#0b0b12 !important } .card { background:#15151f !important; border-color:#27273a !important }
    .h1 { color:#f4f4f5 !important } .p { color:#c4c4cf !important } .muted { color:#8b8ba0 !important }
    .codebox { background:#1e1e2d !important; color:#f4f4f5 !important }
  }
  @media (max-width:560px) { .pad { padding-left:22px !important; padding-right:22px !important } }
</style></head>
<body style="margin:0;padding:0;background:#f3f3f8" class="bg">
<span style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent">{{.Preheader}}</span>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" class="bg" style="background:#f3f3f8;padding:36px 12px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<tr><td align="center">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" class="card" style="max-width:540px;background:#ffffff;border:1px solid #e7e7ef;border-radius:16px;box-shadow:0 12px 40px -18px rgba(49,46,129,.35);overflow:hidden">
    <tr><td style="background:#4f46e5;background-image:linear-gradient(135deg,#4338ca 0%,#6d28d9 55%,#7c3aed 100%);padding:26px 32px" class="pad">
      <table role="presentation" cellpadding="0" cellspacing="0"><tr>
        <td class="badge" style="width:40px;height:40px;border-radius:11px;background:rgba(255,255,255,.18);text-align:center;vertical-align:middle;font-size:18px;font-weight:800;color:#ffffff">{{.Initial}}</td>
        <td style="padding-left:12px;font-size:16px;font-weight:700;color:#ffffff;letter-spacing:.2px">{{.App}}</td>
      </tr></table>
    </td></tr>
    <tr><td style="padding:30px 32px 6px" class="pad">
      <h1 class="h1" style="margin:0 0 14px;font-size:23px;line-height:30px;font-weight:700;color:#18181b">{{.M.Heading}}</h1>
      {{range .M.Lines}}<p class="p" style="margin:0 0 12px;font-size:15px;line-height:24px;color:#3f3f46">{{.}}</p>{{end}}
    </td></tr>
    {{if .M.Code}}<tr><td style="padding:6px 32px 8px" class="pad">
      <div class="code codebox" style="font-family:'SF Mono',Menlo,Consolas,monospace;font-size:34px;letter-spacing:10px;font-weight:700;color:#18181b;background:#f4f4fb;border:1px dashed #c7c7e0;border-radius:12px;padding:18px 12px;text-align:center">{{.M.Code}}</div>
    </td></tr>{{end}}
    {{if .M.Button}}<tr><td style="padding:14px 32px 6px" class="pad">
      <table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:10px;background:#4f46e5">
        <a href="{{.M.Button.URL}}" class="btn" style="display:inline-block;padding:13px 26px;border-radius:10px;background-color:#4f46e5;color:#ffffff;font-size:15px;font-weight:600;text-decoration:none">{{.M.Button.Label}} &rarr;</a>
      </td></tr></table>
      <p class="muted" style="margin:16px 0 0;font-size:12px;line-height:18px;color:#71717a">Button not working? Paste this link into your browser:<br>
        <a href="{{.M.Button.URL}}" style="color:#4f46e5;word-break:break-all">{{.M.Button.URL}}</a></p>
    </td></tr>{{end}}
    <tr><td style="padding:22px 32px 28px" class="pad">
      <div style="height:1px;background:#ececf3;margin-bottom:16px"></div>
      {{if .M.Footer}}<p class="muted" style="margin:0;font-size:12px;line-height:19px;color:#71717a">{{.M.Footer}}</p>{{end}}
    </td></tr>
  </table>
  <p class="muted" style="margin:18px 0 0;font-size:11px;line-height:16px;color:#a1a1aa">Sent by {{.App}} &middot; You're getting this because of activity on your account.</p>
</td></tr></table>
</body></html>`))

func renderHTML(m Message, appName string) (string, error) {
	var buf bytes.Buffer
	pre := ""
	if len(m.Lines) > 0 {
		pre = m.Lines[0]
	}
	initial := "C"
	if appName != "" {
		initial = strings.ToUpper(appName[:1])
	}
	err := htmlTemplate.Execute(&buf, map[string]any{"M": m, "App": appName, "Preheader": pre, "Initial": initial})
	return buf.String(), err
}

// extractAddress turns `Name <a@b.c>` into `a@b.c`.
func extractAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.LastIndex(from, ">"); j > i {
			return from[i+1 : j]
		}
	}
	return from
}
