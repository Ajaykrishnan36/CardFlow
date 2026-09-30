package records

import (
	"testing"
	"time"
)

func TestReplySubject(t *testing.T) {
	for in, want := range map[string]string{"Pricing": "Re: Pricing", "RE: Pricing": "Re: Pricing", "Re: Fwd: Pricing": "Re: Pricing", "": "Re:"} {
		if got := replySubject(in); got != want {
			t.Errorf("replySubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupThreads(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	row := func(id, subject, thread, rfc, reply string, minutes int) threadRow {
		return threadRow{msg: ThreadMessage{ID: id, Subject: subject, From: id + "@x.com", At: at.Add(time.Duration(minutes) * time.Minute)},
			thread: thread, rfcID: rfc, replyTo: reply}
	}
	got := groupThreads([]threadRow{
		row("a", "Pricing", "crm:a", "<a@x>", "", 0),
		row("b", "Hello", "g1", "", "", 1),
		row("c", "Re: Pricing", "g2", "<c@x>", "<a@x>", 2), // answers a
		row("d", "Other", "g1", "", "", 3),                 // same provider thread as b
		row("e", "RE: pricing", "", "", "", 4),             // same subject as a
	})
	if len(got) != 2 {
		t.Fatalf("want 2 threads, got %d: %+v", len(got), got)
	}
	if got[0].ID != "a" || got[0].Count != 3 || got[1].ID != "b" || got[1].Count != 2 {
		t.Fatalf("unexpected grouping: %+v", got)
	}
}
