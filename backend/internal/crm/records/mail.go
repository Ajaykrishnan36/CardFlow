package records

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	netmail "net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/oauth"
	"cardflow-backend/internal/crm/shared"
	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

// Email & calendar (D-65). People connect their own mailbox — Gmail or Outlook with one
// click, or any IMAP/SMTP account — per workspace. The CRM syncs emails with people it
// knows (contacts, leads, accounts by email address) onto their timelines, optionally
// creates contacts for new correspondents, and syncs calendar events into the Calendar
// module. Emails are sent from a record through the person's mailbox, or through the
// CRM's own address when they have none. Mailbox owners choose how much colleagues see
// (everything, subject only, or only that an email happened).

type emailInput struct {
	To        []string   `json:"to"`
	Cc        []string   `json:"cc"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body"` // plain text; line breaks kept
	HTML      string     `json:"html,omitempty"`
	MailboxID *uuid.UUID `json:"mailboxId,omitempty"`
	ReplyTo   *uuid.UUID `json:"replyTo,omitempty"` // a message on the record this answers (D-80)
}

// threadHeaders carry what a reply needs to land in the same conversation.
type threadHeaders struct {
	MessageID   string // our RFC Message-ID for the new email
	InReplyTo   string // the parent's RFC Message-ID, when known
	GmailThread string // Gmail thread to add the email to
	GraphParent string // Outlook message id to reply to
}

func (t threadHeaders) headers() map[string]string {
	out := map[string]string{}
	if t.MessageID != "" {
		out["Message-ID"] = t.MessageID
	}
	if t.InReplyTo != "" {
		out["In-Reply-To"] = t.InReplyTo
		out["References"] = t.InReplyTo
	}
	return out
}

type recordLink struct {
	Object string
	ID     uuid.UUID
}

func textToHTML(s string) string {
	paras := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n")
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:1.55;color:#18181b">`)
	for _, p := range paras {
		b.WriteString("<p style=\"margin:0 0 12px\">" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
	}
	b.WriteString("</div>")
	return b.String()
}

func cleanAddresses(list []string) ([]string, error) {
	out := []string{}
	for _, raw := range list {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			a, err := netmail.ParseAddress(part)
			if err != nil {
				return nil, fmt.Errorf("%q isn't an email address", part)
			}
			out = append(out, strings.ToLower(a.Address))
		}
	}
	if len(out) > 50 {
		return nil, errors.New("send to at most 50 people at once")
	}
	return out, nil
}

type mailbox struct {
	ID         uuid.UUID
	Workspace  uuid.UUID
	IdentityID uuid.UUID
	Provider   string
	Email      string
	Name       string
	Creds      mailCreds
	Status     string
	SyncEmail  bool
	SyncCal    bool
	AutoCreate bool
	Cursor     map[string]any
}

type mailCreds struct {
	Token *oauth2.Token `json:"token,omitempty"`
	IMAP  *serverCreds  `json:"imap,omitempty"`
	SMTP  *serverCreds  `json:"smtp,omitempty"`
}

type serverCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) loadMailbox(ctx context.Context, q querier, id uuid.UUID) (*mailbox, error) {
	m := &mailbox{}
	var enc, cursor []byte
	err := q.QueryRow(ctx, `SELECT id, workspace_id, identity_id, provider, email, display_name, credentials_enc, status, sync_email, sync_calendar, auto_create_contacts, cursor
		FROM crm.mail_accounts WHERE id = $1`, id).Scan(&m.ID, &m.Workspace, &m.IdentityID, &m.Provider, &m.Email, &m.Name, &enc, &m.Status, &m.SyncEmail, &m.SyncCal, &m.AutoCreate, &cursor)
	if err != nil {
		return nil, shared.NotFound("mailbox_not_found")
	}
	raw, err := shared.Decrypt(h.cfg.EncryptionKey, enc)
	if err != nil {
		return nil, errors.New("the mailbox credentials can't be read; connect it again")
	}
	_ = json.Unmarshal(raw, &m.Creds)
	_ = json.Unmarshal(cursor, &m.Cursor)
	if m.Cursor == nil {
		m.Cursor = map[string]any{}
	}
	return m, nil
}

