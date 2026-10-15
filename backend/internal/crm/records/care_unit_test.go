package records

import (
	"testing"
	"time"
)

func TestAddIntervalUsesCalendarMonths(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	cases := []struct {
		from string
		n    int
		unit string
		want string
	}{
		{"2026-01-31", 1, "months", "2026-02-28"}, {"2028-01-31", 1, "months", "2028-02-29"}, {"2026-03-31", 3, "months", "2026-06-30"},
		{"2028-02-29", 1, "years", "2029-02-28"}, {"2026-10-15", 2, "weeks", "2026-10-29"}, {"2026-12-30", 3, "days", "2027-01-02"},
	}
	for _, c := range cases {
		if got := addInterval(d(c.from), c.n, c.unit).Format("2006-01-02"); got != c.want {
			t.Errorf("%s + %d %s = %s, want %s", c.from, c.n, c.unit, got, c.want)
		}
	}
}
