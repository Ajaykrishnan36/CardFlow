package platform

import (
	"testing"
	"time"
)

func TestCodePartsUseIndiaDateAndISOWeek(t *testing.T) {
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, india)
	if got := codeParts(at, "ABCDE1234F"); got != "27102644234f" {
		t.Fatalf("27 Oct 2026, week 44, PAN …234F: %s", got)
	}
	// 31 Dec 2026 19:00 UTC is already 1 Jan 2027 in India, ISO week 53 of 2026.
	if got := codeParts(time.Date(2026, 12, 31, 19, 0, 0, 0, time.UTC), "33AAAAA0000A1Z5"); got != "01012753a1z5" {
		t.Fatalf("the date is India's: %s", got)
	}
	if got := codeParts(at, ""); got != "271026440000" {
		t.Fatalf("no ID gives 0000: %s", got)
	}
	if DisplayCode("27102644234f000501") != "271026-44-234F-000501" || NormalizeCode(" 271026-44-234F-000501 ") != "27102644234f000501" {
		t.Fatal("display and typed forms")
	}
}

// The six digits are a keyed shuffle of the running number (D-165): each number its own
// digits, in no order that follows the running number, and a different key gives others.
func TestCodeNumberIsAKeyedShuffle(t *testing.T) {
	seen := make(map[int]int64, 1_000_000)
	for n := int64(0); n < 1_000_000; n++ {
		d := codeNumber(n)
		if d < 0 || d > 999_999 {
			t.Fatalf("number %d gave %d, not six digits", n, d)
		}
		if prev, ok := seen[d]; ok {
			t.Fatalf("numbers %d and %d both gave %06d", prev, n, d)
		}
		seen[d] = n
	}
	in := 0
	for n := int64(1); n <= 100; n++ {
		if diff := codeNumber(n+1) - codeNumber(n); diff >= -10 && diff <= 10 {
			in++
		}
	}
	if in > 2 {
		t.Fatalf("%d of 100 next codes sit within 10 of the one before: guessable", in)
	}
	before := codeNumber(501)
	old := codeKey
	SetCodeKey([]byte("another server secret"))
	defer func() { codeKey = old }()
	if codeNumber(501) == before {
		t.Fatal("the server key must change the digits")
	}
}
