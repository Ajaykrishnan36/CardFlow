package records

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// Email conversations (D-80). A record's emails are grouped into threads: messages share
// a provider thread, answer one another (In-Reply-To), or carry the same subject once
// "Re:"/"Fwd:" are stripped. Replies go through sendEmail with replyTo set.

func linkObject(l *recordLink) string {
	if l == nil {
		return ""
	}
	return l.Object
}

func linkID(l *recordLink) uuid.UUID {
	if l == nil {
		return uuid.Nil
	}
	return l.ID
}

var replyPrefix = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|aw|sv|wg)\s*(\[\d+\])?\s*:\s*)+`)

// threadSubject is the subject without reply/forward prefixes, lower-cased.
func threadSubject(s string) string {
	return strings.ToLower(strings.TrimSpace(replyPrefix.ReplaceAllString(s, "")))
}

func replySubject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Re:"
	}
	if replyPrefix.MatchString(s) {
		return "Re: " + strings.TrimSpace(replyPrefix.ReplaceAllString(s, ""))
	}
	return "Re: " + s
}

type ThreadMessage struct {
	ID        string    `json:"id"`
	Direction string    `json:"direction"`
	From      string    `json:"from"`
	FromName  string    `json:"fromName,omitempty"`
	To        []string  `json:"to"`
	Cc        []string  `json:"cc"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body,omitempty"`
	HTML      string    `json:"html,omitempty"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
	Hidden    string    `json:"hidden,omitempty"` // "body" | "all" — the mailbox owner shares less
	Mailbox   string    `json:"mailboxId,omitempty"`
}

type EmailThread struct {
	ID           string          `json:"id"` // the first message's id
	Subject      string          `json:"subject"`
	Count        int             `json:"count"`
	LastAt       time.Time       `json:"lastAt"`
	Snippet      string          `json:"snippet"`
	Participants []string        `json:"participants"`
	Messages     []ThreadMessage `json:"messages"`
}

type threadRow struct {
	msg                    ThreadMessage
	thread, rfcID, replyTo string
	snippet                string
}

// groupThreads joins messages into conversations (union-find over the three rules).
func groupThreads(rows []threadRow) []EmailThread {
	parent := make([]int, len(rows))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[rb] = ra
		}
	}
	byThread, byRFC, bySubject := map[string]int{}, map[string]int{}, map[string]int{}
	for i, r := range rows {
		if r.thread != "" {
			if j, ok := byThread[r.thread]; ok {
				union(j, i)
			} else {
				byThread[r.thread] = i
			}
		}
		if r.rfcID != "" {
			byRFC[r.rfcID] = i
		}
		if s := threadSubject(r.msg.Subject); s != "" && r.msg.Hidden != "all" {
			if j, ok := bySubject[s]; ok {
				union(j, i)
			} else {
				bySubject[s] = i
			}
		}
	}
	for i, r := range rows {
		if j, ok := byRFC[r.replyTo]; ok && r.replyTo != "" {
			union(j, i)
		}
	}
	groups := map[int]*EmailThread{}
	order := []int{}
	for i, r := range rows { // rows are oldest first
		root := find(i)
		t := groups[root]
		if t == nil {
			t = &EmailThread{ID: r.msg.ID, Subject: r.msg.Subject, Participants: []string{}}
			groups[root] = t
			order = append(order, root)
		}
		t.Messages = append(t.Messages, r.msg)
		t.Count++
		t.LastAt = r.msg.At
		t.Snippet = r.snippet
		for _, a := range append(append([]string{r.msg.From}, r.msg.To...), r.msg.Cc...) {
			a = strings.ToLower(a)
			if a != "" && !contains(t.Participants, a) && len(t.Participants) < 20 {
				t.Participants = append(t.Participants, a)
			}
		}
	}
	out := make([]EmailThread, 0, len(order))
	for _, root := range order {
		out = append(out, *groups[root])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	return out
}

// GET /crm/{object}/{id}/emails — the record's email conversations, newest first.
func (h *Handler) handleEmailThreads(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "read")
	if !ok {
		return
	}
	ctx := r.Context()
	me := actor(r)
	rows, err := h.store.Pool.Query(ctx, `SELECT * FROM (
		SELECT m.id::text, m.direction, m.from_addr, m.from_name, m.to_addrs, m.cc_addrs, m.subject, m.snippet, m.body_text, m.body_html, m.status,
			COALESCE(m.error, ''), m.sent_at, COALESCE(m.thread_id, ''), COALESCE(m.rfc_message_id, ''), COALESCE(m.in_reply_to, ''),
			COALESCE(m.mail_account_id::text, ''), COALESCE(ma.visibility, 'share_everything'), COALESCE(ma.identity_id, '00000000-0000-0000-0000-000000000000')
		FROM crm.message_links l JOIN crm.messages m ON m.id = l.message_id LEFT JOIN crm.mail_accounts ma ON ma.id = m.mail_account_id
		WHERE l.workspace_id = $1 AND l.object_key = $2 AND l.record_id = $3 ORDER BY m.sent_at DESC LIMIT 300) x ORDER BY sent_at`, ws, spec.Key, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []threadRow{}
	for rows.Next() {
		var tr threadRow
		var body, html, visibility, mailbox string
		var owner uuid.UUID
		m := &tr.msg
		if err := rows.Scan(&m.ID, &m.Direction, &m.From, &m.FromName, &m.To, &m.Cc, &m.Subject, &tr.snippet, &body, &html, &m.Status, &m.Error, &m.At,
			&tr.thread, &tr.rfcID, &tr.replyTo, &mailbox, &visibility, &owner); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		switch {
		case owner == me || visibility == "share_everything":
			m.Body, m.HTML = body, sanitizeRich(html)
			if m.Body == "" && m.HTML == "" {
				m.Body = tr.snippet
			}
		case visibility == "subject":
			m.Hidden, tr.snippet = "body", ""
		default:
			m.Hidden, m.Subject, m.Cc, tr.snippet = "all", "", nil, ""
		}
		if owner == me {
			m.Mailbox = mailbox
		}
		list = append(list, tr)
	}
	if err := rows.Err(); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": groupThreads(list)})
}
