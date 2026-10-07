package fog

import (
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// A track continuing past 180 (ingest's unwrapLons) draws into the tiles on both sides of the
// antimeridian: the first column of the world as much as the last.
func TestMaskAcrossTheAntimeridian(t *testing.T) {
	points := []parse.Point{
		{Lon: 179.999, Lat: -16.82, Time: time.Unix(0, 0)},
		{Lon: 180.001, Lat: -16.82, Time: time.Unix(60, 0)},
	}
	east, y := tilemath.LonLatToTile(179.999, -16.82, Zoom)
	for _, x := range []int{east, 0} {
		mask := renderActivityMask(projectToTile(points, x, y, Zoom)...)
		lit := 0
		for _, px := range mask.Pix {
			if px > 0 {
				lit++
			}
		}
		if lit == 0 {
			t.Errorf("tile %d/%d: nothing drawn", x, y)
		}
	}
}