func (h *Handler) saveMailboxCreds(ctx context.Context, m *mailbox) {
	raw, _ := json.Marshal(m.Creds)
	enc, err := shared.Encrypt(h.cfg.EncryptionKey, raw)
	if err == nil {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.mail_accounts SET credentials_enc = $2, updated_at = now() WHERE id = $1`, m.ID, enc)
	}
}

// httpClient is an OAuth client whose refreshed tokens are saved back.
func (h *Handler) mailboxHTTP(ctx context.Context, m *mailbox) (*http.Client, error) {
	p := oauth.Get(m.Provider, h.cfg.BaseURL)
	if p == nil || m.Creds.Token == nil {
		return nil, fmt.Errorf("%s isn't set up on this server (missing client ID)", m.Provider)
	}
	ts := p.TokenSource(ctx, m.Creds.Token)
	tok, err := ts.Token()
	if err != nil {
		return nil, errors.New("the mailbox needs to be connected again (its access expired)")
	}
	if tok.AccessToken != m.Creds.Token.AccessToken {
		m.Creds.Token = tok
		h.saveMailboxCreds(ctx, m)
	}
	c := oauth2.NewClient(ctx, oauth2.StaticTokenSource(tok))
	c.Timeout = 20 * time.Second
	return c, nil
}

func buildRFC822(from, fromName string, to, cc []string, subject, htmlBody, textBody string, extra map[string]string) []byte {
	var b bytes.Buffer
	addr := (&netmail.Address{Name: fromName, Address: from}).String()
	b.WriteString("From: " + addr + "\r\n")
	for _, k := range []string{"Message-ID", "In-Reply-To", "References"} {
		if v := extra[k]; v != "" && !strings.ContainsAny(v, "\r\n") {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		b.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	boundary := "crm" + strings.ReplaceAll(uuid.NewString(), "-", "")
	b.WriteString(`Content-Type: multipart/alternative; boundary="` + boundary + "\"\r\n\r\n")
	for _, part := range []struct{ ct, body string }{{"text/plain", textBody}, {"text/html", htmlBody}} {
		b.WriteString("--" + boundary + "\r\nContent-Type: " + part.ct + "; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		enc := base64.StdEncoding.EncodeToString([]byte(part.body))
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

// sendThroughMailbox sends from a connected mailbox; it returns the provider's message
// and thread ids when the provider gives them.
func (h *Handler) sendThroughMailbox(ctx context.Context, m *mailbox, to, cc []string, subject, htmlBody, textBody string, th threadHeaders) (string, string, error) {
	switch m.Provider {
	case "google":
		c, err := h.mailboxHTTP(ctx, m)
		if err != nil {
			return "", "", err
		}
		raw := base64.RawURLEncoding.EncodeToString(buildRFC822(m.Email, m.Name, to, cc, subject, htmlBody, textBody, th.headers()))
		req := map[string]string{"raw": raw}
		if th.GmailThread != "" {
			req["threadId"] = th.GmailThread
		}
		body, _ := json.Marshal(req)
		res, err := c.Post("https://gmail.googleapis.com/gmail/v1/users/me/messages/send", "application/json", bytes.NewReader(body))
		if err != nil {
			return "", "", err
		}
		defer res.Body.Close()
		var out struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode >= 300 {
			return "", "", fmt.Errorf("Gmail refused the email (%s)", res.Status)
		}
		return out.ID, out.ThreadID, nil
	case "microsoft":
		c, err := h.mailboxHTTP(ctx, m)
		if err != nil {
			return "", "", err
		}
		rcpt := func(list []string) []map[string]any {
			out := []map[string]any{}
			for _, a := range list {
				out = append(out, map[string]any{"emailAddress": map[string]string{"address": a}})
			}
			return out
		}
		msg := map[string]any{"subject": subject, "body": map[string]string{"contentType": "HTML", "content": htmlBody},
			"toRecipients": rcpt(to), "ccRecipients": rcpt(cc)}
		endpoint := "https://graph.microsoft.com/v1.0/me/sendMail"
		payload := map[string]any{"message": msg, "saveToSentItems": true}
		if th.GraphParent != "" {
			// Replying keeps Outlook's conversation; Outlook sets the subject itself.
			delete(msg, "subject")
			endpoint = "https://graph.microsoft.com/v1.0/me/messages/" + url.PathEscape(th.GraphParent) + "/reply"
			payload = map[string]any{"message": msg}
		}
		body, _ := json.Marshal(payload)
		res, err := c.Post(endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			return "", "", err
		}
		res.Body.Close()
		if res.StatusCode >= 300 {
			return "", "", fmt.Errorf("Outlook refused the email (%s)", res.Status)
		}
		return "", "", nil
	case "imap":
		s := m.Creds.SMTP
		if s == nil {
			return "", "", errors.New("this mailbox has no SMTP server for sending")
		}
		msg := buildRFC822(m.Email, m.Name, to, cc, subject, htmlBody, textBody, th.headers())
		return "", "", smtpSend(ctx, s, m.Email, append(append([]string{}, to...), cc...), msg)
	}
	return "", "", errors.New("unknown mailbox type")
}

func smtpSend(ctx context.Context, s *serverCreds, from string, rcpts []string, msg []byte) error {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	done := make(chan error, 1)
	go func() {
		var c *smtp.Client
		var err error
		if s.Port == 465 {
			conn, e := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: s.Host})
			if e != nil {
				done <- e
				return
			}
			c, err = smtp.NewClient(conn, s.Host)
		} else {
			conn, e := net.DialTimeout("tcp", addr, 10*time.Second)
			if e != nil {
				done <- e
				return
			}
			c, err = smtp.NewClient(conn, s.Host)
			if err == nil {
				if ok, _ := c.Extension("STARTTLS"); ok {
					err = c.StartTLS(&tls.Config{ServerName: s.Host})
				}
			}
		}
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		if s.Username != "" {
			if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
				done <- errors.New("the SMTP server didn't accept the username or password")
				return
			}
		}
		if err := c.Mail(from); err != nil {
			done <- err
			return
		}
		for _, r := range rcpts {
			if err := c.Rcpt(r); err != nil {
				done <- err
				return
			}
		}
		w, err := c.Data()
		if err != nil {
			done <- err
			return
		}
		if _, err := w.Write(msg); err != nil {
			done <- err
			return
		}
		done <- w.Close()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return errors.New("the SMTP server didn't answer in time")
	}
}

