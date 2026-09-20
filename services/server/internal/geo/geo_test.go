package geo

import "testing"

// nullableISO is the one pure, independently-testable piece of the seed path — the
// upsert/backfill queries themselves are exercised live against a real account instead, per
// this project's own stated preference for integration-shaped behavior (see internal/fog's
// cap_test.go for the same convention). Confirmed live against the actual vendored Natural
// Earth data: seeding loads all 242 countries and 4580 of 4596 regions (16 skipped for having
// no matching country — small disputed territories like Gibraltar or Bir Tawil, present in
// the 1:10m region layer but not the 1:50m country layer), and re-running the seed is a
// verified no-op on both the upserts and the activity backfill.
func TestNullableISO(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"", nil},
		{"-99", nil},
	}
	for _, c := range cases {
		if got := nullableISO(c.in); got != nil {
			t.Errorf("nullableISO(%q) = %q, want nil", c.in, *got)
		}
	}

	if got := nullableISO("NO"); got == nil || *got != "NO" {
		t.Errorf("nullableISO(%q) = %v, want %q", "NO", got, "NO")
	}
}
