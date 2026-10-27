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
