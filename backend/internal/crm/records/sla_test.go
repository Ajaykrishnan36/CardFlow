package records

import (
	"testing"
	"time"
)

// SLA clocks count only working time when the policy says so (D-115).
func TestBusinessHoursClock(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, ist)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	h := Hours{BusinessOnly: true, Start: "09:00", End: "18:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-10-06"}, TZ: "Asia/Kolkata"}
	for _, tc := range []struct {
		name, start string
		minutes     int
		want        string
	}{
		{"inside the day", "2026-10-05 10:00", 120, "2026-10-05 12:00"},         // Monday
		{"spills over a holiday", "2026-10-05 17:00", 120, "2026-10-07 10:00"},  // Tuesday is a holiday
		{"starts before opening", "2026-10-07 06:00", 60, "2026-10-07 10:00"},   // Wednesday
		{"starts after closing", "2026-10-07 20:00", 30, "2026-10-08 09:30"},    // → Thursday
		{"skips the weekend", "2026-10-09 17:30", 60, "2026-10-12 09:30"},       // Friday → Monday
		{"starts on a weekend", "2026-10-10 12:00", 9 * 60, "2026-10-12 18:00"}, // Saturday → all of Monday
		{"several days", "2026-10-07 09:00", 3 * 9 * 60, "2026-10-09 18:00"},    // Wed, Thu, Fri
	} {
		if got := h.Add(at(tc.start), tc.minutes); !got.Equal(at(tc.want)) {
			t.Errorf("%s: %s + %d min = %s, want %s", tc.name, tc.start, tc.minutes, got.In(ist).Format("2006-01-02 15:04"), tc.want)
		}
	}
	// Time waited while paused counts only the working part.
	if got := h.Between(at("2026-10-09 17:00"), at("2026-10-12 10:00")); got != 2*time.Hour {
		t.Errorf("working time across a weekend = %s, want 2h", got)
	}
	if got := h.Between(at("2026-10-05 17:00"), at("2026-10-07 09:30")); got != 90*time.Minute {
		t.Errorf("working time across a holiday = %s, want 1h30m", got)
	}
	// Round the clock: plain minutes.
	all := Hours{}
	if got := all.Add(at("2026-10-10 23:30"), 60); !got.Equal(at("2026-10-11 00:30")) {
		t.Errorf("24x7 clock = %s", got)
	}
	if got := all.Between(at("2026-10-10 23:30"), at("2026-10-11 00:30")); got != time.Hour {
		t.Errorf("24x7 wait = %s", got)
	}
}
