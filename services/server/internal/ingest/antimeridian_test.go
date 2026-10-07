package ingest

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

func lons(points []parse.Point) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = math.Round(p.Lon*1000) / 1000
	}
	return out
}

func track(lonlats ...float64) []parse.Point {
	var out []parse.Point
	for i := 0; i < len(lonlats); i += 2 {
		out = append(out, parse.Point{Lon: lonlats[i], Lat: lonlats[i+1], Time: time.Unix(int64(i*30), 0)})
	}
	return out
}

// A track across the antimeridian continues past ±180 rather than jumping round the world,
// either way across; one that doesn't cross is left alone, and doing it twice changes nothing.
func TestUnwrapLons(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []parse.Point
		want []float64
	}{
		{"eastward", track(179.9, -16.8, 179.95, -16.81, -179.98, -16.82, -179.92, -16.83), []float64{179.9, 179.95, 180.02, 180.08}},
		{"westward", track(-179.9, 0, 179.9, 0, 179.8, 0), []float64{-179.9, -180.1, -180.2}},
		{"there and back", track(179.9, 0, -179.9, 0, 179.95, 0), []float64{179.9, 180.1, 179.95}},
		{"nowhere near", track(-81.7, 41.4, -81.6, 41.5), []float64{-81.7, -81.6}},
		{"a first point past 180", track(181, 0, 182, 0), []float64{-179, -178}},
	} {
		got := unwrapLons(c.in)
		if !slices.Equal(lons(got), c.want) {
			t.Errorf("%s: %v, want %v", c.name, lons(got), c.want)
		}
		if !slices.Equal(lons(unwrapLons(got)), c.want) {
			t.Errorf("%s: unwrapping twice changed it", c.name)
		}
	}
}

// A track across the antimeridian touches the tiles either side of it, a handful, not a tile
// in every column of the world.
func TestTouchedTilesAcrossTheAntimeridian(t *testing.T) {
	tiles, err := computeTouchedTiles(unwrapLons(track(179.95, -16.81, -179.98, -16.82)), FogZoom)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiles) == 0 || len(tiles) > 20 {
		t.Fatalf("%d tiles, want a handful", len(tiles))
	}
	last := 1<<FogZoom - 1
	var east, west bool
	for _, tile := range tiles {
		if tile[0] < 0 || tile[0] > last {
			t.Errorf("tile %v is off the world", tile)
		}
		east = east || tile[0] == last
		west = west || tile[0] == 0
	}
	if !east || !west {
		t.Errorf("tiles %v, want the last column and the first", tiles)
	}
}
