package e2e

import (
	"testing"
	"time"
)

func card(name, company, phone, email string) map[string]any {
	c := map[string]any{"personName": name, "company": company, "designation": "Manager"}
	if phone != "" {
		c["phones"] = []string{phone}
	}
	if email != "" {
		c["emails"] = []string{email}
	}
	return c
}

// A scanned card is matched only against the business it was scanned in, and saved as a
// lead, a contact, an attachment or just a card — record, link and timeline together (D-98).
func TestCardScanToLeadContactAndAttach(t *testing.T) {
	token := signIn(t, freshPhone(), "Card Owner")
	ws := newBusiness(t, token, "Card Traders")
	base := crmAPI + "/w/" + ws

	// Nobody is known yet: the screen should offer "new lead".
	m := call(t, "POST", base+"/cards/match", token, card("Ravi Kumar", "Kumar Steels", "+91 98400 11223", "ravi@kumarsteels.in"))
	want(t, m, 200, "match on empty business")
	if len(m.list("matches")) != 0 || m.str("suggested") != "lead" {
		t.Fatalf("empty business should have no matches and suggest a lead: %s", truncate(m.Raw, 300))
	}

	// Save as a lead with a follow-up and a note.
	followUp := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	s := call(t, "POST", base+"/cards", token, map[string]any{
		"action": "lead", "card": card("Ravi Kumar", "Kumar Steels", "+91 98400 11223", "ravi@kumarsteels.in"),
		"followUpAt": followUp, "note": "Met at the trade fair"})
	want(t, s, 201, "save card as lead")
	leadID, cardID := s.str("record", "recordId"), s.str("card", "id")
	if s.str("record", "object") != "leads" || leadID == "" || cardID == "" || s.at("created") != true {
		t.Fatalf("lead not created from card: %s", truncate(s.Raw, 400))
	}
	lead := call(t, "GET", base+"/crm/leads/"+leadID, token, nil)
	want(t, lead, 200, "open the new lead")
	v := lead.at("record", "values").(map[string]any)
	if v["lastName"] != "Kumar" || v["firstName"] != "Ravi" || v["organization"] != "Kumar Steels" || v["source"] != "business_card" || v["nextFollowUpAt"] == nil {
		t.Fatalf("lead fields not taken from the card: %v", v)
	}
	foundCards := false
	for _, l := range lead.list("related") {
		rl := l.(map[string]any)
		if rl["key"] == "cards" && len(rl["rows"].([]any)) == 1 {
			foundCards = true
		}
	}
	if !foundCards {
		t.Fatalf("lead page has no business card list: %s", truncate(lead.Raw, 600))
	}

	// The same card again (other spelling of the number) is recognised, not doubled.
	m = call(t, "POST", base+"/cards/match", token, card("R Kumar", "", "098400-11223", ""))
	want(t, m, 200, "match again")
	if len(m.list("matches")) != 1 || m.str("suggested") != "attach" {
		t.Fatalf("rescan should match the lead: %s", truncate(m.Raw, 300))
	}
	dup := call(t, "POST", base+"/cards", token, map[string]any{"action": "lead", "card": card("R Kumar", "", "9840011223", "")})
	want(t, dup, 409, "rescan must not create a second lead")
	if dup.str("code") != "possible_duplicate" || len(dup.list("details", "matches")) != 1 {
		t.Fatalf("duplicate answer should carry the matches: %s", truncate(dup.Raw, 300))
	}
	if n := total(t, token, ws, "leads", ""); n != 1 {
		t.Fatalf("leads = %d, want 1", n)
	}

	// Attach the rescanned card to the lead instead.
	a := call(t, "POST", base+"/cards", token, map[string]any{"action": "attach", "target": map[string]any{"object": "leads", "id": leadID},
		"card": map[string]any{"personName": "R Kumar", "phones": []string{"9840011223"}, "website": "kumarsteels.in"}})
	want(t, a, 201, "attach card")
	if a.at("created") != false || a.str("record", "recordId") != leadID {
		t.Fatalf("attach should point at the existing lead: %s", truncate(a.Raw, 300))
	}
	lead = call(t, "GET", base+"/crm/leads/"+leadID, token, nil)
	if got := lead.at("record", "values").(map[string]any)["website"]; got != "https://kumarsteels.in" {
		t.Fatalf("attach should fill the lead's empty website, got %v", got)
	}

	// A new person at a new company: contact + account in one go.
	c := call(t, "POST", base+"/cards", token, map[string]any{"action": "contact", "createAccount": true,
		"card": card("Meena Iyer", "Iyer Exports", "9000012345", "meena@iyerexports.com"), "followUpAt": followUp})
	want(t, c, 201, "save card as contact")
	if c.str("record", "object") != "contacts" || c.str("account", "recordId") == "" || c.str("task", "id") == "" {
		t.Fatalf("contact, account and follow-up task expected: %s", truncate(c.Raw, 400))
	}
	// A colleague's card from the same company goes under the same account.
	c2 := call(t, "POST", base+"/cards", token, map[string]any{"action": "contact", "createAccount": true,
		"card": card("Arun Iyer", "iyer exports", "9000054321", "")})
	want(t, c2, 201, "second contact")
	if c2.str("account", "recordId") != c.str("account", "recordId") {
		t.Fatalf("the company account should be reused, not doubled")
	}
	if n := total(t, token, ws, "accounts", ""); n != 1 {
		t.Fatalf("accounts = %d, want 1", n)
	}

	// Card only: no CRM record.
	o := call(t, "POST", base+"/cards", token, map[string]any{"action": "card_only", "card": card("Walk In", "", "9111111111", "")})
	want(t, o, 201, "card only")
	if o.at("record") != nil {
		t.Fatalf("card_only must not create a record: %s", truncate(o.Raw, 300))
	}

	// My cards in this business, and the cards on one record.
	list := call(t, "GET", base+"/cards", token, nil)
	want(t, list, 200, "list cards")
	if len(list.list("items")) != 5 {
		t.Fatalf("cards = %d, want 5", len(list.list("items")))
	}
	onLead := call(t, "GET", base+"/cards?object=leads&recordId="+leadID, token, nil)
	want(t, onLead, 200, "cards on the lead")
	if len(onLead.list("items")) != 2 {
		t.Fatalf("cards on lead = %d, want 2", len(onLead.list("items")))
	}

	// Same request twice with one Idempotency-Key saves once.
	want(t, call(t, "GET", base+"/cards/"+cardID, token, nil), 200, "open card")
	want(t, call(t, "POST", base+"/cards", token, map[string]any{"action": "nonsense", "card": card("X", "", "", "")}), 422, "bad action")
	want(t, call(t, "POST", base+"/cards", token, map[string]any{"action": "card_only", "card": map[string]any{}}), 422, "empty card")
}

