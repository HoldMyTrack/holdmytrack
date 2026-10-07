package httpapi

import (
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// spotsAreaLimit caps how many places one "Show in this area" request returns (§4.25). A
// region-sized view can hold tens of thousands of playgrounds; 2,000 badges is already a dense
// map, and the response says how many there were, so the client can ask the user to zoom in.
const spotsAreaLimit = 2000

var spotCategories = []string{"playground", "dog_park", "monument", "viewpoint", "history"}

// spotsAreaQuery is the places in an area, in the chosen categories, at most $6 of them,
// leaving out a retired place (ADR-0027) unless the account $7 captured it. The area is one or
// two boxes (spotArea, $1–$4): two when the view crosses the antimeridian. Named places first — the ones worth a popup — then by a hash of the id: a stable order that's
// spread evenly over the area, so a cut-off leaves a thinner map rather than an empty corner.
var spotsAreaQuery = `
SELECT s.id, s.category, s.name, s.address, s.description, s.inscription, s.memorial,
       s.start_date, s.wikipedia, ST_X(p.pt), ST_Y(p.pt)
FROM spots s
CROSS JOIN LATERAL (SELECT ST_PointOnSurface(s.geom) AS pt) p
WHERE ` + spotsInAreaSQL + ` AND s.category = ANY($5)
  AND (s.retired_at IS NULL OR EXISTS (SELECT 1 FROM spot_captures c WHERE c.spot_id = s.id AND c.user_id = $7))
ORDER BY s.name IS NULL, md5(s.id::text)
LIMIT $6`

var spotsAreaCountQuery = `
SELECT count(*) FROM spots s
CROSS JOIN LATERAL (SELECT ST_PointOnSurface(s.geom) AS pt) p
WHERE ` + spotsInAreaSQL + ` AND s.category = ANY($5)
  AND (s.retired_at IS NULL OR EXISTS (SELECT 1 FROM spot_captures c WHERE c.spot_id = s.id AND c.user_id = $6))`

// spotsInAreaSQL is "s's point p.pt lies in one of the area's boxes": $1 south, $2 north, and
// the boxes' west and east edges as two float8 arrays, $3 and $4. Each box is its own && test,
// so both stay index lookups — one envelope around both would span the whole world.
const spotsInAreaSQL = `EXISTS (
    SELECT 1 FROM unnest($3::float8[], $4::float8[]) AS e(w, x)
    WHERE s.geom && ST_MakeEnvelope(e.w, $1, e.x, $2, 4326)
      AND ST_Intersects(p.pt, ST_MakeEnvelope(e.w, $1, e.x, $2, 4326)))`

// spotJSON is one place as "Show in this area" returns it — the tiles' `spots` layer's
// properties (§4.25), with a field absent when OSM has none.
type spotJSON struct {
	ID          int64   `json:"id"`
	Category    string  `json:"category"`
	Name        *string `json:"name,omitempty"`
	Address     *string `json:"address,omitempty"`
	Description *string `json:"description,omitempty"`
	Inscription *string `json:"inscription,omitempty"`
	Memorial    *string `json:"memorial,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	Wikipedia   *string `json:"wikipedia,omitempty"`
	Lon         float64 `json:"lon"`
	Lat         float64 `json:"lat"`
}

type spotsAreaResponse struct {
	Spots []spotJSON `json:"spots"`
	// Total is how many places the area holds in those categories; more than len(Spots) when
	// the response was cut at spotsAreaLimit.
	Total int `json:"total"`
}

// handleSpotsInArea serves `GET /v1/spots?bbox=west,south,east,north&categories=a,b` — "Show in
// this area" below the zoom the spots tiles start at (FR-15.5). The places are the same for
// everyone but the retired ones, which only an account that captured one gets back; it's behind
// the session like the tiles.
func (s *Server) handleSpotsInArea(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	area, ok := parseBBox(q.Get("bbox"))
	if !ok {
		http.Error(w, "bbox must be west,south,east,north in degrees, south < north", http.StatusBadRequest)
		return
	}
	var categories []string
	for c := range strings.SplitSeq(q.Get("categories"), ",") {
		if !slices.Contains(spotCategories, c) {
			http.Error(w, "categories must be one or more of "+strings.Join(spotCategories, ","), http.StatusBadRequest)
			return
		}
		categories = append(categories, c)
	}

	resp := spotsAreaResponse{Spots: []spotJSON{}}
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	if err := s.pool.QueryRow(ctx, spotsAreaCountQuery, area.south, area.north, area.west, area.east, categories, userID).Scan(&resp.Total); err != nil {
		s.log.Error("spots area count failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows, err := s.pool.Query(ctx, spotsAreaQuery, area.south, area.north, area.west, area.east, categories, spotsAreaLimit, userID)
	if err != nil {
		s.log.Error("spots area query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sp spotJSON
		if err := rows.Scan(&sp.ID, &sp.Category, &sp.Name, &sp.Address, &sp.Description, &sp.Inscription,
			&sp.Memorial, &sp.StartDate, &sp.Wikipedia, &sp.Lon, &sp.Lat); err != nil {
			s.log.Error("spots area scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		resp.Spots = append(resp.Spots, sp)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("spots area rows failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// spotArea is a view as boxes within −180…180: one, or two when the view crosses the
// antimeridian, west[i] to east[i] each, all between south and north.
type spotArea struct {
	south, north float64
	west, east   []float64
}

// parseBBox reads "west,south,east,north" into the boxes it covers. A view across the
// antimeridian comes either way: unwrapped, east past 180 (170,…,190,…, as MapLibre's web
// bounds give it), or wrapped, west > east (170,…,−170,…); both are 170…180 and −180…−170. A
// view 360° wide or more is the whole world.
func parseBBox(raw string) (spotArea, bool) {
	var box [4]float64
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return spotArea{}, false
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return spotArea{}, false
		}
		box[i] = v
	}
	west, east := box[0], box[2]
	area := spotArea{south: math.Max(box[1], -90), north: math.Min(box[3], 90)}
	if area.south >= area.north || west == east {
		return spotArea{}, false
	}
	if west > east {
		east += 360
	}
	if east-west >= 360 {
		area.west, area.east = []float64{-180}, []float64{180}
		return area, true
	}
	// Bring west into −180…180; east then lies within 360° of it.
	shift := 360 * math.Floor((west+180)/360)
	west, east = west-shift, east-shift
	if east <= 180 {
		area.west, area.east = []float64{west}, []float64{east}
	} else {
		area.west, area.east = []float64{west, -180}, []float64{180, east - 360}
	}
	return area, true
}
