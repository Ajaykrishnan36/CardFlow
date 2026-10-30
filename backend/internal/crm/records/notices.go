package records

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/billing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Business notifications (D-169): the bell tells people what happened in their business —
// a member added or signing in for the first time, a payment for members, paid time
// running out (7, 5, 3 and 1 day before, and once it ends), a sale (deal won, invoice paid,
// bike delivered, policy in force) and work booked for a service person. Record
// assignments were already told to the new owner (notifyForEvent).

// leaders are a business's active members ranked Admin or above.
func leaders(ctx context.Context, q querier, ws uuid.UUID, minRank int) []uuid.UUID {
	rows, err := q.Query(ctx, `
		SELECT m.identity_id FROM crm.memberships m
		WHERE m.workspace_id = $1 AND m.status = 'active'
		  AND COALESCE((SELECT max(r.rank) FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
		                WHERE ra.membership_id = m.id AND (ra.expires_at IS NULL OR ra.expires_at > now())), 0) >= $2`, ws, minRank)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// others drops the people in skip (and duplicates) from a list.
func others(list []uuid.UUID, skip ...uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{uuid.Nil: true}
	for _, s := range skip {
		seen[s] = true
	}
	var out []uuid.UUID
	for _, id := range list {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

type businessInfo struct{ code, name string }

func businessOf(ctx context.Context, q querier, ws uuid.UUID) (businessInfo, bool) {
	var b businessInfo
	var isPlatform bool
	if err := q.QueryRow(ctx, `SELECT code, name, is_platform FROM crm.workspaces WHERE id = $1`, ws).Scan(&b.code, &b.name, &isPlatform); err != nil {
		return b, false
	}
	return b, !isPlatform
}

func (b businessInfo) link(path string) string { return "/crm/w/" + b.code + "/" + path }

var ist = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return l
	}
	return time.FixedZone("IST", 5*3600+1800)
}()

func dayText(t time.Time) string { return t.In(ist).Format("2 Jan 2006") }

// rupeesText writes an amount the Indian way: ₹1,47,480.30.
func rupeesText(v float64) string {
	neg := v < 0
	v = math.Abs(math.Round(v*100) / 100)
	whole := int64(v)
	paise := int64(math.Round((v - float64(whole)) * 100))
	s := strconv.FormatInt(whole, 10)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		s = strings.Join(parts, ",") + "," + tail
	}
	if paise > 0 {
		s += fmt.Sprintf(".%02d", paise)
	}
	if neg {
		return "-₹" + s
	}
	return "₹" + s
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func nameOf(ctx context.Context, q querier, id uuid.UUID) string {
	name := "Someone"
	if id != uuid.Nil {
		_ = q.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, id).Scan(&name)
	}
	return name
}

// roleNameOf is a membership's highest role, for "added as Sales staff".
func roleNameOf(ctx context.Context, q querier, membership uuid.UUID) string {
	var role string
	_ = q.QueryRow(ctx, `SELECT r.name FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
		WHERE ra.membership_id = $1 AND (ra.expires_at IS NULL OR ra.expires_at > now()) ORDER BY r.rank DESC LIMIT 1`, membership).Scan(&role)
	return role
}

// NotifyMemberAdded: the new person is welcomed and the business's leaders are told.
func (h *Handler) NotifyMemberAdded(ctx context.Context, ws, membership, actorID uuid.UUID) {
	q := h.store.Pool
	b, ok := businessOf(ctx, q, ws)
	if !ok {
		return
	}
	var person uuid.UUID
	if err := q.QueryRow(ctx, `SELECT identity_id FROM crm.memberships WHERE id = $1`, membership).Scan(&person); err != nil {
		return
	}
	who, by, role := nameOf(ctx, q, person), nameOf(ctx, q, actorID), roleNameOf(ctx, q, membership)
	as := ""
	if role != "" {
		as = " as " + role
	}
	var actor *uuid.UUID
	if actorID != uuid.Nil {
		actor = &actorID
	}
	h.notify(ctx, q, ws, person, "member.welcome", "You were added to "+b.name+as, by+" added you. You can sign in at your business address.", b.link("home"), actor)
	for _, id := range others(leaders(ctx, q, ws, access.RankAdmin), actorID, person) {
		h.notify(ctx, q, ws, id, "member.added", by+" added "+who+as, "They can sign in once they are paid for.", b.link("settings/access"), actor)
	}
}

