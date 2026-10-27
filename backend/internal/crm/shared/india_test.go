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

func TestCheckTaxID(t *testing.T) {
	if v, msg := CheckTaxID("pan", "abcde1234f", ""); msg != "" || v != "ABCDE1234F" {
		t.Fatalf("a valid PAN passes: %q %q", v, msg)
	}
	if _, msg := CheckTaxID("pan", "ABCDE12345", ""); msg == "" {
		t.Fatal("a PAN ending in a digit is caught")
	}
	if _, msg := CheckTaxID("tan", "ABCD12345E", ""); msg != "" {
		t.Fatalf("a valid TAN passes: %q", msg)
	}
	if _, msg := CheckTaxID("tan", "ABCDE1234F", ""); msg == "" {
		t.Fatal("a PAN is not a TAN")
	}
	if _, msg := CheckTaxID("cin", "X", ""); msg == "" {
		t.Fatal("unknown kinds are refused")
	}
}
