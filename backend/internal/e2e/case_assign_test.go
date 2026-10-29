package e2e

import "testing"

// Giving a new case to someone moves it to Assigned and records when (D-166), with or
// without an SLA; a case already being worked keeps its status when it changes hands.
func TestAssigningACaseMarksItAssigned(t *testing.T) {
	boss := signIn(t, freshPhone(), "Desk Boss")
	ws := newBusiness(t, boss, "Assign Desk")
	_, meena := staffOf(t, boss, ws, "Meena Service")
	_, ravi := staffOf(t, boss, ws, "Ravi Service")

	c := create(t, boss, ws, "cases", map[string]any{"name": "Loose stitching"})
	if v := get(t, boss, ws, "cases", c); v["status"] != "new" || v["assignedAt"] != nil {
		t.Fatalf("a new case starts New and unassigned: %v / %v", v["status"], v["assignedAt"])
	}
	want(t, edit(t, boss, ws, "cases", c, map[string]any{"ownerId": meena}), 200, "assign to Meena")
	v := get(t, boss, ws, "cases", c)
	if v["status"] != "assigned" || v["assignedAt"] == nil || v["ownerId"] != meena {
		t.Fatalf("an assigned case is Assigned with the time: %v / %v / %v", v["status"], v["assignedAt"], v["ownerId"])
	}
	at := v["assignedAt"]
	want(t, edit(t, boss, ws, "cases", c, map[string]any{"status": "working"}), 200, "work on it")
	want(t, edit(t, boss, ws, "cases", c, map[string]any{"ownerId": ravi}), 200, "hand it to Ravi")
	if v := get(t, boss, ws, "cases", c); v["status"] != "working" || v["assignedAt"] != at {
		t.Fatalf("handing over a case in work keeps its status and first assignment: %v / %v", v["status"], v["assignedAt"])
	}
}