// NotifyFirstSignIn: whoever added the person, and the leaders, hear that they are in.
func (h *Handler) NotifyFirstSignIn(ctx context.Context, person uuid.UUID) {
	q := h.store.Pool
	rows, err := q.Query(ctx, `SELECT id, workspace_id, created_by FROM crm.memberships WHERE identity_id = $1 AND status = 'active'`, person)
	if err != nil {
		return
	}
	type m struct {
		id, ws uuid.UUID
		by     *uuid.UUID
	}
	var list []m
	for rows.Next() {
		var x m
		if rows.Scan(&x.id, &x.ws, &x.by) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	who := nameOf(ctx, q, person)
	for _, x := range list {
		b, ok := businessOf(ctx, q, x.ws)
		if !ok {
			continue
		}
		role := roleNameOf(ctx, q, x.id)
		body := b.name
		if role != "" {
			body = role + " · " + b.name
		}
		to := leaders(ctx, q, x.ws, access.RankAdmin)
		if x.by != nil {
			to = append([]uuid.UUID{*x.by}, to...)
		}
		for _, id := range others(to, person) {
			h.notify(ctx, q, x.ws, id, "member.first_sign_in", who+" signed in for the first time", body, b.link("settings/access"), &person)
		}
	}
}

// NotifyPaid: the payer, each person paid for and the other Super Admins hear about a payment.
func (h *Handler) NotifyPaid(ctx context.Context, p billing.Paid) {
	q := h.store.Pool
	b, ok := businessOf(ctx, q, p.Workspace)
	if !ok {
		return
	}
	rows, err := q.Query(ctx, `SELECT m.identity_id, i.display_name, m.paid_until FROM crm.memberships m
		JOIN crm.identities i ON i.id = m.identity_id WHERE m.id = ANY($1) ORDER BY i.display_name`, p.Memberships)
	if err != nil {
		return
	}
	type paidFor struct {
		id    uuid.UUID
		name  string
		until *time.Time
	}
	var list []paidFor
	for rows.Next() {
		var x paidFor
		if rows.Scan(&x.id, &x.name, &x.until) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	payer := uuid.Nil
	if p.PaidBy != nil {
		payer = *p.PaidBy
	}
	byName := nameOf(ctx, q, payer)
	months := fmt.Sprintf("%d months", p.Months)
	if p.Months == 1 {
		months = "1 month"
	}
	amount := rupeesText(float64(p.AmountPaise) / 100)
	people := fmt.Sprintf("%d people", len(list))
	if len(list) == 1 {
		people = list[0].name
	}
	until := ""
	if u := list[0].until; u != nil {
		until = " · paid until " + dayText(*u)
	}
	link := b.link("settings/billing")
	if payer != uuid.Nil {
		h.notify(ctx, q, p.Workspace, payer, "billing.paid", "Payment received: "+amount, "For "+people+", "+months+until, link, p.PaidBy)
	}
	for _, x := range list {
		if x.id == payer || x.until == nil {
			continue
		}
		h.notify(ctx, q, p.Workspace, x.id, "billing.paid_for_you", "Your access is paid until "+dayText(*x.until), byName+" paid for "+months+".", link, p.PaidBy)
	}
	skip := []uuid.UUID{payer}
	for _, x := range list {
		skip = append(skip, x.id)
	}
	for _, id := range others(leaders(ctx, q, p.Workspace, access.RankSuperAdmin), skip...) {
		h.notify(ctx, q, p.Workspace, id, "billing.paid", byName+" paid "+amount, "For "+people+", "+months+until, link, p.PaidBy)
	}
}

// reminderStep is which reminder is due with this much paid time left: 7, 5, 3 or 1 day
// before, 0 once it has ended.
func reminderStep(left time.Duration) int {
	if left <= 0 {
		return 0
	}
	days := int(math.Ceil(left.Hours() / 24))
	for _, s := range []int{1, 3, 5, 7} {
		if days <= s {
			return s
		}
	}
	return -1
}

func daysText(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// sweepBilling sends the "paid time is running out" reminders, each once.
func (h *Handler) sweepBilling(ctx context.Context) {
	if h.billing == nil || !h.billing.Enforced(ctx) {
		return
	}
	q := h.store.Pool
	rows, err := q.Query(ctx, `
		SELECT m.id, m.workspace_id, m.identity_id, i.display_name, m.paid_until
		FROM crm.memberships m
		JOIN crm.identities i ON i.id = m.identity_id
		JOIN crm.workspaces w ON w.id = m.workspace_id
		WHERE m.status = 'active' AND m.paid_until IS NOT NULL AND NOT w.is_platform
		  AND m.paid_until <= now() + interval '7 days' AND m.paid_until > now() - interval '2 days'
		LIMIT 2000`)
	if err != nil {
		slog.Warn("crm: billing reminders", "error", err)
		return
	}
	type due struct {
		id, ws, person uuid.UUID
		name           string
		until          time.Time
	}
	var list []due
	for rows.Next() {
		var x due
		if rows.Scan(&x.id, &x.ws, &x.person, &x.name, &x.until) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		b, ok := businessOf(ctx, q, x.ws)
		if !ok || billing.Exempt(false, b.code) {
			continue
		}
		step := reminderStep(time.Until(x.until))
		if step < 0 {
			continue
		}
		var sent bool
		if err := q.QueryRow(ctx, `INSERT INTO crm.billing_reminders (membership_id, paid_until, days) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING RETURNING true`, x.id, x.until, step).Scan(&sent); err != nil || !sent {
			continue
		}
		link := b.link("settings/billing")
		mine, theirs, body := "Your access ends in "+daysText(step), x.name+"’s access ends in "+daysText(step), "Paid until "+dayText(x.until)+". Pay in Billing to keep access."
		if step == 0 {
			mine, theirs, body = "Your access has ended", x.name+"’s access has ended", "It was paid until "+dayText(x.until)+". Pay in Billing to sign in again."
		}
		h.notify(ctx, q, x.ws, x.person, "billing.expiring", mine, body, link, nil)
		for _, id := range others(leaders(ctx, q, x.ws, access.RankSuperAdmin), x.person) {
			h.notify(ctx, q, x.ws, id, "billing.expiring", theirs, body, link, nil)
		}
	}
}

// saleNotices: what counts as a sale or a hand-over the business wants to hear about,
// by object and the status it moved to.
var saleNotices = map[string]map[string]string{
	"opportunities":      {"closed_won": "Deal won"},
	"invoices":           {"paid": "Invoice paid"},
	"vehicle_bookings":   {"delivered": "Vehicle delivered"},
	"insurance_policies": {"in_force": "Policy in force"},
}

// newNotices: records whose creation is news by itself.
var newNotices = map[string]string{"vehicle_bookings": "New booking"}

var saleAmountKeys = []string{"total", "amount", "onRoadPrice", "premium", "bookingAmount"}

// notifyBusinessEvent tells the leaders (and the record's owner) about a sale, and a
// service person about work booked for them.
func (h *Handler) notifyBusinessEvent(ctx context.Context, tx pgx.Tx, ev Event) {
	if ev.Type != "record.updated" && ev.Type != "record.created" {
		return
	}
	changed := map[string]bool{}
	for _, c := range ev.Changed {
		changed[c] = true
	}
	created := ev.Type == "record.created"
	b, ok := businessOf(ctx, tx, ev.WorkspaceID)
	if !ok {
		return
	}
	actor := uuid.Nil
	if ev.ActorID != nil {
		actor = *ev.ActorID
	}
	link := b.link(ev.Object + "/" + ev.RecordID.String())
	title := ev.Title
	if title == "" {
		title, _ = ev.Record["title"].(string)
	}

	// A sale or hand-over.
	status, _ := ev.Record["status"].(string)
	head, sale := saleNotices[ev.Object][status]
	sale = sale && (created || changed["status"])
	if n, ok := newNotices[ev.Object]; ok && created {
		head, sale = n, true
	}
	if sale {
		{
			body := title
			for _, k := range saleAmountKeys {
				if v, ok := numberOf(ev.Record[k]); ok && v > 0 {
					body += " · " + rupeesText(v)
					break
				}
			}
			if actor != uuid.Nil {
				body += " · by " + nameOf(ctx, tx, actor)
			}
			to := leaders(ctx, tx, ev.WorkspaceID, access.RankAdmin)
			if owner, err := uuid.Parse(fmt.Sprint(ev.Record["ownerId"])); err == nil {
				to = append(to, owner)
			}
			for _, id := range others(to, actor) {
				h.notify(ctx, tx, ev.WorkspaceID, id, "sale."+ev.Object, head+": "+title, body, link, ev.ActorID)
			}
		}
	}

	// Work booked for a service person: a job card or appointment given a resource, or an
	// appointment assigned to someone.
	if ev.Object != "work_orders" && ev.Object != "appointments" {
		return
	}
	var people []uuid.UUID
	if res, ok := ev.Record["resourceId"].(string); ok && res != "" && (created || changed["resourceId"]) {
		var user string
		if tx.QueryRow(ctx, `SELECT COALESCE(custom->>'userId', '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, res, ev.WorkspaceID).Scan(&user) == nil {
			if id, err := uuid.Parse(user); err == nil {
				people = append(people, id)
			}
		}
	}
	if a, ok := ev.Record["assignedTo"].(string); ok && a != "" && (created || changed["assignedTo"]) {
		if id, err := uuid.Parse(a); err == nil {
			people = append(people, id)
		}
	}
	owner, _ := uuid.Parse(fmt.Sprint(ev.Record["ownerId"]))
	what := "a job card"
	if ev.Object == "appointments" {
		what = "an appointment"
	}
	when := ""
	for _, k := range []string{"startsAt", "scheduledStart"} {
		if s, ok := ev.Record[k].(string); ok && s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				when = " · " + t.In(ist).Format("2 Jan, 3:04 pm")
			}
			break
		}
	}
	// The record's owner already hears "assigned you" from notifyForEvent.
	for _, id := range others(people, actor, owner) {
		h.notify(ctx, tx, ev.WorkspaceID, id, "work.booked", nameOf(ctx, tx, actor)+" booked you for "+what, title+when, link, ev.ActorID)
	}
}
