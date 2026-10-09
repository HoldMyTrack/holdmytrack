// cmd/loadtest drives the load-test sessions docs/PERFORMANCE.md logs: it creates test accounts,
// uploads activities, browses the map the way the web client does and reports latency, and
// deletes the accounts again. Run it from services/server against a deployment you may load:
//
//	go run ./cmd/loadtest -base https://example.com -dir /tmp/lt signup 21
//	go run ./cmd/loadtest -base https://example.com -dir /tmp/lt seed
//	go run ./cmd/loadtest -base https://example.com -dir /tmp/lt browse 25 3m
//	go run ./cmd/loadtest -base https://example.com -dir /tmp/lt cleanup
//
// Subcommands:
//
//	signup N        create N accounts (the deployment must skip email verification meanwhile,
//	                docs/PERFORMANCE.md "Test accounts"); their sessions go to -dir/tokens.json
//	seed            upload every demo track (internal/httpapi/demo_data) to every account
//	gen K N         write K sets of N tracks each to -dir/set-1..K, demo tracks moved apart and
//	                back in time so every file is new; -long keeps the three tracks of 200+ z14
//	                tiles
//	import K        upload set-1..K, one per account, all accounts at once, each set's files one
//	                at a time as the web's Upload menu sends them
//	browse VUS DUR  VUS people at the map for DUR, each in Normal, Fog or Heatmap mode, panning
//	                and zooming and requesting the tiles the web client would; each keeps a
//	                browser-like tile cache unless -cold. Latency per tile layer every 30 s
//	probe           upload one new track from the last account and time it until it's processed
//	cleanup         delete every account in tokens.json
//
// Nothing it writes goes in the repo: tokens and track sets live in -dir.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	base     = flag.String("base", "", "the deployment's origin, e.g. https://example.com (required)")
	dir      = flag.String("dir", "", "working directory for tokens.json and track sets (required)")
	demoDir  = flag.String("demo", "internal/httpapi/demo_data", "the demo tracks, relative to services/server")
	emailFmt = flag.String("email", "loadtest+%02d@holdmytrack.com", "test account address, numbered from 1")
	long     = flag.Bool("long", false, "gen: keep the long demo tracks (200+ z14 tiles each)")
	client   = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 512}}
	upload   = &http.Client{Timeout: 30 * time.Minute}
)

type acct struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

func inDir(name string) string { return filepath.Join(*dir, name) }

func loadAccts() []acct {
	b, err := os.ReadFile(inDir("tokens.json"))
	must(err)
	var a []acct
	must(json.Unmarshal(b, &a))
	return a
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 || *dir == "" || (*base == "" && args[0] != "gen") {
		fmt.Fprintln(os.Stderr, "usage: loadtest -base URL -dir DIR signup N | seed | gen K N | import K | browse VUS DUR | probe | cleanup")
		os.Exit(2)
	}
	must(os.MkdirAll(*dir, 0o700))
	os.Args = append([]string{os.Args[0]}, args...)
	switch os.Args[1] {
	case "signup":
		n, _ := strconv.Atoi(os.Args[2])
		signup(n)
	case "seed":
		seed()
	case "gen":
		k, _ := strconv.Atoi(os.Args[2])
		n, _ := strconv.Atoi(os.Args[3])
		gen(k, n)
	case "browse":
		v, _ := strconv.Atoi(os.Args[2])
		d, _ := time.ParseDuration(os.Args[3])
		browse(v, d)
	case "import":
		k, _ := strconv.Atoi(os.Args[2])
		importSets(k)
	case "probe":
		probe()
	case "cleanup":
		cleanup()
	}
}

// ---- accounts

func signup(n int) {
	var out []acct
	secret := make([]byte, 12)
	_, err := rand.Read(secret)
	must(err)
	password := "lt-" + hex.EncodeToString(secret) // one per run, kept in tokens.json
	for i := 1; i <= n; i++ {
		email := fmt.Sprintf(*emailFmt, i)
		body, _ := json.Marshal(map[string]string{"email": email, "password": password, "timezone": "UTC"})
		resp, err := client.Post(*base+"/v1/auth/signup", "application/json", bytes.NewReader(body))
		must(err)
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var r struct {
			SessionToken  string `json:"session_token"`
			EmailVerified bool   `json:"email_verified"`
		}
		json.Unmarshal(rb, &r)
		fmt.Printf("%s %d verified=%v %s\n", email, resp.StatusCode, r.EmailVerified, trunc(string(rb), 200))
		if r.SessionToken != "" {
			out = append(out, acct{email, password, r.SessionToken})
		}
	}
	b, _ := json.MarshalIndent(out, "", " ")
	must(os.WriteFile(inDir("tokens.json"), b, 0o600))
}

