package shared

import "testing"

func TestCheckGSTIN(t *testing.T) {
	if g, msg := CheckGSTIN(" 27aapfu0939f1zv ", "Maharashtra"); msg != "" || g != "27AAPFU0939F1ZV" {
		t.Fatalf("a valid GSTIN passes and is upper-cased: %q %q", g, msg)
	}
	if _, msg := CheckGSTIN("27AAPFU0939F1ZW", ""); msg == "" {
		t.Fatal("a wrong check character is caught")
	}
	if _, msg := CheckGSTIN("27AAPFU0939F1ZV", "Tamil Nadu"); msg == "" {
		t.Fatal("a GSTIN from another state is caught")
	}
	if _, msg := CheckGSTIN("12345", ""); msg == "" {
		t.Fatal("a short GSTIN is caught")
	}
	if s, ok := IndianStateNamed("tamilnadu"); !ok || s.GSTCode != "33" {
		t.Fatal("state spellings are understood")
	}
}