// sendEmail sends one email and records it (linked to the record and to every record
// whose email is among the recipients).
func (h *Handler) sendEmail(ctx context.Context, ws uuid.UUID, in emailInput, sender *uuid.UUID, link *recordLink, source string) (uuid.UUID, error) {
	to, err := cleanAddresses(in.To)
	if err != nil {
		return uuid.Nil, shared.Validation(map[string]string{"to": err.Error() + "."})
	}
	cc, err := cleanAddresses(in.Cc)
	if err != nil {
		return uuid.Nil, shared.Validation(map[string]string{"cc": err.Error() + "."})
	}
	if len(to) == 0 {
		return uuid.Nil, shared.Validation(map[string]string{"to": "Add who the email goes to."})
	}
	subject := strings.TrimSpace(in.Subject)
	// A reply joins the parent's conversation (D-80).
	var parent struct {
		ID                        uuid.UUID
		Thread, RFCID, ProviderID string
		Subject                   string
		Mailbox                   *uuid.UUID
	}
	if in.ReplyTo != nil {
		linked := link == nil
		err := h.store.Pool.QueryRow(ctx, `SELECT m.id, COALESCE(m.thread_id, ''), COALESCE(m.rfc_message_id, ''), COALESCE(m.provider_id, ''), m.subject, m.mail_account_id,
			$3::text = '' OR EXISTS (SELECT 1 FROM crm.message_links l WHERE l.message_id = m.id AND l.object_key = $3 AND l.record_id = $4)
			FROM crm.messages m WHERE m.id = $1 AND m.workspace_id = $2`, *in.ReplyTo, ws, linkObject(link), linkID(link)).
			Scan(&parent.ID, &parent.Thread, &parent.RFCID, &parent.ProviderID, &parent.Subject, &parent.Mailbox, &linked)
		if err != nil || !linked {
			return uuid.Nil, shared.Validation(map[string]string{"replyTo": "That email isn't on this record."})
		}
		if subject == "" {
			subject = replySubject(parent.Subject)
		}
	}
	if subject == "" || len(subject) > 250 {
		return uuid.Nil, shared.Validation(map[string]string{"subject": "Write a subject (up to 250 characters)."})
	}
	if len(in.Body) > 100_000 || len(in.HTML) > 500_000 {
		return uuid.Nil, shared.Validation(map[string]string{"body": "The email is too long."})
	}
	var suppressed bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.unsubscribes WHERE workspace_id = $1 AND email = ANY($2))`, ws, to).Scan(&suppressed)
	_ = suppressed // unsubscribes only stop campaigns; one-to-one emails still go out
	htmlBody := sanitizeRich(in.HTML)
	textBody := in.Body
	if htmlBody == "" {
		htmlBody = textToHTML(in.Body)
	} else if strings.TrimSpace(textBody) == "" {
		textBody = richPlain(htmlBody)
	}
	mid := uuid.New()
	threadID := "crm:" + mid.String()
	th := threadHeaders{InReplyTo: parent.RFCID}
	if parent.ID != uuid.Nil {
		threadID = parent.Thread
		if threadID == "" {
			threadID = "crm:" + parent.ID.String()
		}
	}
	var fromAddr, fromName string
	var mb *mailbox
	if in.MailboxID != nil {
		m, err := h.loadMailbox(ctx, h.store.Pool, *in.MailboxID)
		if err != nil || m.Workspace != ws || sender == nil || m.IdentityID != *sender {
			return uuid.Nil, shared.Validation(map[string]string{"mailboxId": "Send from one of your own connected mailboxes."})
		}
		mb, fromAddr, fromName = m, m.Email, m.Name
	}
	if sender != nil && fromName == "" {
		_ = h.store.Pool.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, *sender).Scan(&fromName)
	}
	status, errText, providerID, rfcID := "sent", "", "", ""
	if mb != nil {
		domain := "crm.local"
		if i := strings.LastIndex(mb.Email, "@"); i >= 0 {
			domain = mb.Email[i+1:]
		}
		rfcID = "<" + mid.String() + "@" + domain + ">"
		th.MessageID = rfcID
		if parent.Mailbox != nil && *parent.Mailbox == mb.ID {
			switch mb.Provider {
			case "google":
				if !strings.HasPrefix(parent.Thread, "crm:") {
					th.GmailThread = parent.Thread
				}
			case "microsoft":
				th.GraphParent = parent.ProviderID
			}
		}
		var providerThread string
		providerID, providerThread, err = h.sendThroughMailbox(ctx, mb, to, cc, subject, htmlBody, textBody, th)
		if providerThread != "" && parent.ID == uuid.Nil {
			threadID = providerThread
		}
	} else if h.mailer == nil {
		err = errors.New("email isn't set up on this server")
	} else {
		fromAddr = extractMailAddress(h.mailer.From())
		var replyTo string
		if sender != nil {
			_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(value_normalized, '') FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'email' AND namespace = 'global' LIMIT 1`,
				*sender).Scan(&replyTo)
		}
		for _, addr := range append(append([]string{}, to...), cc...) {
			if err = h.mailer.SendHTML(ctx, addr, subject, htmlBody, textBody, MailOptions{FromName: fromName, ReplyTo: replyTo, Headers: th.headers()}); err != nil {
				break
			}
		}
	}
	if err != nil {
		status, errText = "failed", err.Error()
	}
	snippet := textBody
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	var mbID *uuid.UUID
	if mb != nil {
		mbID = &mb.ID
	}
	var pid *string
	if providerID != "" {
		pid = &providerID
	}
	txErr := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO crm.messages (id, workspace_id, mail_account_id, provider_id, direction, from_addr, from_name, to_addrs, cc_addrs,
			subject, snippet, body_text, body_html, status, error, sent_by, thread_id, rfc_message_id, in_reply_to)
			VALUES ($1, $2, $3, $4, 'outbound', $5, $6, $7, $8, $9, $10, $11, $12, $13, NULLIF($14, ''), $15, $16, NULLIF($17, ''), NULLIF($18, ''))`,
			mid, ws, mbID, pid, fromAddr, fromName, to, cc, subject, snippet, textBody, htmlBody, status, errText, sender, threadID, rfcID, parent.RFCID); err != nil {
			return err
		}
		if parent.ID != uuid.Nil && parent.Thread == "" {
			if _, err := tx.Exec(ctx, `UPDATE crm.messages SET thread_id = $2 WHERE id = $1 AND thread_id IS NULL`, parent.ID, threadID); err != nil {
				return err
			}
		}
		links := []recordLink{}
		if link != nil {
			links = append(links, *link)
		}
		more, err := h.recordsByEmail(ctx, tx, ws, append(append([]string{}, to...), cc...))
		if err != nil {
			return err
		}
		links = append(links, more...)
		for _, l := range links {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.message_links (message_id, workspace_id, object_key, record_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				mid, ws, l.Object, l.ID); err != nil {
				return err
			}
		}
		// The Communications log gets the email too (when the module is on).
		if link != nil && specFor("communications") != nil {
			mods, _ := access.WorkspaceModules(ctx, tx, ws)
			if mods["communications"] {
				vals := map[string]any{"name": subject, "status": status, "channel": "email", "direction": "outbound",
					"occurredAt": time.Now().UTC().Format(time.RFC3339), "body": textBody}
				switch link.Object {
				case "accounts":
					vals["accountId"] = link.ID.String()
				case "contacts":
					vals["contactId"] = link.ID.String()
				case "leads":
					vals["leadId"] = link.ID.String()
				}
				if link.Object != "communications" {
					if sender != nil {
						vals["ownerId"] = sender.String()
					}
					a := actorInfo{ID: sender, Kind: "identity", Source: source}
					if sender == nil {
						a = systemActor(source)
					}
					if _, err := h.createRecord(ctx, tx, ws, specFor("communications"), a, vals); err != nil {
						slog.Warn("CRM email: couldn't log the communication", "error", err)
					}
				}
			}
		}
		return nil
	})
	if txErr != nil {
		return uuid.Nil, txErr
	}
	h.bus.Kick()
	if err != nil {
		return mid, shared.NewError(http.StatusBadGateway, "email_failed", "The email couldn't be sent: "+errText)
	}
	return mid, nil
}

func extractMailAddress(from string) string {
	if a, err := netmail.ParseAddress(from); err == nil {
		return a.Address
	}
	return from
}

// recordsByEmail finds contacts, leads and accounts with one of these addresses.
func (h *Handler) recordsByEmail(ctx context.Context, q querier, ws uuid.UUID, emails []string) ([]recordLink, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	lower := make([]string, len(emails))
	for i, e := range emails {
		lower[i] = strings.ToLower(e)
	}
	rows, err := q.Query(ctx, `
		SELECT 'contacts', id FROM crm.contacts WHERE workspace_id = $1 AND deleted_at IS NULL AND lower(email) = ANY($2)
		UNION ALL SELECT 'leads', id FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL AND lower(email) = ANY($2) AND status <> 'converted'
		UNION ALL SELECT 'accounts', id FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL AND lower(email) = ANY($2)
		UNION ALL SELECT 'accounts', c.account_id FROM crm.contacts c WHERE c.workspace_id = $1 AND c.deleted_at IS NULL AND lower(c.email) = ANY($2) AND c.account_id IS NOT NULL
		LIMIT 50`, ws, lower)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recordLink{}
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var l recordLink
		if err := rows.Scan(&l.Object, &l.ID); err != nil {
			return nil, err
		}
		if !seen[l.ID] {
			seen[l.ID] = true
			out = append(out, l)
		}
	}
	return out, rows.Err()
}

// POST /crm/{object}/{id}/email
func (h *Handler) handleSendRecordEmail(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "read")
	if !ok {
		return
	}
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapEmailSend) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Send email” permission."))
		return
	}
	var in emailInput
	if err := shared.DecodeJSONLimit(w, r, &in, 2<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	me := actor(r)
	mid, err := h.sendEmail(r.Context(), ws, in, &me, &recordLink{Object: spec.Key, ID: id}, "ui")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"id": mid, "status": "sent"})
}

// POST /crm/communications/{id}/send — send a logged email communication to its contact.
func (h *Handler) handleSendCommunication(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "update")
	if !ok {
		return
	}
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapEmailSend) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Send email” permission."))
		return
	}
	var in struct {
		MailboxID *uuid.UUID `json:"mailboxId"`
		To        []string   `json:"to"`
	}
	_ = shared.DecodeJSON(w, r, &in)
	row, _, err := h.getRow(r.Context(), h.store.Pool, ws, spec, id, ownerFilter(r, spec))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if ch, _ := row.Values["channel"].(string); ch != "email" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"channel": "Only email communications can be sent from here."}))
		return
	}
	if st, _ := row.Values["status"].(string); st == "sent" {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "already_sent", "This email was already sent."))
		return
	}
	to := in.To
	if len(to) == 0 {
		for _, k := range []string{"contactId", "leadId", "accountId"} {
			ref, _ := row.Values[k].(string)
			rid, err := uuid.Parse(ref)
			if err != nil {
				continue
			}
			table := map[string]string{"contactId": "contacts", "leadId": "leads", "accountId": "accounts"}[k]
			var email string
			if h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(email, '') FROM crm.`+table+` WHERE id = $1 AND workspace_id = $2`, rid, ws).Scan(&email) == nil && email != "" {
				to = []string{email}
				break
			}
		}
	}
	if len(to) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"to": "Link a contact, lead or account with an email address first."}))
		return
	}
	body, _ := row.Values["body"].(string)
	me := actor(r)
	_, sendErr := h.sendEmail(r.Context(), ws, emailInput{To: to, Subject: row.Title, Body: body, MailboxID: in.MailboxID}, &me, &recordLink{Object: "communications", ID: id}, "ui")
	status := "sent"
	if sendErr != nil {
		status = "failed"
	}
	var out *Row
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		out, err = h.updateValues(r.Context(), tx, ws, spec, id, actorFromRequest(r, "ui"), map[string]any{"status": status, "direction": "outbound",
			"occurredAt": time.Now().UTC().Format(time.RFC3339)}, nil, nil)
		return err
	})
	if sendErr != nil {
		shared.WriteError(w, r, sendErr)
		return
	}
	respond(w, r, http.StatusOK, out, err)
}

