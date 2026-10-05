package records

import "testing"

// Money is whole hundredths: nothing is ever added up as a float (D-118, D-119).
func TestCentsArithmetic(t *testing.T) {
	for in, want := range map[string]Cents{"0": 0, "1": 100, "1.5": 150, "1234.56": 123456, "0.005": 1, "0.004": 0, "-2.345": -235, "333.33": 33333, ".5": 50} {
		if got, ok := parseCents(in); !ok || got != want {
			t.Errorf("parseCents(%q) = %d %v, want %d", in, got, ok, want)
		}
	}
	for _, bad := range []any{"", "abc", "1e9", nil} {
		if _, ok := parseCents(bad); ok {
			t.Errorf("parseCents(%v) should fail", bad)
		}
	}
	if got, _ := parseCents(0.1 + 0.2); got != 30 { // the classic float trap
		t.Errorf("0.1 + 0.2 = %d cents, want 30", got)
	}
	// 3 × 333.33 = 999.99; 7.5% of that = 74.99925 → 75.00.
	line := Cents(33333).timesQty(3000)
	if line != 99999 || line.percentOf(750) != 7500 || line-line.percentOf(750) != 92499 {
		t.Errorf("3 × 333.33 less 7.5%% = %s − %s", line, line.percentOf(750))
	}
	// Splitting 100,001 in two and giving the remainder to the last keeps the total exact.
	whole := Cents(10000100)
	half := whole.mulRatio(1, 2)
	if half+(whole-half) != whole || half != 5000050 {
		t.Errorf("half of %s = %s", whole, half)
	}
	if Cents(-150).String() != "-1.50" || Cents(5).String() != "0.05" || Cents(123456).String() != "1234.56" {
		t.Error("formatting")
	}
	if Cents(1).mulRatio(1, 3) != 0 || Cents(2).mulRatio(1, 3) != 1 || Cents(-2).mulRatio(1, 3) != -1 {
		t.Error("rounding half away from zero")
	}
}
