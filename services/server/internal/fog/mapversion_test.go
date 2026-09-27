package fog

import "testing"

func TestTileVersionOwnedBy(t *testing.T) {
	const alice = "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const bob = "22222222-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	cv := TileVersion(alice, 7)
	if cv != "11111111.7" {
		t.Fatalf("TileVersion = %q, want 11111111.7", cv)
	}
	for _, tc := range []struct {
		cv, user string
		want     bool
	}{
		{cv, alice, true},
		{cv, bob, false},
		{"", alice, false},
		{"11111111.", alice, false},
		{"11111111", alice, false},
		{"7", alice, false},
	} {
		if got := TileVersionOwnedBy(tc.cv, tc.user); got != tc.want {
			t.Errorf("TileVersionOwnedBy(%q, %s) = %v, want %v", tc.cv, tc.user[:8], got, tc.want)
		}
	}
}