// ---- mailbox management (/w/{code}/mailboxes) ----

type Mailbox struct {
	ID                 uuid.UUID  `json:"id"`
	Provider           string     `json:"provider"`
	Email              string     `json:"email"`
	Status             string     `json:"status"`
	Error              string     `json:"error,omitempty"`
	SyncEmail          bool       `json:"syncEmail"`
	SyncCalendar       bool       `json:"syncCalendar"`
	Visibility         string     `json:"visibility"`
	AutoCreateContacts bool       `json:"autoCreateContacts"`
	LastSyncedAt       *time.Time `json:"lastSyncedAt,omitempty"`
	Messages           int        `json:"messages"`
}

func (h *Handler) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rows, err := h.store.Pool.Query(r.Context(), `SELECT m.id, m.provider, m.email, m.status, COALESCE(m.error, ''), m.sync_email, m.sync_calendar, m.visibility,
		m.auto_create_contacts, m.last_synced_at, (SELECT count(*) FROM crm.messages x WHERE x.mail_account_id = m.id)
		FROM crm.mail_accounts m WHERE m.workspace_id = $1 AND m.identity_id = $2 ORDER BY m.created_at`, sc.WS, actor(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		var m Mailbox
		if err := rows.Scan(&m.ID, &m.Provider, &m.Email, &m.Status, &m.Error, &m.SyncEmail, &m.SyncCalendar, &m.Visibility, &m.AutoCreateContacts, &m.LastSyncedAt, &m.Messages); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, m)
	}
	var blocked []string
	br, err := h.store.Pool.Query(r.Context(), `SELECT pattern FROM crm.mail_blocklist WHERE workspace_id = $1 AND identity_id = $2 ORDER BY pattern`, sc.WS, actor(r))
	if err == nil {
		for br.Next() {
			var p string
			if br.Scan(&p) == nil {
				blocked = append(blocked, p)
			}
		}
		br.Close()
	}
	if blocked == nil {
		blocked = []string{}
	}
	cfg := oauth.Configured(h.cfg.BaseURL)
	respond(w, r, http.StatusOK, map[string]any{"data": out, "blocklist": blocked, "providers": map[string]bool{"google": cfg["google"], "microsoft": cfg["microsoft"], "imap": true},
		"crmSender": h.mailer != nil && h.mailer.From() != "", "canSend": sc.Owner || sc.Eff.HasCapability(access.CapEmailSend)}, rows.Err())
}

// GET /mailboxes/connect/{provider} → {"url": …} to open.
func (h *Handler) handleConnectMailbox(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil {
		shared.WriteError(w, r, errForbidden)
		return
	}
	name := chi.URLParam(r, "provider")
	p := oauth.Get(name, h.cfg.BaseURL)
	if p == nil || name == "linkedin" {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "provider_not_configured",
			"Connecting "+map[string]string{"google": "Gmail", "microsoft": "Outlook"}[name]+" isn't set up on this server yet. Ask the owner to add the OAuth client ID (see Integrations)."))
		return
	}
	nonce, _ := shared.RandomToken(16)
	state := oauth.EncodeState(h.cfg.EncryptionKey, oauth.State{Purpose: "mailbox", Provider: name, Workspace: sc.Code, Identity: actor(r).String(), Nonce: nonce,
		Return: "/crm/w/" + sc.Code + "/settings/email", Expires: time.Now().Add(10 * time.Minute).Unix()})
	http.SetCookie(w, &http.Cookie{Name: "crm_oauth", Value: nonce, Path: "/api/crm/v1/oauth", MaxAge: 600, HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	shared.WriteJSON(w, http.StatusOK, map[string]string{"url": p.AuthURL(state, "mailbox")})
}

// CompleteMailboxConnect finishes the OAuth flow for a mailbox (called by the callback).
func (h *Handler) CompleteMailboxConnect(ctx context.Context, st *oauth.State, code string) (string, error) {
	p := oauth.Get(st.Provider, h.cfg.BaseURL)
	if p == nil {
		return "", errors.New("provider not configured")
	}
	identityID, err := uuid.Parse(st.Identity)
	if err != nil {
		return "", err
	}
	var ws uuid.UUID
	if err := h.store.Pool.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE code = $1`, st.Workspace).Scan(&ws); err != nil {
		return "", err
	}
	tok, err := p.Exchange(ctx, code)
	if err != nil {
		return "", errors.New("the provider didn't accept the sign-in; try again")
	}
	u, err := p.FetchUser(ctx, tok)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(mailCreds{Token: tok})
	enc, err := shared.Encrypt(h.cfg.EncryptionKey, raw)
	if err != nil {
		return "", err
	}
	var id uuid.UUID
	if err := h.store.Pool.QueryRow(ctx, `INSERT INTO crm.mail_accounts (workspace_id, identity_id, provider, email, display_name, credentials_enc)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id, identity_id, email) DO UPDATE SET credentials_enc = EXCLUDED.credentials_enc, provider = EXCLUDED.provider, status = 'active', error = NULL, updated_at = now()
		RETURNING id`, ws, identityID, st.Provider, u.Email, u.Name, enc).Scan(&id); err != nil {
		return "", err
	}
	_ = shared.WriteAudit(ctx, h.store.Pool, shared.AuditEvent{WorkspaceID: &ws, ActorID: &identityID, Action: "mailbox.connected", EntityType: "mailbox", EntityID: &id,
		After: map[string]any{"provider": st.Provider, "email": u.Email}})
	go h.syncMailbox(context.Background(), id)
	return st.Return + "?connected=" + url.QueryEscape(u.Email), nil
}

// POST /mailboxes/imap — any mailbox with IMAP (reading) and SMTP (sending).
func (h *Handler) handleConnectIMAP(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		IMAPHost string `json:"imapHost"`
		IMAPPort int    `json:"imapPort"`
		SMTPHost string `json:"smtpHost"`
		SMTPPort int    `json:"smtpPort"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	addr, err := netmail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil {
		fe["email"] = "Enter the mailbox's email address."
	}
	if in.IMAPHost == "" {
		fe["imapHost"] = "Enter the IMAP server (e.g. imap.zoho.in)."
	}
	if in.IMAPPort == 0 {
		in.IMAPPort = 993
	}
	if in.SMTPPort == 0 {
		in.SMTPPort = 587
	}
	if in.Username == "" {
		in.Username = strings.TrimSpace(in.Email)
	}
	if in.Password == "" {
		fe["password"] = "Enter the mailbox password (or an app password)."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	creds := mailCreds{IMAP: &serverCreds{Host: in.IMAPHost, Port: in.IMAPPort, Username: in.Username, Password: in.Password}}
	if in.SMTPHost != "" {
		creds.SMTP = &serverCreds{Host: in.SMTPHost, Port: in.SMTPPort, Username: in.Username, Password: in.Password}
	}
	// Check the details before saving them.
	c, err := dialIMAP(creds.IMAP)
	if err != nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"imapHost": "Couldn't sign in to the IMAP server: " + err.Error()}))
		return
	}
	_ = c.Logout()
	raw, _ := json.Marshal(creds)
	enc, err := shared.Encrypt(h.cfg.EncryptionKey, raw)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var id uuid.UUID
	if err := h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.mail_accounts (workspace_id, identity_id, provider, email, display_name, credentials_enc, sync_calendar)
		VALUES ($1, $2, 'imap', $3, $4, $5, false)
		ON CONFLICT (workspace_id, identity_id, email) DO UPDATE SET credentials_enc = EXCLUDED.credentials_enc, provider = 'imap', status = 'active', error = NULL, updated_at = now()
		RETURNING id`, sc.WS, actor(r), strings.ToLower(addr.Address), strings.TrimSpace(in.Name), enc).Scan(&id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	go h.syncMailbox(context.Background(), id)
	h.handleListMailboxes(w, r)
}

func (h *Handler) ownMailbox(r *http.Request) (uuid.UUID, error) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "mailboxId"))
	if err != nil {
		return uuid.Nil, shared.NotFound("mailbox_not_found")
	}
	var ok bool
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.mail_accounts WHERE id = $1 AND workspace_id = $2 AND identity_id = $3)`, id, sc.WS, actor(r)).Scan(&ok)
	if !ok {
		return uuid.Nil, shared.NotFound("mailbox_not_found")
	}
	return id, nil
}

