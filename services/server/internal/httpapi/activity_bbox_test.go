package httpapi

import (
	"context"
	"math"
	"slices"
	"testing"
)

// An activity's bbox goes the shorter way round in longitude: one across the antimeridian is a
// few tenths of a degree with east past 180, not the whole globe (IMPLEMENTATION.md §4.7).
func TestActivityBBoxAcrossTheAntimeridian(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	ctx := context.Background()
	track := func(wkt string) string {
		id := d.newActivity(me, testActivity{activityType: "sailing", at: &[2]float64{0, 0}})
		if _, err := d.pool.Exec(ctx, `UPDATE activities SET trajectory = ST_GeomFromText($2, 4326) WHERE id = $1`, id, wkt); err != nil {
			t.Fatal(err)
		}
		return id
	}
	crossing := track("LINESTRING M(179.9 -17 0, -179.9 -16.9 60)")
	greenwich := track("LINESTRING M(-1 51 0, 1 51.5 60)")
	pacific := track("LINESTRING M(170 -17 0, 175 -16 60)")
	want := map[string][]float64{
		crossing:  {179.9, -17, 180.1, -16.9},
		greenwich: {-1, 51, 1, 51.5},
		pacific:   {170, -17, 175, -16},
	}
	for _, a := range d.listActivities(me) {
		got := make([]float64, len(a.BBox))
		for i, v := range a.BBox {
			got[i] = math.Round(v*1000) / 1000 // float noise from the shift
		}
		if !slices.Equal(got, want[a.ID]) {
			t.Errorf("activity %s: bbox %v, want %v", a.ID, a.BBox, want[a.ID])
		}
	}
}
