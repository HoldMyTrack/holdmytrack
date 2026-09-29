package spots

import "testing"

func TestCategory(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
	}{
		{map[string]string{"leisure": "playground"}, "playground"},
		{map[string]string{"leisure": "dog_park"}, "dog_park"},
		{map[string]string{"historic": "monument"}, "monument"},
		{map[string]string{"historic": "memorial"}, "monument"},
		{map[string]string{"tourism": "viewpoint"}, "viewpoint"},
		{map[string]string{"historic": "castle"}, "history"},
		{map[string]string{"historic": "ruins"}, "history"},
		{map[string]string{"historic": "fort"}, "history"},
		{map[string]string{"historic": "archaeological_site"}, "history"},
		// A castle ruin with a view is the rarer, more specific kind of place.
		{map[string]string{"historic": "ruins", "tourism": "viewpoint"}, "history"},
		{map[string]string{"historic": "boundary_stone"}, ""},
		{map[string]string{"leisure": "park"}, ""},
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := Category(c.tags); got != c.want {
			t.Errorf("Category(%v) = %q, want %q", c.tags, got, c.want)
		}
	}
}

func TestAddress(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
	}{
		{map[string]string{"addr:housenumber": "12", "addr:street": "Main Street", "addr:city": "Springfield", "addr:postcode": "12345"}, "12 Main Street, Springfield 12345"},
		{map[string]string{"addr:street": "Main Street", "addr:city": "Springfield"}, "Main Street, Springfield"},
		{map[string]string{"addr:housenumber": "3", "addr:place": "Old Town"}, "3 Old Town"},
		{map[string]string{"addr:full": "1 Castle Hill, Edinburgh", "addr:street": "ignored"}, "1 Castle Hill, Edinburgh"},
		// A city alone isn't an address a navigator could find.
		{map[string]string{"addr:city": "Springfield", "addr:postcode": "12345"}, ""},
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := Address(c.tags); got != c.want {
			t.Errorf("Address(%v) = %q, want %q", c.tags, got, c.want)
		}
	}
}

func TestParseFeature(t *testing.T) {
	cases := []struct {
		name, line   string
		ok           bool
		typ          string
		id           int64
		category     string
		placeName    string
		wantParseErr bool
	}{
		{
			name: "a node, with osmium's record separator",
			line: "\x1e" + `{"type":"Feature","id":"n42","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"tourism":"viewpoint","name":"Lookout"}}`,
			ok:   true, typ: "node", id: 42, category: "viewpoint", placeName: "Lookout",
		},
		{
			name: "a way exported as itself",
			line: `{"type":"Feature","id":"w7","geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]},"properties":{"leisure":"playground"}}`,
			ok:   true, typ: "way", id: 7, category: "playground",
		},
		{
			// osmium's area ids: a way's id times two, a relation's times two plus one.
			name: "an area built from a way",
			line: `{"type":"Feature","id":"a14","geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]},"properties":{"leisure":"playground"}}`,
			ok:   true, typ: "way", id: 7, category: "playground",
		},
		{
			name: "an area built from a relation",
			line: `{"type":"Feature","id":"a19","geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]},"properties":{"historic":"ruins"}}`,
			ok:   true, typ: "relation", id: 9, category: "history",
		},
		{
			name: "the id as attributes instead",
			line: `{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"@type":"relation","@id":9,"historic":"fort"}}`,
			ok:   true, typ: "relation", id: 9, category: "history",
		},
		{
			name: "none of the categories",
			line: `{"type":"Feature","id":"n1","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"amenity":"bench"}}`,
		},
		{
			name: "no id",
			line: `{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"leisure":"playground"}}`,
		},
		{
			name: "no geometry",
			line: `{"type":"Feature","id":"n1","geometry":null,"properties":{"leisure":"playground"}}`,
		},
		{name: "not JSON", line: `<osm>`, wantParseErr: true},
	}
	for _, c := range cases {
		p, ok, err := parseFeature([]byte(c.line))
		if (err != nil) != c.wantParseErr {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (p.osmType != c.typ || p.osmID != c.id || p.category != c.category || p.name != c.placeName) {
			t.Errorf("%s: got %+v", c.name, p)
		}
	}
}

func TestPlaceText(t *testing.T) {
	got := PlaceText(map[string]string{
		"description": " A bronze statue. ", "inscription": "To the fallen", "memorial": "war_memorial",
		"start_date": "1919", "wikipedia": "en:Soldiers' and Sailors' Monument (Cleveland)",
	})
	want := Text{"A bronze statue.", "To the fallen", "war_memorial", "1919", "en:Soldiers' and Sailors' Monument (Cleveland)"}
	if got != want {
		t.Errorf("PlaceText = %+v, want %+v", got, want)
	}
	for _, w := range []string{"Soldiers' Monument", "https://en.wikipedia.org/wiki/X", "en:", ""} {
		if got := PlaceText(map[string]string{"wikipedia": w}).Wikipedia; got != "" {
			t.Errorf("wikipedia %q kept as %q; only lang:Title is", w, got)
		}
	}
}