func (h *Handler) handleUpdateMailbox(w http.ResponseWriter, r *http.Request) {
	id, err := h.ownMailbox(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		SyncEmail          *bool   `json:"syncEmail"`
		SyncCalendar       *bool   `json:"syncCalendar"`
		Visibility         *string `json:"visibility"`
		AutoCreateContacts *bool   `json:"autoCreateContacts"`
		Paused             *bool   `json:"paused"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Visibility != nil && *in.Visibility != "share_everything" && *in.Visibility != "subject" && *in.Visibility != "metadata" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"visibility": "Pick how much colleagues see."}))
		return
	}
	var status *string
	if in.Paused != nil {
		s := "active"
		if *in.Paused {
			s = "paused"
		}
		status = &s
	}
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.mail_accounts SET sync_email = COALESCE($2, sync_email), sync_calendar = COALESCE($3, sync_calendar),
		visibility = COALESCE($4, visibility), auto_create_contacts = COALESCE($5, auto_create_contacts), status = COALESCE($6, status), updated_at = now() WHERE id = $1`,
		id, in.SyncEmail, in.SyncCalendar, in.Visibility, in.AutoCreateContacts, status); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListMailboxes(w, r)
}

func (h *Handler) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	id, err := h.ownMailbox(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.mail_accounts WHERE id = $1`, id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSyncMailbox(w http.ResponseWriter, r *http.Request) {
	id, err := h.ownMailbox(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res := h.syncMailbox(r.Context(), id)
	shared.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) handleBlocklist(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		Add    string `json:"add"`
		Remove string `json:"remove"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if p := strings.ToLower(strings.TrimSpace(in.Add)); p != "" {
		if len(p) > 200 || !strings.Contains(p, "@") && !strings.Contains(p, ".") {
			shared.WriteError(w, r, shared.Validation(map[string]string{"add": "Enter an email address or a domain like @example.com."}))
			return
		}
		_, _ = h.store.Pool.Exec(r.Context(), `INSERT INTO crm.mail_blocklist (workspace_id, identity_id, pattern) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, sc.WS, actor(r), p)
	}
	if p := strings.ToLower(strings.TrimSpace(in.Remove)); p != "" {
		_, _ = h.store.Pool.Exec(r.Context(), `DELETE FROM crm.mail_blocklist WHERE workspace_id = $1 AND identity_id = $2 AND pattern = $3`, sc.WS, actor(r), p)
	}
	h.handleListMailboxes(w, r)
}

// ---- sync ----

type syncResult struct {
	Emails   int    `json:"emails"`
	Events   int    `json:"events"`
	Contacts int    `json:"contactsCreated"`
	Error    string `json:"error,omitempty"`
}

type syncedMessage struct {
	ProviderID string
	ThreadID   string
	MessageID  string // RFC Message-ID
	InReplyTo  string
	From       string
	FromName   string
	To, Cc     []string
	Subject    string
	Snippet    string
	Body       string
	At         time.Time
}

type syncedEvent struct {
	ExternalID string
	Title      string
	Start, End time.Time
	Location   string
	Link       string
	Attendees  []string
	Cancelled  bool
	Body       string
}

// syncDueMailboxes syncs mailboxes that haven't synced for an hour.
func (h *Handler) syncDueMailboxes(ctx context.Context) {
	rows, err := h.store.Pool.Query(ctx, `SELECT id FROM crm.mail_accounts WHERE status = 'active' AND (last_synced_at IS NULL OR last_synced_at < now() - interval '1 hour') LIMIT 20`)
	if err != nil {
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		h.syncMailbox(ctx, id)
	}
}

func (h *Handler) syncMailbox(ctx context.Context, id uuid.UUID) syncResult {
	res := syncResult{}
	m, err := h.loadMailbox(ctx, h.store.Pool, id)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	fail := func(err error) syncResult {
		res.Error = err.Error()
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.mail_accounts SET status = 'error', error = $2, last_synced_at = now() WHERE id = $1`, id, res.Error)
		return res
	}
	since := time.Now().AddDate(0, 0, -30)
	if v, ok := m.Cursor["emailAfter"].(float64); ok && v > 0 {
		since = time.Unix(int64(v), 0)
	}
	started := time.Now()
	if m.SyncEmail {
		var msgs []syncedMessage
		switch m.Provider {
		case "google":
			msgs, err = h.fetchGmail(ctx, m, since)
		case "microsoft":
			msgs, err = h.fetchOutlook(ctx, m, since)
		case "imap":
			msgs, err = fetchIMAP(m, since)
		}
		if err != nil {
			return fail(err)
		}
		n, created, err := h.storeMessages(ctx, m, msgs)
		if err != nil {
			return fail(err)
		}
		res.Emails, res.Contacts = n, created
		m.Cursor["emailAfter"] = float64(started.Add(-10 * time.Minute).Unix())
	}
	if m.SyncCal && m.Provider != "imap" {
		evs, err := h.fetchCalendar(ctx, m)
		if err != nil {
			return fail(err)
		}
		n, err := h.storeEvents(ctx, m, evs)
		if err != nil {
			return fail(err)
		}
		res.Events = n
	}
	cursor, _ := json.Marshal(m.Cursor)
	_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.mail_accounts SET status = 'active', error = NULL, cursor = $2, last_synced_at = now() WHERE id = $1`, id, cursor)
	h.bus.Kick()
	return res
}

func (h *Handler) blocked(ctx context.Context, m *mailbox) []string {
	out := []string{}
	rows, err := h.store.Pool.Query(ctx, `SELECT pattern FROM crm.mail_blocklist WHERE workspace_id = $1 AND identity_id = $2`, m.Workspace, m.IdentityID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	return out
}

func isBlocked(addr string, patterns []string) bool {
	for _, p := range patterns {
		if addr == p || (strings.HasPrefix(p, "@") && strings.HasSuffix(addr, p)) || strings.HasSuffix(addr, "@"+strings.TrimPrefix(p, "@")) {
			return true
		}
	}
	return false
}

// storeMessages keeps emails that involve people the CRM knows (or creates contacts).
func (h *Handler) storeMessages(ctx context.Context, m *mailbox, msgs []syncedMessage) (int, int, error) {
	stored, created := 0, 0
	domain := m.Email[strings.LastIndex(m.Email, "@"):]
	blocklist := h.blocked(ctx, m)
	contacts := specFor("contacts")
	for _, msg := range msgs {
		participants := []string{}
		for _, a := range append(append([]string{msg.From}, msg.To...), msg.Cc...) {
			a = strings.ToLower(strings.TrimSpace(a))
			if a == "" || a == m.Email || isBlocked(a, blocklist) || contains(participants, a) {
				continue
			}
			participants = append(participants, a)
		}
		// Internal emails (same domain) stay private.
		external := []string{}
		for _, a := range participants {
			if !strings.HasSuffix(a, domain) || domain == "@gmail.com" || domain == "@outlook.com" {
				external = append(external, a)
			}
		}
		if len(external) == 0 {
			continue
		}
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			links, err := h.recordsByEmail(ctx, tx, m.Workspace, external)
			if err != nil {
				return err
			}
			if len(links) == 0 && m.AutoCreate && contacts != nil {
				for _, a := range external {
					if len(links) >= 5 {
						break
					}
					name := a[:strings.Index(a, "@")]
					if a == strings.ToLower(msg.From) && msg.FromName != "" {
						name = msg.FromName
					}
					first, last, _ := strings.Cut(strings.TrimSpace(name), " ")
					if last == "" {
						first, last = "", first
					}
					owner := m.IdentityID.String()
					row, err := h.createRecord(ctx, tx, m.Workspace, contacts, systemActor("email"), map[string]any{"firstName": first, "lastName": last, "email": a,
						"ownerId": owner, "leadSource": "email"})
					if err != nil {
						continue
					}
					created++
					if rid, err := uuid.Parse(row.ID); err == nil {
						links = append(links, recordLink{Object: "contacts", ID: rid})
					}
				}
			}
			if len(links) == 0 {
				return nil
			}
			dir := "inbound"
			if strings.EqualFold(msg.From, m.Email) {
				dir = "outbound"
			}
			var mid uuid.UUID
			err = tx.QueryRow(ctx, `INSERT INTO crm.messages (workspace_id, mail_account_id, provider_id, thread_id, direction, from_addr, from_name, to_addrs, cc_addrs,
				subject, snippet, body_text, status, sent_at, sent_by, rfc_message_id, in_reply_to)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, CASE WHEN $5 = 'outbound' THEN $15::uuid END, NULLIF($16, ''), NULLIF($17, ''))
				ON CONFLICT (mail_account_id, provider_id) DO NOTHING RETURNING id`,
				m.Workspace, m.ID, msg.ProviderID, msg.ThreadID, dir, strings.ToLower(msg.From), msg.FromName, msg.To, msg.Cc, msg.Subject, msg.Snippet, msg.Body,
				map[string]string{"inbound": "received", "outbound": "sent"}[dir], msg.At, m.IdentityID, msg.MessageID, msg.InReplyTo).Scan(&mid)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // already synced
			}
			if err != nil {
				return err
			}
			for _, l := range links {
				if _, err := tx.Exec(ctx, `INSERT INTO crm.message_links (message_id, workspace_id, object_key, record_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
					mid, m.Workspace, l.Object, l.ID); err != nil {
					return err
				}
			}
			stored++
			return nil
		})
		if err != nil {
			return stored, created, err
		}
	}
	return stored, created, nil
}

