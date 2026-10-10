package bikepaths

import "testing"

func TestKind(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
	}{
		{map[string]string{"highway": "cycleway"}, KindCycleway},
		// A cycleway is one whatever its bicycle tag says.
		{map[string]string{"highway": "cycleway", "bicycle": "designated"}, KindCycleway},
		{map[string]string{"highway": "path", "bicycle": "designated"}, KindShared},
		{map[string]string{"highway": "footway", "bicycle": "designated"}, KindShared},
		{map[string]string{"highway": "bridleway", "bicycle": "designated"}, KindShared},
		// Bikes allowed, not designated: a trail like any other.
		{map[string]string{"highway": "path", "bicycle": "yes"}, ""},
		{map[string]string{"highway": "path"}, ""},
		// A road with a designated bike lane is a road.
		{map[string]string{"highway": "residential", "bicycle": "designated"}, ""},
		{map[string]string{"highway": "construction", "construction": "cycleway"}, ""},
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := Kind(c.tags); got != c.want {
			t.Errorf("Kind(%v) = %q, want %q", c.tags, got, c.want)
		}
	}
}

func TestParseFeature(t *testing.T) {
	cases := []struct {
		name         string
		line         string
		ok           bool
		id           int64
		kind, wName  string
		wantParseErr bool
	}{
		{
			name: "a cycleway, with osmium's record separator",
			line: "\x1e" + `{"type":"Feature","id":"w42","geometry":{"type":"LineString","coordinates":[[1,2],[1.1,2]]},"properties":{"highway":"cycleway","name":" Towpath Trail "}}`,
			ok:   true, id: 42, kind: KindCycleway, wName: "Towpath Trail",
		},
		{
			name: "a shared path",
			line: `{"type":"Feature","id":"w7","geometry":{"type":"LineString","coordinates":[[1,2],[1.1,2]]},"properties":{"highway":"path","bicycle":"designated"}}`,
			ok:   true, id: 7, kind: KindShared,
		},
		{
			name: "neither kind",
			line: `{"type":"Feature","id":"w1","geometry":{"type":"LineString","coordinates":[[1,2],[1.1,2]]},"properties":{"highway":"footway"}}`,
		},
		{
			name: "a node, not a way",
			line: `{"type":"Feature","id":"n1","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"highway":"cycleway"}}`,
		},
		{
			name: "no id",
			line: `{"type":"Feature","geometry":{"type":"LineString","coordinates":[[1,2],[1.1,2]]},"properties":{"highway":"cycleway"}}`,
		},
		{
			name: "no geometry",
			line: `{"type":"Feature","id":"w1","geometry":null,"properties":{"highway":"cycleway"}}`,
		},
		{name: "not JSON", line: `<osm>`, wantParseErr: true},
	}
	for _, c := range cases {
		w, ok, err := parseFeature([]byte(c.line))
		if (err != nil) != c.wantParseErr {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (w.osmID != c.id || w.kind != c.kind || w.name != c.wName) {
			t.Errorf("%s: got %+v", c.name, w)
		}
	}
}
