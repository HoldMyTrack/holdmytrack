package httpapi

import (
	"context"
	"strings"

	"github.com/fitmap/fitmap/services/server/internal/ingest"
)

// demoPreset is one fixed, hand-written activity a fresh demo account is seeded with —
// docs/ROADMAP.md's "Email verification + demo without real ingest": demo accounts no longer
// get real Health Connect/upload access at all (see requireNotDemo), so without this a demo
// session would open on a genuinely empty map. The full curated, geo-selected preset library
// that item also describes is explicitly out of scope for now — this is the smallest thing
// that makes "view-only preset activities" real: two fixed GPX tracks, ingested through the
// exact same pipeline (internal/ingest.Process) a real upload uses, not a hand-rolled shortcut
// that would need its own fog/heatmap rendering path.
type demoPreset struct {
	externalID string
	filename   string
	gpx        string
}

var demoPresets = []demoPreset{
	{
		externalID: "demo-preset-park-walk",
		filename:   "demo-park-walk.gpx",
		gpx: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="FitMap">
<trk><name>Park walk</name><type>walk</type><trkseg>
<trkpt lat="40.78210" lon="-73.96550"><ele>15</ele><time>2026-06-01T09:00:00Z</time></trkpt>
<trkpt lat="40.78240" lon="-73.96500"><ele>15</ele><time>2026-06-01T09:02:00Z</time></trkpt>
<trkpt lat="40.78290" lon="-73.96470"><ele>16</ele><time>2026-06-01T09:04:00Z</time></trkpt>
<trkpt lat="40.78330" lon="-73.96520"><ele>16</ele><time>2026-06-01T09:06:00Z</time></trkpt>
<trkpt lat="40.78300" lon="-73.96580"><ele>15</ele><time>2026-06-01T09:08:00Z</time></trkpt>
<trkpt lat="40.78250" lon="-73.96600"><ele>15</ele><time>2026-06-01T09:10:00Z</time></trkpt>
<trkpt lat="40.78210" lon="-73.96550"><ele>15</ele><time>2026-06-01T09:12:00Z</time></trkpt>
</trkseg></trk>
</gpx>`,
	},
	{
		externalID: "demo-preset-coastal-run",
		filename:   "demo-coastal-run.gpx",
		gpx: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="FitMap">
<trk><name>Coastal run</name><type>run</type><trkseg>
<trkpt lat="37.76940" lon="-122.48620"><ele>25</ele><time>2026-06-02T07:00:00Z</time></trkpt>
<trkpt lat="37.77120" lon="-122.48720"><ele>22</ele><time>2026-06-02T07:03:00Z</time></trkpt>
<trkpt lat="37.77300" lon="-122.48830"><ele>18</ele><time>2026-06-02T07:06:00Z</time></trkpt>
<trkpt lat="37.77480" lon="-122.48950"><ele>16</ele><time>2026-06-02T07:09:00Z</time></trkpt>
<trkpt lat="37.77660" lon="-122.49060"><ele>14</ele><time>2026-06-02T07:12:00Z</time></trkpt>
<trkpt lat="37.77840" lon="-122.49180"><ele>12</ele><time>2026-06-02T07:15:00Z</time></trkpt>
</trkseg></trk>
</gpx>`,
	},
}

// seedDemoPresets ingests every demoPreset into a freshly created demo account, through the
// real pipeline (parse, persist, fog/heatmap mask render) rather than a hand-rolled DB insert
// that would need to reimplement it. Best-effort: a seeding failure is logged, not surfaced to
// the caller — handleDemoStart already succeeded in creating the account and session by the
// time this runs, and failing the whole demo-start over a seed hiccup would be a worse outcome
// than a demo account that opens on an empty map.
func (s *Server) seedDemoPresets(ctx context.Context, userID string) {
	for _, preset := range demoPresets {
		rawKey := "raw/" + userID + "/demo-preset/" + preset.externalID + ".gpx"
		if err := s.store.Put(ctx, rawKey, strings.NewReader(preset.gpx), int64(len(preset.gpx))); err != nil {
			s.log.Error("demo preset upload failed", "err", err, "preset", preset.externalID)
			continue
		}
		job := ingest.Job{
			UserID:        userID,
			Source:        "demo-preset",
			SourceDetail:  preset.filename,
			ExternalID:    preset.externalID,
			RawPayloadKey: rawKey,
			PrivacyTrimM:  0, // fixed, hand-written coordinates — no real location to protect.
		}
		if _, err := ingest.Process(ctx, s.pool, s.store, job); err != nil {
			s.log.Error("demo preset ingest failed", "err", err, "preset", preset.externalID)
		}
	}
}
