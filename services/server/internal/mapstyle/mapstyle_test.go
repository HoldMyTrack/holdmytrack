package mapstyle

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// The style is generated from apps/web/src/map/style.ts, so these tests deliberately assert
// the contract this package is responsible for — that every flavor is embedded, parses, and
// has its origin substituted — not the style's content, which is style.ts's to define and
// `npm run verify:style` to police.

func TestFlavorsMatchWebClient(t *testing.T) {
	// Mirrors FLAVORS in apps/web/src/map/style.ts. If that list changes, the generator
	// writes a new file and this fails, which is the intended way to notice.
	want := []string{"black", "dark", "grayscale", "light", "white"}
	got := Flavors()
	if len(got) != len(want) {
		t.Fatalf("Flavors() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Flavors()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDocumentSubstitutesOrigin(t *testing.T) {
	const origin = "https://cdn.example.com"
	for _, flavor := range Flavors() {
		doc, err := Document(flavor, "en", origin, Satellite{})
		if err != nil {
			t.Fatalf("Document(%q): %v", flavor, err)
		}
		if strings.Contains(string(doc), originPlaceholder) {
			t.Errorf("%s: placeholder survived substitution", flavor)
		}

		var style struct {
			Version int    `json:"version"`
			Glyphs  string `json:"glyphs"`
			Sprite  string `json:"sprite"`
			Sources map[string]struct {
				URL         string `json:"url"`
				Attribution string `json:"attribution"`
			} `json:"sources"`
			Layers []json.RawMessage `json:"layers"`
		}
		if err := json.Unmarshal(doc, &style); err != nil {
			t.Fatalf("%s: not valid JSON: %v", flavor, err)
		}
		if style.Version != 8 {
			t.Errorf("%s: version = %d, want 8", flavor, style.Version)
		}
		if len(style.Layers) == 0 {
			t.Errorf("%s: no layers", flavor)
		}
		if !strings.HasPrefix(style.Glyphs, origin) {
			t.Errorf("%s: glyphs = %q, want prefix %q", flavor, style.Glyphs, origin)
		}
		if !strings.HasSuffix(style.Sprite, "/"+flavor) {
			t.Errorf("%s: sprite = %q, want it to name its own flavor", flavor, style.Sprite)
		}
		src, ok := style.Sources["protomaps"]
		if !ok {
			t.Fatalf("%s: missing the protomaps source", flavor)
		}
		if !strings.Contains(src.URL, origin) {
			t.Errorf("%s: source url = %q, want it to carry the origin", flavor, src.URL)
		}
		// ODbL "Produced Work" credit is mandatory, not decoration (style.ts's ATTRIBUTION)
		// — a client rendering this document must be able to show OSM credit.
		if !strings.Contains(src.Attribution, "OpenStreetMap") {
			t.Errorf("%s: attribution missing OSM credit: %q", flavor, src.Attribution)
		}
	}
}

func TestDocumentTrimsTrailingSlash(t *testing.T) {
	with, err := Document("light", "en", "https://cdn.example.com/", Satellite{})
	if err != nil {
		t.Fatal(err)
	}
	without, err := Document("light", "en", "https://cdn.example.com", Satellite{})
	if err != nil {
		t.Fatal(err)
	}
	// A trailing slash would otherwise produce "https://cdn.example.com//basemap/...".
	if string(with) != string(without) {
		t.Error("a trailing slash on origin changed the document")
	}
}

func TestDocumentUnknownFlavor(t *testing.T) {
	if _, err := Document("neon", "en", "https://example.com", Satellite{}); !errors.Is(err, ErrUnknownFlavor) {
		t.Errorf("err = %v, want ErrUnknownFlavor so the handler can answer 404", err)
	}
}

func TestLanguagesMatchCatalogs(t *testing.T) {
	// The generator writes one set per catalog in apps/web/src/i18n, which has the same
	// languages as the server's. A language added to one and not the other fails here.
	want := slices.Sorted(slices.Values(i18n.Supported))
	if got := Languages(); !slices.Equal(got, want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
}

func TestDocumentLabelsInLanguage(t *testing.T) {
	for _, lang := range Languages() {
		for _, flavor := range Flavors() {
			doc, err := Document(flavor, lang, "https://cdn.example.com", Satellite{})
			if err != nil {
				t.Fatalf("Document(%q, %q): %v", flavor, lang, err)
			}
			if !strings.Contains(string(doc), `"name:`+lang+`"`) {
				t.Errorf("%s/%s: labels don't read name:%s", lang, flavor, lang)
			}
		}
	}
}

func TestDocumentUnknownLanguage(t *testing.T) {
	if _, err := Document("light", "xx", "https://example.com", Satellite{}); !errors.Is(err, ErrUnknownLanguage) {
		t.Fatalf("err = %v, want ErrUnknownLanguage", err)
	}
}

func TestETagVariesWithOrigin(t *testing.T) {
	a, _ := Document("light", "en", "https://a.example.com", Satellite{})
	b, _ := Document("light", "en", "https://b.example.com", Satellite{})
	if ETag(a) == ETag(b) {
		t.Error("same ETag for different origins — a client would cache the wrong asset URLs")
	}
	if ETag(a) != ETag(a) {
		t.Error("ETag is not stable for identical input")
	}
}

// satelliteStyle decodes just what the satellite tests look at.
type satelliteStyle struct {
	Sources  map[string]map[string]any `json:"sources"`
	Metadata *struct {
		Hides       []string `json:"holdmytrack:satellite-hides"`
		Dims        []string `json:"holdmytrack:satellite-dims"`
		RoadOpacity float64  `json:"holdmytrack:satellite-road-opacity"`
	} `json:"metadata"`
	Layers []struct {
		ID     string         `json:"id"`
		Type   string         `json:"type"`
		Layout map[string]any `json:"layout"`
	} `json:"layers"`
}

func decodeSatellite(t *testing.T, doc []byte) satelliteStyle {
	t.Helper()
	var s satelliteStyle
	if err := json.Unmarshal(doc, &s); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	return s
}

func TestDocumentFillsSatellite(t *testing.T) {
	sat := Satellite{
		Tiles:       `https://tiles.example.com/sat/{z}/{x}/{y}.jpg?key=k&"quoted"`,
		TileSize:    512,
		MaxZoom:     18,
		Attribution: `<a href="https://example.com">© Imagery "Co"</a>`,
	}
	for _, flavor := range Flavors() {
		doc, err := Document(flavor, "en", "https://cdn.example.com", sat)
		if err != nil {
			t.Fatalf("Document(%q): %v", flavor, err)
		}
		for _, placeholder := range []string{satelliteTilesPlaceholder, satelliteAttributionPlaceholder, originPlaceholder} {
			if strings.Contains(string(doc), placeholder) {
				t.Errorf("%s: %s survived", flavor, placeholder)
			}
		}
		s := decodeSatellite(t, doc)
		src := s.Sources["satellite"]
		// Arbitrary text in the URL and credit comes back intact: the source is encoded, not
		// spliced in as a string.
		if tiles, _ := src["tiles"].([]any); len(tiles) != 1 || tiles[0] != sat.Tiles {
			t.Errorf("%s: tiles = %v, want [%q]", flavor, src["tiles"], sat.Tiles)
		}
		if src["type"] != "raster" || src["tileSize"] != float64(512) || src["maxzoom"] != float64(18) {
			t.Errorf("%s: source = %v", flavor, src)
		}
		if src["attribution"] != sat.Attribution {
			t.Errorf("%s: attribution = %v, want %q", flavor, src["attribution"], sat.Attribution)
		}
		// Hidden by default, right above the background; the metadata names what it hides.
		if len(s.Layers) < 2 || s.Layers[0].Type != "background" || s.Layers[1].ID != "satellite" {
			t.Fatalf("%s: satellite is not the layer right above the background", flavor)
		}
		if s.Layers[1].Layout["visibility"] != "none" {
			t.Errorf("%s: satellite starts %v, want hidden", flavor, s.Layers[1].Layout["visibility"])
		}
		if s.Metadata == nil {
			t.Fatalf("%s: no metadata", flavor)
		}
		if m := s.Metadata; !slices.Contains(m.Hides, "background") || !slices.Contains(m.Hides, "water") {
			t.Errorf("%s: satellite-hides = %v, want the background and fills", flavor, m.Hides)
		}
		if m := s.Metadata; !slices.Contains(m.Dims, "roads_major") || slices.Contains(m.Dims, "roads_rail") {
			t.Errorf("%s: satellite-dims = %v, want the roads but not rail", flavor, m.Dims)
		}
		if s.Metadata.RoadOpacity != 0.4 {
			t.Errorf("%s: satellite-road-opacity = %v, want 0.4", flavor, s.Metadata.RoadOpacity)
		}
		if _, ok := s.Sources["protomaps"]; !ok {
			t.Errorf("%s: lost the basemap source", flavor)
		}
	}
}

func TestDocumentStripsSatelliteUnconfigured(t *testing.T) {
	for _, flavor := range Flavors() {
		doc, err := Document(flavor, "en", "https://cdn.example.com", Satellite{})
		if err != nil {
			t.Fatalf("Document(%q): %v", flavor, err)
		}
		if strings.Contains(string(doc), "__HOLDMYTRACK_SATELLITE") {
			t.Errorf("%s: a satellite placeholder survived", flavor)
		}
		s := decodeSatellite(t, doc)
		if _, ok := s.Sources["satellite"]; ok {
			t.Errorf("%s: satellite source served with no imagery configured", flavor)
		}
		if s.Metadata != nil {
			t.Errorf("%s: metadata = %v, want none", flavor, s.Metadata)
		}
		for _, l := range s.Layers {
			if l.ID == "satellite" {
				t.Errorf("%s: satellite layer served with no imagery configured", flavor)
			}
		}
		// Only the imagery goes: every other layer is still there.
		with, _ := Document(flavor, "en", "https://cdn.example.com", Satellite{Tiles: "https://t/{z}/{x}/{y}", TileSize: 256, MaxZoom: 19})
		if got, want := len(s.Layers), len(decodeSatellite(t, with).Layers)-1; got != want {
			t.Errorf("%s: %d layers without imagery, want %d", flavor, got, want)
		}
	}
}

func TestETagVariesWithSatellite(t *testing.T) {
	a, _ := Document("light", "en", "https://a.example.com", Satellite{})
	b, _ := Document("light", "en", "https://a.example.com", Satellite{Tiles: "https://t/{z}/{x}/{y}", TileSize: 512, MaxZoom: 18})
	if ETag(a) == ETag(b) {
		t.Error("same ETag with and without imagery — a client would keep a style missing the switch")
	}
}