func (h *Handler) fetchGmail(ctx context.Context, m *mailbox, since time.Time) ([]syncedMessage, error) {
	c, err := h.mailboxHTTP(ctx, m)
	if err != nil {
		return nil, err
	}
	q := url.Values{"q": {"after:" + strconv.FormatInt(since.Unix(), 10) + " -in:chats"}, "maxResults": {"100"}}
	res, err := c.Get("https://gmail.googleapis.com/gmail/v1/users/me/messages?" + q.Encode())
	if err != nil {
		return nil, err
	}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	err = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode >= 300 || err != nil {
		return nil, fmt.Errorf("Gmail: %s", res.Status)
	}
	out := []syncedMessage{}
	for _, it := range list.Messages {
		r2, err := c.Get("https://gmail.googleapis.com/gmail/v1/users/me/messages/" + it.ID + "?format=full")
		if err != nil {
			continue
		}
		var full struct {
			ID, ThreadID, Snippet string
			InternalDate          string `json:"internalDate"`
			Payload               gmailPart
		}
		_ = json.NewDecoder(r2.Body).Decode(&full)
		r2.Body.Close()
		msg := syncedMessage{ProviderID: full.ID, ThreadID: full.ThreadID, Snippet: html.UnescapeString(full.Snippet)}
		for _, hd := range full.Payload.Headers {
			switch strings.ToLower(hd.Name) {
			case "from":
				if a, err := netmail.ParseAddress(hd.Value); err == nil {
					msg.From, msg.FromName = a.Address, a.Name
				}
			case "to":
				msg.To = parseAddrList(hd.Value)
			case "cc":
				msg.Cc = parseAddrList(hd.Value)
			case "subject":
				msg.Subject = hd.Value
			case "message-id":
				msg.MessageID = strings.TrimSpace(hd.Value)
			case "in-reply-to":
				msg.InReplyTo = strings.TrimSpace(hd.Value)
			}
		}
		if ms, err := strconv.ParseInt(full.InternalDate, 10, 64); err == nil {
			msg.At = time.UnixMilli(ms)
		}
		msg.Body = full.Payload.text()
		out = append(out, msg)
	}
	return out, nil
}