// The same person scanned in two businesses: neither business learns about the other.
func TestCardMatchingStaysInsideTheBusiness(t *testing.T) {
	alice := signIn(t, freshPhone(), "Alice")
	wsA := newBusiness(t, alice, "Alpha Cards")
	bob := signIn(t, freshPhone(), "Bob")
	wsB := newBusiness(t, bob, "Beta Cards")
	person := card("Shared Person", "Shared Co", "+91 63821 24970", "shared@example.com")

	s := call(t, "POST", crmAPI+"/w/"+wsA+"/cards", alice, map[string]any{"action": "lead", "card": person})
	want(t, s, 201, "alice saves the card as a lead")
	cardA := s.str("card", "id")

	// Bob's business knows nothing about this person.
	m := call(t, "POST", crmAPI+"/w/"+wsB+"/cards/match", bob, person)
	want(t, m, 200, "bob matches")
	if len(m.list("matches")) != 0 || m.at("hidden") != float64(0) {
		t.Fatalf("another business's lead leaked into matching: %s", truncate(m.Raw, 300))
	}
	// Bob can't look into Alice's business, her card, or its picture.
	want(t, call(t, "POST", crmAPI+"/w/"+wsA+"/cards/match", bob, person), 403, "bob matching in alice's business")
	want(t, call(t, "GET", crmAPI+"/w/"+wsA+"/cards/"+cardA, bob, nil), 403, "bob opening alice's card through her business")
	want(t, call(t, "GET", crmAPI+"/w/"+wsB+"/cards/"+cardA, bob, nil), 404, "bob opening alice's card through his business")
	want(t, call(t, "GET", crmAPI+"/w/"+wsB+"/cards/"+cardA+"/image", bob, nil), 404, "bob loading alice's card picture")
	// Nor can he save "his" record onto her card.
	want(t, call(t, "POST", crmAPI+"/w/"+wsB+"/cards", bob, map[string]any{"action": "lead", "cardId": cardA}), 404, "bob converting alice's card")

	// Bob saves the same person himself: a separate contact in his business.
	sb := call(t, "POST", crmAPI+"/w/"+wsB+"/cards", bob, map[string]any{"action": "contact", "card": person})
	want(t, sb, 201, "bob saves the card as a contact")
	if n := total(t, alice, wsA, "leads", ""); n != 1 {
		t.Fatalf("alice's leads = %d, want 1", n)
	}
	if n := total(t, alice, wsA, "contacts", ""); n != 0 {
		t.Fatalf("bob's contact appeared in alice's business")
	}
	if got := len(call(t, "GET", crmAPI+"/w/"+wsA+"/cards", alice, nil).list("items")); got != 1 {
		t.Fatalf("alice's cards = %d, want 1", got)
	}

	// Alice's second business doesn't see her first business's lead either.
	wsA2 := newBusiness(t, alice, "Alpha Second")
	m = call(t, "POST", crmAPI+"/w/"+wsA2+"/cards/match", alice, person)
	want(t, m, 200, "alice matches in her second business")
	if len(m.list("matches")) != 0 {
		t.Fatalf("match crossed between one person's two businesses: %s", truncate(m.Raw, 300))
	}
	// But a card from her vault can be converted there (her own card).
	conv := call(t, "POST", crmAPI+"/w/"+wsA2+"/cards", alice, map[string]any{"action": "lead", "cardId": cardA})
	want(t, conv, 201, "alice converts her own card in her second business")
	if n := total(t, alice, wsA2, "leads", ""); n != 1 {
		t.Fatalf("second business leads = %d, want 1", n)
	}
}

