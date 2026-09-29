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

// spotsAreaQuery is the places in a bounding box, in the chosen categories, at most $6 of them.
// Named places first — the ones worth a popup — then by a hash of the id: a stable order that's
// spread evenly over the area, so a cut-off leaves a thinner map rather than an empty corner.
const spotsAreaQuery = `
WITH box AS (SELECT ST_MakeEnvelope($1, $2, $3, $4, 4326) AS b)
SELECT s.id, s.category, s.name, s.address, s.description, s.inscription, s.memorial,
       s.start_date, s.wikipedia, ST_X(p.pt), ST_Y(p.pt)
FROM box, spots s
CROSS JOIN LATERAL (SELECT ST_PointOnSurface(s.geom) AS pt) p
WHERE s.geom && box.b AND s.category = ANY($5) AND ST_Intersects(p.pt, box.b)
ORDER BY s.name IS NULL, md5(s.id::text)
LIMIT $6`

const spotsAreaCountQuery = `
SELECT count(*) FROM spots s
WHERE s.geom && ST_MakeEnvelope($1, $2, $3, $4, 4326) AND s.category = ANY($5)
  AND ST_Intersects(ST_PointOnSurface(s.geom), ST_MakeEnvelope($1, $2, $3, $4, 4326))`

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
// everyone; it's behind the session like the tiles.
func (s *Server) handleSpotsInArea(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	box, ok := parseBBox(q.Get("bbox"))
	if !ok {
		http.Error(w, "bbox must be west,south,east,north in degrees, west < east, south < north", http.StatusBadRequest)
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
	if err := s.pool.QueryRow(ctx, spotsAreaCountQuery, box[0], box[1], box[2], box[3], categories).Scan(&resp.Total); err != nil {
		s.log.Error("spots area count failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows, err := s.pool.Query(ctx, spotsAreaQuery, box[0], box[1], box[2], box[3], categories, spotsAreaLimit)
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

// parseBBox reads "west,south,east,north". A view across the antimeridian (west > east) isn't
// taken: the client splits nothing, and it's open ocean for every place this serves today.
func parseBBox(raw string) ([4]float64, bool) {
	var box [4]float64
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return box, false
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return box, false
		}
		box[i] = v
	}
	box[0], box[2] = math.Max(box[0], -180), math.Min(box[2], 180)
	box[1], box[3] = math.Max(box[1], -90), math.Min(box[3], 90)
	return box, box[0] < box[2] && box[1] < box[3]
}