type gmailPart struct {
	MimeType string `json:"mimeType"`
	Headers  []struct {
		Name, Value string
	} `json:"headers"`
	Body struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

func (p gmailPart) text() string {
	if p.MimeType == "text/plain" && p.Body.Data != "" {
		b, _ := base64.URLEncoding.DecodeString(p.Body.Data)
		if len(b) == 0 {
			b, _ = base64.RawURLEncoding.DecodeString(p.Body.Data)
		}
		return clip(string(b), 20_000)
	}
	for _, c := range p.Parts {
		if t := c.text(); t != "" {
			return t
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func parseAddrList(v string) []string {
	list, err := netmail.ParseAddressList(v)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

func (h *Handler) fetchOutlook(ctx context.Context, m *mailbox, since time.Time) ([]syncedMessage, error) {
	c, err := h.mailboxHTTP(ctx, m)
	if err != nil {
		return nil, err
	}
	q := url.Values{"$filter": {"receivedDateTime ge " + since.UTC().Format(time.RFC3339)}, "$top": {"100"},
		"$select": {"id,conversationId,internetMessageId,subject,bodyPreview,body,from,toRecipients,ccRecipients,receivedDateTime,isDraft"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me/messages?"+q.Encode(), nil)
	req.Header.Set("Prefer", `outlook.body-content-type="text"`)
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("Outlook: %s", res.Status)
	}
	type addr struct {
		EmailAddress struct{ Name, Address string } `json:"emailAddress"`
	}
	var list struct {
		Value []struct {
			ID, ConversationID, InternetMessageID, Subject, BodyPreview string
			Body                                                        struct{ Content string } `json:"body"`
			From                                                        addr
			ToRecipients, CcRecipients                                  []addr
			ReceivedDateTime                                            time.Time
			IsDraft                                                     bool
		} `json:"value"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := []syncedMessage{}
	addrs := func(l []addr) []string {
		o := []string{}
		for _, a := range l {
			o = append(o, strings.ToLower(a.EmailAddress.Address))
		}
		return o
	}
	for _, v := range list.Value {
		if v.IsDraft {
			continue
		}
		out = append(out, syncedMessage{ProviderID: v.ID, ThreadID: v.ConversationID, MessageID: v.InternetMessageID, Subject: v.Subject, Snippet: v.BodyPreview, Body: clip(v.Body.Content, 20_000),
			From: v.From.EmailAddress.Address, FromName: v.From.EmailAddress.Name, To: addrs(v.ToRecipients), Cc: addrs(v.CcRecipients), At: v.ReceivedDateTime})
	}
	return out, nil
}

func dialIMAP(s *serverCreds) (*imapclient.Client, error) {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var c *imapclient.Client
	var err error
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if s.Port == 143 {
		c, err = imapclient.DialWithDialer(dialer, addr)
		if err == nil {
			err = c.StartTLS(&tls.Config{ServerName: s.Host})
		}
	} else {
		c, err = imapclient.DialWithDialerTLS(dialer, addr, &tls.Config{ServerName: s.Host})
	}
	if err != nil {
		return nil, errors.New("can't reach " + addr)
	}
	c.Timeout = 30 * time.Second
	if err := c.Login(s.Username, s.Password); err != nil {
		_ = c.Logout()
		return nil, errors.New("the username or password wasn't accepted")
	}
	return c, nil
}

func fetchIMAP(m *mailbox, since time.Time) ([]syncedMessage, error) {
	if m.Creds.IMAP == nil {
		return nil, errors.New("no IMAP server saved")
	}
	c, err := dialIMAP(m.Creds.IMAP)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	folders := []string{"INBOX"}
	boxes := make(chan *imap.MailboxInfo, 50)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", boxes) }()
	for b := range boxes {
		for _, a := range b.Attributes {
			if a == imap.SentAttr {
				folders = append(folders, b.Name)
			}
		}
	}
	<-done
	out := []syncedMessage{}
	for _, folder := range folders {
		if _, err := c.Select(folder, true); err != nil {
			continue
		}
		crit := imap.NewSearchCriteria()
		crit.Since = since
		uids, err := c.UidSearch(crit)
		if err != nil || len(uids) == 0 {
			continue
		}
		if len(uids) > 100 {
			uids = uids[len(uids)-100:]
		}
		set := new(imap.SeqSet)
		set.AddNum(uids...)
		section := &imap.BodySectionName{Peek: true}
		msgs := make(chan *imap.Message, 20)
		fdone := make(chan error, 1)
		go func() {
			fdone <- c.UidFetch(set, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}, msgs)
		}()
		for msg := range msgs {
			if msg.Envelope == nil {
				continue
			}
			sm := syncedMessage{ProviderID: folder + ":" + strconv.FormatUint(uint64(msg.Uid), 10), Subject: msg.Envelope.Subject, At: msg.Envelope.Date,
				ThreadID: msg.Envelope.InReplyTo, MessageID: msg.Envelope.MessageId, InReplyTo: msg.Envelope.InReplyTo}
			if len(msg.Envelope.From) > 0 {
				sm.From, sm.FromName = msg.Envelope.From[0].Address(), msg.Envelope.From[0].PersonalName
			}
			for _, a := range msg.Envelope.To {
				sm.To = append(sm.To, strings.ToLower(a.Address()))
			}
			for _, a := range msg.Envelope.Cc {
				sm.Cc = append(sm.Cc, strings.ToLower(a.Address()))
			}
			if body := msg.GetBody(section); body != nil {
				raw, _ := io.ReadAll(io.LimitReader(body, 512<<10))
				sm.Body = plainFromRFC822(raw)
				sm.Snippet = clip(strings.Join(strings.Fields(sm.Body), " "), 200)
			}
			out = append(out, sm)
		}
		<-fdone
	}
	return out, nil
}

// plainFromRFC822 pulls the text/plain body out of a raw message.
func plainFromRFC822(raw []byte) string {
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var walk func(ct string, r io.Reader, enc string) string
	walk = func(ct string, r io.Reader, enc string) string {
		mt, params, _ := mime.ParseMediaType(ct)
		if strings.HasPrefix(mt, "multipart/") {
			mr := multipart.NewReader(r, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					return ""
				}
				if t := walk(p.Header.Get("Content-Type"), p, p.Header.Get("Content-Transfer-Encoding")); t != "" {
					return t
				}
			}
		}
		if mt == "text/plain" || mt == "" {
			b, _ := io.ReadAll(io.LimitReader(r, 100<<10))
			if strings.EqualFold(enc, "base64") {
				if d, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(b)), "")); err == nil {
					b = d
				}
			}
			return clip(string(b), 20_000)
		}
		return ""
	}
	return walk(msg.Header.Get("Content-Type"), msg.Body, msg.Header.Get("Content-Transfer-Encoding"))
}

func (h *Handler) fetchCalendar(ctx context.Context, m *mailbox) ([]syncedEvent, error) {
	c, err := h.mailboxHTTP(ctx, m)
	if err != nil {
		return nil, err
	}
	from, to := time.Now().AddDate(0, 0, -30), time.Now().AddDate(0, 0, 90)
	out := []syncedEvent{}
	switch m.Provider {
	case "google":
		q := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)}, "singleEvents": {"true"}, "maxResults": {"250"}, "orderBy": {"startTime"}}
		res, err := c.Get("https://www.googleapis.com/calendar/v3/calendars/primary/events?" + q.Encode())
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return nil, fmt.Errorf("Google Calendar: %s", res.Status)
		}
		type when struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		}
		var list struct {
			Items []struct {
				ID, Summary, Location, HangoutLink, Status, Description string
				Start, End                                              when
				Attendees                                               []struct{ Email string } `json:"attendees"`
			} `json:"items"`
		}
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			return nil, err
		}
		parse := func(w when) time.Time {
			if t, err := time.Parse(time.RFC3339, w.DateTime); err == nil {
				return t
			}
			t, _ := time.ParseInLocation("2006-01-02", w.Date, istLocation)
			return t
		}
		for _, it := range list.Items {
			ev := syncedEvent{ExternalID: "google:" + it.ID, Title: it.Summary, Start: parse(it.Start), End: parse(it.End), Location: it.Location, Link: it.HangoutLink,
				Cancelled: it.Status == "cancelled", Body: clip(it.Description, 5000)}
			for _, a := range it.Attendees {
				ev.Attendees = append(ev.Attendees, strings.ToLower(a.Email))
			}
			out = append(out, ev)
		}
	case "microsoft":
		q := url.Values{"startDateTime": {from.UTC().Format(time.RFC3339)}, "endDateTime": {to.UTC().Format(time.RFC3339)}, "$top": {"250"},
			"$select": {"id,subject,start,end,location,onlineMeeting,isCancelled,attendees,bodyPreview"}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me/calendarView?"+q.Encode(), nil)
		req.Header.Set("Prefer", `outlook.timezone="UTC"`)
		res, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return nil, fmt.Errorf("Outlook calendar: %s", res.Status)
		}
		var list struct {
			Value []struct {
				ID, Subject, BodyPreview string
				Start                    struct{ DateTime string } `json:"start"`
				End                      struct{ DateTime string } `json:"end"`
				Location                 struct{ DisplayName string }
				OnlineMeeting            *struct{ JoinURL string } `json:"onlineMeeting"`
				IsCancelled              bool
				Attendees                []struct {
					EmailAddress struct{ Address string } `json:"emailAddress"`
				}
			} `json:"value"`
		}
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			return nil, err
		}
		for _, it := range list.Value {
			st, _ := time.Parse("2006-01-02T15:04:05.0000000", it.Start.DateTime)
			en, _ := time.Parse("2006-01-02T15:04:05.0000000", it.End.DateTime)
			ev := syncedEvent{ExternalID: "microsoft:" + it.ID, Title: it.Subject, Start: st, End: en, Location: it.Location.DisplayName, Cancelled: it.IsCancelled, Body: it.BodyPreview}
			if it.OnlineMeeting != nil {
				ev.Link = it.OnlineMeeting.JoinURL
			}
			for _, a := range it.Attendees {
				ev.Attendees = append(ev.Attendees, strings.ToLower(a.EmailAddress.Address))
			}
			out = append(out, ev)
		}
	}
	return out, nil
}

// storeEvents upserts synced events into the Calendar module (events object).
func (h *Handler) storeEvents(ctx context.Context, m *mailbox, evs []syncedEvent) (int, error) {
	spec := specFor("events")
	if spec == nil {
		return 0, nil
	}
	mods, err := access.WorkspaceModules(ctx, h.store.Pool, m.Workspace)
	if err != nil || !mods["calendar"] {
		return 0, err
	}
	n := 0
	for _, ev := range evs {
		if ev.Title == "" {
			ev.Title = "(no title)"
		}
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			vals := map[string]any{"name": ev.Title, "startsAt": ev.Start.UTC().Format(time.RFC3339), "location": ev.Location, "description": ev.Body,
				"status": map[bool]string{true: "cancelled", false: "planned"}[ev.Cancelled]}
			if !ev.End.IsZero() {
				vals["endsAt"] = ev.End.UTC().Format(time.RFC3339)
			}
			if ev.Link != "" {
				vals["meetingLink"] = ev.Link
			} else {
				vals["meetingLink"] = nil
			}
			if ev.Start.Before(time.Now()) && !ev.Cancelled {
				vals["status"] = "held"
			}
			links, _ := h.recordsByEmail(ctx, tx, m.Workspace, ev.Attendees)
			for _, l := range links {
				switch l.Object {
				case "contacts":
					if _, ok := vals["contactId"]; !ok {
						vals["contactId"] = l.ID.String()
					}
				case "accounts":
					if _, ok := vals["accountId"]; !ok {
						vals["accountId"] = l.ID.String()
					}
				case "leads":
					if _, ok := vals["leadId"]; !ok {
						vals["leadId"] = l.ID.String()
					}
				}
			}
			var existing uuid.UUID
			_ = tx.QueryRow(ctx, `SELECT id FROM crm.obj_events WHERE workspace_id = $1 AND custom->>'externalId' = $2 AND deleted_at IS NULL`, m.Workspace, ev.ExternalID).Scan(&existing)
			a := systemActor("calendar")
			if existing != uuid.Nil {
				_, err := h.updateValues(ctx, tx, m.Workspace, spec, existing, a, vals, nil, nil)
				return err
			}
			if len(links) == 0 {
				return nil // only meetings with people the CRM knows
			}
			vals["ownerId"] = m.IdentityID.String()
			row, err := h.createRecord(ctx, tx, m.Workspace, spec, a, vals)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('externalId', $2::text, 'mailboxId', $3::text) WHERE id = $1`,
				row.ID, ev.ExternalID, m.ID.String())
			n++
			return err
		})
		if err != nil {
			slog.Warn("CRM calendar sync: event skipped", "error", err)
		}
	}
	return n, nil
}

// pushEventToCalendar adds a CRM event to its owner's connected calendar (Google / Outlook).
func (h *Handler) pushEventToCalendar(ctx context.Context, ws uuid.UUID, ev Event) {
	if ev.Object != "events" || ev.Type != "record.created" || ev.Source == "calendar" || ev.ActorID == nil {
		return
	}
	var mbID uuid.UUID
	if err := h.store.Pool.QueryRow(ctx, `SELECT id FROM crm.mail_accounts WHERE workspace_id = $1 AND identity_id = $2 AND status = 'active' AND sync_calendar AND provider IN ('google', 'microsoft')
		ORDER BY created_at LIMIT 1`, ws, *ev.ActorID).Scan(&mbID); err != nil {
		return
	}
	m, err := h.loadMailbox(ctx, h.store.Pool, mbID)
	if err != nil {
		return
	}
	c, err := h.mailboxHTTP(ctx, m)
	if err != nil {
		return
	}
	title, _ := ev.Record["name"].(string)
	start, _ := ev.Record["startsAt"].(string)
	end, _ := ev.Record["endsAt"].(string)
	if end == "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			end = t.Add(30 * time.Minute).Format(time.RFC3339)
		}
	}
	loc, _ := ev.Record["location"].(string)
	desc, _ := ev.Record["description"].(string)
	var externalID string
	switch m.Provider {
	case "google":
		body, _ := json.Marshal(map[string]any{"summary": title, "location": loc, "description": desc,
			"start": map[string]string{"dateTime": start}, "end": map[string]string{"dateTime": end}})
		res, err := c.Post("https://www.googleapis.com/calendar/v3/calendars/primary/events", "application/json", bytes.NewReader(body))
		if err != nil {
			return
		}
		var out struct{ ID string }
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		externalID = "google:" + out.ID
	case "microsoft":
		body, _ := json.Marshal(map[string]any{"subject": title, "location": map[string]string{"displayName": loc}, "body": map[string]string{"contentType": "text", "content": desc},
			"start": map[string]string{"dateTime": strings.TrimSuffix(start, "Z"), "timeZone": "UTC"}, "end": map[string]string{"dateTime": strings.TrimSuffix(end, "Z"), "timeZone": "UTC"}})
		res, err := c.Post("https://graph.microsoft.com/v1.0/me/events", "application/json", bytes.NewReader(body))
		if err != nil {
			return
		}
		var out struct{ ID string }
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		externalID = "microsoft:" + out.ID
	}
	if externalID != "" && !strings.HasSuffix(externalID, ":") {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('externalId', $2::text, 'mailboxId', $3::text) WHERE id = $1`,
			ev.RecordID, externalID, m.ID.String())
	}
}