func cleanup() {
	for _, a := range loadAccts() {
		body, _ := json.Marshal(map[string]string{"email": a.Email})
		req, _ := http.NewRequest("DELETE", *base+"/v1/account", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+a.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println(a.Email, err)
			continue
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println(a.Email, resp.StatusCode, trunc(string(rb), 200))
	}
}

// ---- uploads

func uploadFile(c *http.Client, tok, name string, data []byte) (int, []byte, time.Duration, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", *base+"/v1/activities/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	t := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, time.Since(t), err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, rb, time.Since(t), nil
}

func demoFiles() []string {
	f, _ := filepath.Glob(filepath.Join(*demoDir, "*.gpx"))
	sort.Strings(f)
	return f
}

func seed() {
	accts := loadAccts()
	files := demoFiles()
	st := newStats()
	var wg sync.WaitGroup
	t0 := time.Now()
	for _, a := range accts {
		wg.Add(1)
		go func(a acct) {
			defer wg.Done()
			for _, f := range files {
				data, _ := os.ReadFile(f)
				code, rb, d, err := uploadFile(upload, a.Token, filepath.Base(f), data)
				st.add("upload", code, d, err)
				if code != 202 && code != 200 {
					fmt.Println(a.Email, filepath.Base(f), code, err, trunc(string(rb), 200))
				}
			}
		}(a)
	}
	wg.Wait()
	fmt.Printf("seed uploads done in %s\n", time.Since(t0).Round(time.Second))
	st.print()
}

var latRe = regexp.MustCompile(`lat="(-?[0-9.]+)"`)
var lonRe = regexp.MustCompile(`lon="(-?[0-9.]+)"`)
var timeRe = regexp.MustCompile(`<time>([^<]+)</time>`)

// shift moves a GPX by (dlat, dlon) degrees and by days, so every copy has a distinct hash,
// a distinct place on the fog map and a distinct date.
func shift(gpx []byte, dlat, dlon float64, days int) []byte {
	sh := func(re *regexp.Regexp, key string, d float64) func([]byte) []byte {
		return func(m []byte) []byte {
			v, _ := strconv.ParseFloat(string(re.FindSubmatch(m)[1]), 64)
			return []byte(fmt.Sprintf(`%s="%.6f"`, key, v+d))
		}
	}
	gpx = latRe.ReplaceAllFunc(gpx, sh(latRe, "lat", dlat))
	gpx = lonRe.ReplaceAllFunc(gpx, sh(lonRe, "lon", dlon))
	gpx = timeRe.ReplaceAllFunc(gpx, func(m []byte) []byte {
		t, err := time.Parse(time.RFC3339, string(timeRe.FindSubmatch(m)[1]))
		if err != nil {
			return m
		}
		return []byte("<time>" + t.AddDate(0, 0, -days).Format(time.RFC3339) + "</time>")
	})
	return gpx
}

func gen(k, n int) {
	var files []string
	for _, f := range demoFiles() {
		// The three demo tracks of 200+ z14 tiles each (the 1,478 km "2026-01-22 Vietnam" drive and
		// two Niagara trips) are left out by default: an import made of copies of them measures
		// those tracks, not an import.
		if *long || !strings.Contains(f, "01-22 Vietnam") && !strings.Contains(f, "07-13 Niagara") && !strings.Contains(f, "07-17 Niagara") {
			files = append(files, f)
		}
	}
	src := make([][]byte, len(files))
	for i, f := range files {
		src[i], _ = os.ReadFile(f)
	}
	for a := 1; a <= k; a++ {
		set := inDir(fmt.Sprintf("set-%d", a))
		must(os.MkdirAll(set, 0o755))
		var size int
		for i := 0; i < n; i++ {
			// a spiral of offsets, ~0.05° (5 km) apart, so copies spread outward around each origin
			ang := float64(i) * 2.399963
			r := 0.05 * math.Sqrt(float64(i+1)) * float64(a)
			g := shift(src[i%len(src)], r*math.Sin(ang), r*math.Cos(ang), i+a*1000)
			must(os.WriteFile(filepath.Join(set, fmt.Sprintf("a%d-track-%04d.gpx", a, i)), g, 0o644))
			size += len(g)
		}
		fmt.Printf("set-%d %d files %.1f MB\n", a, n, float64(size)/1e6)
	}
}

func importSets(k int) {
	accts := loadAccts()
	var wg sync.WaitGroup
	for a := 1; a <= k; a++ {
		wg.Add(1)
		go func(a int, ac acct) {
			defer wg.Done()
			files, err := filepath.Glob(filepath.Join(inDir(fmt.Sprintf("set-%d", a)), "*.gpx"))
			must(err)
			sort.Strings(files)
			t0 := time.Now()
			failed := 0
			for _, f := range files {
				code, rb, _, err := uploadFile(upload, ac.Token, filepath.Base(f), mustRead(f))
				if err != nil || code >= 300 {
					failed++
					fmt.Printf("%s set-%d %s: %d err=%v %s\n", time.Now().Format("15:04:05"), a, filepath.Base(f), code, err, trunc(string(rb), 200))
				}
			}
			fmt.Printf("%s set-%d: %d files sent in %s, %d failed\n", time.Now().Format("15:04:05"), a, len(files), time.Since(t0).Round(time.Millisecond), failed)
		}(a, accts[a-1])
	}
	wg.Wait()
}

func probe() {
	accts := loadAccts()
	a := accts[len(accts)-1]
	files := demoFiles()
	g := shift(mustRead(files[mrand.Intn(len(files))]), mrand.Float64()*0.2, mrand.Float64()*0.2, mrand.Intn(3000)+5000)
	code, rb, d, err := uploadFile(client, a.Token, "probe.gpx", g)
	fmt.Printf("probe upload %d in %s err=%v %s\n", code, d.Round(time.Millisecond), err, trunc(string(rb), 200))
	var r struct {
		ExternalID string `json:"external_id"`
	}
	json.Unmarshal(rb, &r)
	if r.ExternalID == "" {
		return
	}
	t0 := time.Now()
	for {
		req, _ := http.NewRequest("GET", *base+"/v1/activities/status/"+r.ExternalID, nil)
		req.Header.Set("Authorization", "Bearer "+a.Token)
		resp, err := client.Do(req)
		if err == nil {
			sb, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var s struct{ Status string }
			json.Unmarshal(sb, &s)
			if s.Status == "done" || s.Status == "failed" {
				fmt.Printf("probe %s after %s\n", s.Status, time.Since(t0).Round(time.Second))
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func mustRead(f string) []byte {
	b, err := os.ReadFile(f)
	must(err)
	return b
}

// ---- browse

type pt struct{ lat, lon float64 }

func centres() []pt {
	var out []pt
	for _, f := range demoFiles() {
		b := mustRead(f)
		la := latRe.FindSubmatch(b)
		lo := lonRe.FindSubmatch(b)
		if la == nil || lo == nil {
			continue
		}
		x, _ := strconv.ParseFloat(string(la[1]), 64)
		y, _ := strconv.ParseFloat(string(lo[1]), 64)
		out = append(out, pt{x, y})
	}
	return out
}

func tileXY(lat, lon float64, z int) (int, int) {
	n := math.Exp2(float64(z))
	x := int((lon + 180) / 360 * n)
	lr := lat * math.Pi / 180
	y := int((1 - math.Log(math.Tan(lr)+1/math.Cos(lr))/math.Pi) / 2 * n)
	return x, y
}

// The web client's tiers (apps/web/src/map/zoomTiers.ts and the layers' own zoom ranges): Fog
// and Heatmap draw whole countries below z3 and whole regions below z7, their rasters from z7;
// Normal draws tracks from z4. Every tile source stops at z14 and is stretched past it.
const (
	countryMaxZoom = 3
	regionMaxZoom  = 7
	tracksMinZoom  = 4
	spotsMinZoom   = 13
	sourceMaxZoom  = 14
	viewW, viewH   = 1280, 800 // a laptop browser window, CSS pixels; the clients draw tiles 512 px
)

// layers returns the tile layers the web client requests at whole zoom z in mode, each as
// "name:ext", and the zoom it requests them at.
func layers(mode string, spots bool, z int) ([]string, int) {
	tz := min(z, sourceMaxZoom)
	var out []string
	switch mode {
	case "normal":
		if z >= tracksMinZoom {
			out = append(out, "tracks:mvt")
		}
	default: // fog, heatmap
		switch {
		case z < countryMaxZoom:
			out = append(out, "country-"+mode+":mvt")
		case z < regionMaxZoom:
			out = append(out, "region-"+mode+":mvt")
		default:
			out = append(out, mode+":png")
		}
	}
	if spots && z >= spotsMinZoom {
		out = append(out, "spots:mvt")
	}
	return out, tz
}

// visitor is one simulated person at the map: one account, one mode at a time, and — unless
// -cold — a tile cache like a browser's, which keeps every tile it has fetched for good (they
// carry the account's tile version and are served immutable, IMPLEMENTATION.md §4.2.6).
type visitor struct {
	r     *mrand.Rand
	tok   string
	mode  string
	spots bool
	c     pt
	z     int
	cache map[string]bool
}

func pickMode(r *mrand.Rand) string {
	switch n := r.Intn(100); {
	case n < 50:
		return "normal"
	case n < 85:
		return "fog"
	default:
		return "heatmap"
	}
}

// step moves the visitor the way people use the map: mostly panning about half a screen,
// sometimes zooming in or out a level, now and then pulling right out to see a country and
// coming back, and rarely switching mode.
func (v *visitor) step() {
	switch n := v.r.Intn(100); {
	case n < 55:
		// Half a screen in a random direction, in degrees at this zoom.
		span := 360 / math.Exp2(float64(v.z)) * (float64(viewW) / 512)
		v.c.lon += (v.r.Float64() - 0.5) * span
		v.c.lat += (v.r.Float64() - 0.5) * span * float64(viewH) / float64(viewW) * math.Cos(v.c.lat*math.Pi/180)
		// The map stops at Web Mercator's edge and wraps east-west, as the web client's does; a
		// centre past a pole sends view's tile rows to infinity.
		v.c.lat = math.Max(-85, math.Min(85, v.c.lat))
		v.c.lon = math.Mod(math.Mod(v.c.lon+180, 360)+360, 360) - 180
	case n < 80:
		if v.r.Intn(2) == 0 {
			v.z = max(v.z-1, 2)
		} else {
			v.z = min(v.z+1, 17)
		}
	case n < 92:
		v.z = 2 + v.r.Intn(4) // out to a country or region
	case n < 97:
		v.z = 11 + v.r.Intn(4) // back into a city
	default:
		v.mode = pickMode(v.r)
	}
}

// -cold drops the tile cache: every view fetches all its tiles again, the worst case the
// 2026-10-08 and 2026-10-09 runs measured.
var cold = flag.Bool("cold", false, "browse: no tile cache, every view fetches every tile")

func browse(vus int, dur time.Duration) {
	accts := loadAccts()
	if len(accts) > 20 {
		accts = accts[:20] // the last account is the probe's
	}
	cs := centres()
	st := newStats()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	var wg sync.WaitGroup
	var views, hits atomic.Int64
	for i := 0; i < vus; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := mrand.New(mrand.NewSource(int64(i) + time.Now().UnixNano()))
			// Opening the map frames the account's latest activity, at city zoom (SPEC.md FR-4.5).
			v := &visitor{r: r, tok: accts[i%len(accts)].Token, mode: pickMode(r), spots: r.Intn(5) == 0,
				c: cs[r.Intn(len(cs))], z: 12 + r.Intn(3), cache: map[string]bool{}}
			time.Sleep(time.Duration(r.Intn(3000)) * time.Millisecond)
			for ctx.Err() == nil {
				hits.Add(int64(view(ctx, st, v)))
				views.Add(1)
				time.Sleep(time.Duration(1500+r.Intn(2500)) * time.Millisecond)
				v.step()
			}
		}(i)
	}
	tick := time.NewTicker(30 * time.Second)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	t0 := time.Now()
loop:
	for {
		select {
		case <-tick.C:
			fmt.Printf("--- %s  vus=%d views=%d cached tiles=%d\n", time.Since(t0).Round(time.Second), vus, views.Load(), hits.Load())
			st.printInterval()
		case <-done:
			break loop
		}
	}
	secs := time.Since(t0).Seconds()
	reqs := st.total()
	fmt.Printf("=== FINAL vus=%d dur=%s cold=%v views=%d (%.1f views/s) requests=%d (%.1f/s, %.1f a view) cached tiles=%d\n",
		vus, dur, *cold, views.Load(), float64(views.Load())/secs, reqs, float64(reqs)/secs, float64(reqs)/math.Max(1, float64(views.Load())), hits.Load())
	st.print()
}

// view requests the tiles the visitor's window shows at its zoom, in its mode, six at a time
// like a browser, skipping those its cache already holds. It returns how many it skipped.
func view(ctx context.Context, st *stats, v *visitor) int {
	ls, tz := layers(v.mode, v.spots, v.z)
	// The window in tiles of zoom tz: viewW/512 tiles across at zoom z, twice as many per zoom
	// level the tiles are deeper than that.
	scale := math.Exp2(float64(tz - v.z))
	hw, hh := float64(viewW)/512/2*scale, float64(viewH)/512/2*scale
	n := math.Exp2(float64(tz))
	fx := (v.c.lon + 180) / 360 * n
	lr := v.c.lat * math.Pi / 180
	fy := (1 - math.Log(math.Tan(lr)+1/math.Cos(lr))/math.Pi) / 2 * n
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	skipped := 0
	for _, l := range ls {
		parts := strings.SplitN(l, ":", 2)
		for x := int(math.Floor(fx - hw)); x <= int(math.Floor(fx+hw)); x++ {
			for y := int(math.Floor(fy - hh)); y <= int(math.Floor(fy+hh)); y++ {
				if y < 0 || y >= int(n) {
					continue
				}
				wx := ((x % int(n)) + int(n)) % int(n)
				key := fmt.Sprintf("%s/%d/%d/%d", parts[0], tz, wx, y)
				if !*cold {
					if v.cache[key] {
						skipped++
						continue
					}
					v.cache[key] = true
				}
				url := fmt.Sprintf("%s/tiles/v1/%s.%s", *base, key, parts[1])
				wg.Add(1)
				sem <- struct{}{}
				go func(name, url string) {
					defer wg.Done()
					defer func() { <-sem }()
					req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
					req.Header.Set("Authorization", "Bearer "+v.tok)
					t := time.Now()
					resp, err := client.Do(req)
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
					if ctx.Err() != nil {
						return
					}
					code := 0
					if resp != nil {
						code = resp.StatusCode
					}
					st.add(name, code, time.Since(t), err)
				}(parts[0], url)
			}
		}
	}
	wg.Wait()
	return skipped
}

// ---- stats

type stats struct {
	mu     sync.Mutex
	lat    map[string][]time.Duration
	codes  map[string]map[int]int
	errs   map[string]int
	ilat   map[string][]time.Duration
	icodes map[string]map[int]int
}

func newStats() *stats {
	return &stats{lat: map[string][]time.Duration{}, codes: map[string]map[int]int{}, errs: map[string]int{}, ilat: map[string][]time.Duration{}, icodes: map[string]map[int]int{}}
}

func (s *stats) add(name string, code int, d time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codes[name] == nil {
		s.codes[name] = map[int]int{}
	}
	if s.icodes[name] == nil {
		s.icodes[name] = map[int]int{}
	}
	if err != nil {
		s.errs[name]++
		s.codes[name][0]++
		s.icodes[name][0]++
		return
	}
	s.lat[name] = append(s.lat[name], d)
	s.ilat[name] = append(s.ilat[name], d)
	s.codes[name][code]++
	s.icodes[name][code]++
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[int(math.Min(float64(len(d)-1), p*float64(len(d))))]
}

func dump(lat map[string][]time.Duration, codes map[string]map[int]int) {
	names := make([]string, 0, len(codes))
	for n := range codes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := lat[n]
		fmt.Printf("  %-14s n=%-6d p50=%-8s p95=%-8s p99=%-8s max=%-8s codes=%v\n", n, len(d),
			pct(d, .5).Round(time.Millisecond), pct(d, .95).Round(time.Millisecond), pct(d, .99).Round(time.Millisecond), pct(d, 1).Round(time.Millisecond), codes[n])
	}
}

func (s *stats) printInterval() {
	s.mu.Lock()
	defer s.mu.Unlock()
	dump(s.ilat, s.icodes)
	s.ilat = map[string][]time.Duration{}
	s.icodes = map[string]map[int]int{}
}

// total is every request made, failed ones included.
func (s *stats) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, codes := range s.codes {
		for _, c := range codes {
			n += c
		}
	}
	return n
}

func (s *stats) print() {
	s.mu.Lock()
	defer s.mu.Unlock()
	dump(s.lat, s.codes)
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