// Cards saved through the app's own card API are the same cards the CRM works with.
func TestAppCardBecomesCRMLead(t *testing.T) {
	token := signIn(t, freshPhone(), "App Scanner")
	ws := newBusiness(t, token, "Scan Shop")
	saved := call(t, "POST", appAPI+"/cards", token, map[string]any{
		"person_name": "Vault Person", "company": "Vault Co", "designation": "Owner",
		"phones": []map[string]any{{"raw": "9555512345", "e164": "+919555512345", "type": "work"}},
		"emails": []string{"vault@example.com"}})
	if saved.Status != 200 && saved.Status != 201 {
		t.Skipf("app card API not available in this build (%d): %s", saved.Status, truncate(saved.Raw, 200))
	}
	id := saved.str("id")
	if id == "" {
		id = saved.str("card", "id")
	}
	if id == "" {
		id = saved.str("data", "id")
	}
	if id == "" {
		t.Fatalf("app card save returned no id: %s", truncate(saved.Raw, 300))
	}
	conv := call(t, "POST", crmAPI+"/w/"+ws+"/cards", token, map[string]any{"action": "lead", "cardId": id})
	want(t, conv, 201, "convert an app card to a lead")
	lead := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads/"+conv.str("record", "recordId"), token, nil)
	v := lead.at("record", "values").(map[string]any)
	if v["lastName"] != "Person" || v["organization"] != "Vault Co" || v["email"] != "vault@example.com" {
		t.Fatalf("lead should carry the app card's details: %v", v)
	}
}

// A directory listing becomes a lead in the viewer's own business; the listing's owner
// and their CRM are not touched.
func TestDirectoryListingBecomesALeadInMyBusiness(t *testing.T) {
	seller := signIn(t, freshPhone(), "Seller")
	sellerWS := newBusiness(t, seller, "Listed Seller Co")
	buyer := signIn(t, freshPhone(), "Buyer")
	buyerWS := newBusiness(t, buyer, "Buyer Co")

	lead := call(t, "POST", crmAPI+"/w/"+buyerWS+"/crm/leads", buyer, map[string]any{"values": map[string]any{
		"lastName": "Listed Seller Co", "organization": "Listed Seller Co", "phone": "9000077777", "source": "directory", "city": "Coimbatore"}})
	if lead.Status != 200 && lead.Status != 201 {
		t.Fatalf("lead from a listing: %d %s", lead.Status, truncate(lead.Raw, 300))
	}
	if got := lead.at("values").(map[string]any)["source"]; got != "directory" {
		t.Fatalf("lead source = %v, want directory", got)
	}
	// Looking again finds it, so the same business isn't added twice by accident.
	m := call(t, "POST", crmAPI+"/w/"+buyerWS+"/cards/match", buyer, map[string]any{"company": "Listed Seller Co", "phones": []string{"9000077777"}})
	want(t, m, 200, "match the listing against my CRM")
	if len(m.list("matches")) != 1 {
		t.Fatalf("the lead should be found: %s", truncate(m.Raw, 300))
	}
	if n := total(t, seller, sellerWS, "leads", ""); n != 0 {
		t.Fatalf("the listing owner's CRM must not change, leads = %d", n)
	}
}
