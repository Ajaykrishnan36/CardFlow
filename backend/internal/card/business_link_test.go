package card

import (
	"testing"

	"cardflow-backend/internal/domain"
)

func TestNormalizeGSTIN(t *testing.T) {
	cases := map[string]string{
		"33abcde1234f1z5":    "33ABCDE1234F1Z5",
		" 33 ABCDE-1234F1Z5": "33ABCDE1234F1Z5",
		"33DF345665":         "", // too short
		"ABCDE1234F1Z533":    "", // must start with the state code
		"":                   "",
	}
	for in, want := range cases {
		if got := NormalizeGSTIN(in); got != want {
			t.Errorf("NormalizeGSTIN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBusinessFromCard(t *testing.T) {
	site := "murugan.in"
	f := businessFromCard(domain.SavedCard{
		PersonName: " Murugan K ", Company: "", GSTIN: "33murug1234k1z5", Website: &site,
		Phones: []domain.CardPhone{
			{Raw: "0422 223344"},                        // landline: not a mobile, skipped
			{E164: "+91 98123 00009", IsWhatsApp: true}, // WhatsApp only
			{Raw: "98123-00001"},                        // primary
		},
		Emails: []string{"a@b.in"}, RawAddress: "12 Avinashi Rd, Coimbatore", Pincode: "6410",
	})
	if f.GSTIN != "33MURUG1234K1Z5" || f.Name != "Murugan K" || f.ContactName != "Murugan K" {
		t.Fatalf("names/gstin wrong: %+v", f)
	}
	if f.Phone != "+919812300001" || f.WhatsApp != "+919812300009" {
		t.Fatalf("phones wrong: %+v", f)
	}
	if f.Address != "12 Avinashi Rd, Coimbatore" || f.Pincode != "" || f.Email != "a@b.in" || f.Website != "murugan.in" {
		t.Fatalf("details wrong: %+v", f)
	}
	// Only a WhatsApp number → it is also the contact phone (used to match the owner).
	g := businessFromCard(domain.SavedCard{GSTIN: "33MURUG1234K1Z5", Phones: []domain.CardPhone{{E164: "9812300009", IsWhatsApp: true}}})
	if g.Phone != "+919812300009" {
		t.Fatalf("whatsapp fallback: %+v", g)
	}
}

func TestValidateCard(t *testing.T) {
	phone := []domain.CardPhone{{Raw: "+91 98123 00001"}}
	cases := []struct {
		name string
		card domain.SavedCard
		ok   bool
	}{
		{"manual complete", domain.SavedCard{Source: "MANUAL", PersonName: "A", GSTIN: "33ABCDE1234F1Z5", Phones: phone}, true},
		{"manual no name", domain.SavedCard{Source: "MANUAL", GSTIN: "33ABCDE1234F1Z5", Phones: phone}, false},
		{"manual no phone", domain.SavedCard{Source: "MANUAL", PersonName: "A", GSTIN: "33ABCDE1234F1Z5"}, false},
		{"manual no gstin", domain.SavedCard{Source: "MANUAL", PersonName: "A", Phones: phone}, false},
		{"manual bad gstin", domain.SavedCard{Source: "MANUAL", PersonName: "A", GSTIN: "33ABC", Phones: phone}, false},
		{"manual bad phone", domain.SavedCard{Source: "MANUAL", PersonName: "A", GSTIN: "33ABCDE1234F1Z5", Phones: []domain.CardPhone{{Raw: "123"}}}, false},
		{"scanned company only", domain.SavedCard{Source: "SCANNED", Company: "Acme"}, true},
		{"scanned empty", domain.SavedCard{Source: "SCANNED"}, false},
	}
	for _, c := range cases {
		if got := validateCard(c.card) == ""; got != c.ok {
			t.Errorf("%s: valid=%v, want %v (%q)", c.name, got, c.ok, validateCard(c.card))
		}
	}
}
