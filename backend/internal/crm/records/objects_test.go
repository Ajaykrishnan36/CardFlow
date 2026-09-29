package records

import "testing"

func TestPluralizeAndKeys(t *testing.T) {
	for in, want := range map[string]string{"Project": "Projects", "Property": "Properties", "Class": "Classes", "Day": "Days", "Box": "Boxes"} {
		if got := pluralize(in); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Close date": "closeDate", "Budget": "budget", "2nd contact": "f2ndContact", "Owner's phone": "ownerSPhone"} {
		if got := camelKey(in); got != want {
			t.Errorf("camelKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStandardObjectsBuildValidSpecs(t *testing.T) {
	prefixes := map[string]bool{}
	for _, d := range standardObjects() {
		d := d
		if !objectKeyRe.MatchString(d.Key) || reservedKeys[d.Key] {
			t.Fatalf("%s: invalid key", d.Key)
		}
		if prefixes[d.Prefix] || reservedPrefixes[d.Prefix] || !prefixRe.MatchString(d.Prefix) {
			t.Fatalf("%s: prefix %q clashes or is invalid", d.Key, d.Prefix)
		}
		prefixes[d.Prefix] = true
		s := buildSpec(&d)
		if s.Table != "obj_"+d.Key || s.TitleSQL != "t.name" {
			t.Fatalf("%s: unexpected table/title", d.Key)
		}
		seen := map[string]bool{}
		for _, f := range s.Fields {
			if seen[f.Key] {
				t.Fatalf("%s: field %q twice", d.Key, f.Key)
			}
			seen[f.Key] = true
			// Only name/status and system fields have columns; the rest live in custom jsonb.
			if f.inColumn() != (f.column != "") {
				t.Fatalf("%s.%s: storage mismatch", d.Key, f.Key)
			}
		}
		placed := map[string]bool{}
		for _, sec := range s.Layout.Sections {
			for _, k := range sec.Fields {
				if !seen[k] || placed[k] {
					t.Fatalf("%s: layout places unknown or duplicate field %q", d.Key, k)
				}
				placed[k] = true
			}
		}
	}
}
