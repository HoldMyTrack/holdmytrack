// cmd/make-testing-files writes the sample files the Android test suite page offers testers
// (`/testing`, docs/SPEC.md FR-10.7, IMPLEMENTATION.md §4.14) into
// internal/web/static/testing/, from the Demo Customer's own tracks and photos
// (internal/httpapi/demo_data) and the Takeout test sample (internal/takeout/testdata).
// Run it from services/server after changing either, or this file:
//
//	go run ./cmd/make-testing-files
//
// The output is committed and embedded in the binary like the rest of static/.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

const (
	demoDir     = "internal/httpapi/demo_data"
	takeoutDir  = "internal/takeout/testdata/sample"
	photoWidth  = 1600
	jpegQuality = 82
)

// sampleZip is what Setup imports: the Demo Customer's trips abroad, each a country Fog
// clears at country level, and a few Cleveland walks for a slider that spans months.
var sampleZip = []string{
	"2026-01-20 Vietnam.gpx", "2026-01-21 Vietnam.gpx", "2026-01-22 Vietnam.gpx", "2026-01-23 Vietnam.gpx", "2026-01-24 Vietnam.gpx",
	"2026-05-07 Estonia.gpx", "2026-05-08 Estonia.gpx", "2026-05-09 Estonia.gpx",
	"2026-08-02 Italy.gpx", "2026-08-03 Italy-2.gpx", "2026-08-03 Italy-3.gpx", "2026-08-03 Italy.gpx",
	"2026-08-04 Italy.gpx", "2026-08-05 Italy-2.gpx", "2026-08-05 Italy.gpx", "2026-08-06 Italy.gpx",
	"2026-09-05 Big Creek Reservation.gpx", "2026-09-11 Lakefront Reservation.gpx",
	"2026-09-19 Radlick Park.gpx", "2026-09-27 Clague Park-2.gpx",
}

// The single-file samples, each a track not in sampleZip, so uploading one adds an activity.
const (
	westSide    = "2026-10-01 Cleveland s West Side.gpx" // west-side.gpx, and the photos' activity
	eveningLoop = "2026-10-05 Evening loop.gpx"          // evening-loop.tcx
	millCreek   = "2026-10-06 Mill Creek Falls.gpx"      // mill-creek-falls.fit
	clague      = "2026-09-27 Clague Park-2.gpx"         // duplicate.tcx: in sampleZip
	playArea    = "2026-09-07 Andrew s Nature Play Area.gpx"
	solon       = "2026-09-13 Solon Community Park.gpx"
	driving1    = "2026-10-03 driving.gpx"
	driving2    = "2026-10-03 driving-2.gpx"
	brecksDrive = "2026-09-26 Brecksville Reservation trip.gpx"
	brecksWalk  = "2026-09-26 Brecksville Reservation trip-2.gpx"
)

// outDir is where the files are written. main_test.go points it elsewhere to check that the
// committed files are what this writes.
var (
	outDir  = "internal/web/static/testing"
	verbose = true
)

func main() {
	log.SetFlags(0)
	if err := os.RemoveAll(outDir); err != nil {
		log.Fatal(err)
	}
	generate()
}

func generate() {
	must(os.MkdirAll(outDir, 0o755))

	writeZip("holdmytrack-sample.zip", func(add func(name string, data []byte)) {
		for _, name := range sampleZip {
			add(name, demoFile(name))
		}
	})
	write("west-side.gpx", demoFile(westSide))
	write("evening-loop.tcx", tcx(load(eveningLoop), "Biking", true))
	write("mill-creek-falls.fit", fit(load(millCreek)))
	// The same outing as the zip's Clague Park walk, without elevation: uploaded, it's a second
	// activity beside the GPX copy, since nothing is merged on ingest (SPEC.md FR-3.7).
	write("duplicate.tcx", tcx(load(clague), "Other", false))

	broken := demoFile(playArea)
	broken = broken[:len(broken)*2/5]
	write("broken.gpx", broken)
	write("planned-route.gpx", untimed(demoFile(solon)))
	write("indoor.gpx", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="HoldMyTrack" xmlns="http://www.topografix.com/GPX/1/1">
  <metadata><name>Treadmill</name><time>2026-09-30T11:00:00Z</time></metadata>
  <trk><type>Running</type><trkseg></trkseg></trk>
</gpx>
`))
	write("one-point.gpx", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="HoldMyTrack" xmlns="http://www.topografix.com/GPX/1/1">
  <trk><type>Walk</type><trkseg>
    <trkpt lat="41.48262" lon="-81.80119"><time>2026-09-29T17:00:00Z</time></trkpt>
  </trkseg></trk>
</gpx>
`))
	write("empty.gpx", nil)
	write("track.kml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2">
  <Placemark><name>Lakewood Park</name>
    <LineString><coordinates>-81.80119,41.49461 -81.79852,41.49512 -81.79533,41.49498</coordinates></LineString>
  </Placemark>
</kml>
`))
	writeZip("mixed.zip", func(add func(name string, data []byte)) {
		add(driving1, demoFile(driving1))
		add(driving2, demoFile(driving2))
		add("broken.gpx", broken)
		add("notes.txt", []byte("Not an activity file: the import skips it.\n"))
	})
	writeZip("takeout-sample.zip", func(add func(name string, data []byte)) {
		must(filepath.WalkDir(takeoutDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			// The fixture's health-data decoy, there for the reader's tests, isn't for testers.
			if strings.Contains(path, "Menstrual Health") {
				return nil
			}
			rel, err := filepath.Rel(takeoutDir, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			add(filepath.ToSlash(rel), data)
			return nil
		}))
	})
	writePhotos()
}

// writePhotos re-encodes three of the West Side ride's photos as JPEG, with the capture time
// the Demo Customer's manifest gives each, so the app places them on west-side.gpx by time
// alone; and a fourth with a time on no activity, which has to be placed by hand.
func writePhotos() {
	var manifest struct {
		Activities map[string]struct {
			Photos []struct {
				File    string `json:"file"`
				TakenAt string `json:"taken_at"`
			} `json:"photos"`
		} `json:"activities"`
	}
	must(json.Unmarshal(read(filepath.Join(demoDir, "manifest.json")), &manifest))
	photos := manifest.Activities[westSide].Photos
	if len(photos) < 4 {
		log.Fatalf("%s: want at least 4 photos in the manifest, have %d", westSide, len(photos))
	}
	for i, p := range photos[:3] {
		taken, err := time.Parse(time.RFC3339, p.TakenAt)
		must(err)
		write(fmt.Sprintf("photo-%d.jpg", i+1), photoJPEG(p.File, taken))
	}
	write("photo-unplaced.jpg", photoJPEG(photos[3].File, time.Date(2026, 3, 14, 16, 0, 0, 0, time.UTC)))
}

// photoJPEG is a demo photo as a phone would have saved it: a JPEG whose EXIF says when it
// was taken, in Cleveland's local time with its offset, and nothing about where.
func photoJPEG(file string, taken time.Time) []byte {
	src, err := webp.Decode(bytes.NewReader(read(filepath.Join(demoDir, "photos", file))))
	must(err)
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, photoWidth, b.Dy()*photoWidth/b.Dx()))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	must(jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}))
	cleveland, err := time.LoadLocation("America/New_York")
	must(err)
	return withExif(buf.Bytes(), taken.In(cleveland))
}

// withExif inserts an APP1 Exif segment after the JPEG's SOI marker, holding DateTimeOriginal
// and OffsetTimeOriginal (Exif 2.31) in a little-endian TIFF structure: IFD0 with only the
// pointer to the Exif IFD, and the Exif IFD with the two times.
func withExif(jpg []byte, t time.Time) []byte {
	dateTime := t.Format("2006:01:02 15:04:05") + "\x00" // 20 bytes
	offset := t.Format("-07:00") + "\x00"                // 7 bytes
	le := binary.LittleEndian
	var tiff bytes.Buffer
	put := func(v any) { must(binary.Write(&tiff, le, v)) }
	entry := func(tag, kind uint16, count, value uint32) {
		put(tag)
		put(kind)
		put(count)
		put(value)
	}
	tiff.WriteString("II")
	put(uint16(42))
	put(uint32(8)) // IFD0 offset
	// IFD0: one entry, ExifIFDPointer (0x8769, LONG).
	const ifd0Size = 2 + 12 + 4
	exifIFD := uint32(8 + ifd0Size)
	put(uint16(1))
	entry(0x8769, 4, 1, exifIFD)
	put(uint32(0))
	// Exif IFD: DateTimeOriginal (0x9003) and OffsetTimeOriginal (0x9011), both ASCII and
	// too long to sit in the entry, so each points past the IFD.
	const exifIFDSize = 2 + 2*12 + 4
	values := exifIFD + exifIFDSize
	put(uint16(2))
	entry(0x9003, 2, uint32(len(dateTime)), values)
	entry(0x9011, 2, uint32(len(offset)), values+uint32(len(dateTime)))
	put(uint32(0))
	tiff.WriteString(dateTime)
	tiff.WriteString(offset)

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write(jpg[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	must(binary.Write(&out, binary.BigEndian, uint16(len(payload)+2)))
	out.Write(payload)
	out.Write(jpg[2:])
	return out.Bytes()
}

// tcx writes a Garmin Training Center file, the way a Garmin export lays one out: one
// Activity, one Lap, one Track.
func tcx(a parse.Activity, sport string, elevation bool) []byte {
	var b strings.Builder
	start := a.Points[0].Time.UTC().Format(time.RFC3339)
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2">
  <Activities>
    <Activity Sport="%s">
      <Id>%s</Id>
      <Lap StartTime="%s">
        <TotalTimeSeconds>%.0f</TotalTimeSeconds>
        <Track>
`, sport, start, start, a.Points[len(a.Points)-1].Time.Sub(a.Points[0].Time).Seconds())
	for _, p := range a.Points {
		fmt.Fprintf(&b, "          <Trackpoint><Time>%s</Time><Position><LatitudeDegrees>%.6f</LatitudeDegrees><LongitudeDegrees>%.6f</LongitudeDegrees></Position>",
			p.Time.UTC().Format(time.RFC3339Nano), p.Lat, p.Lon)
		if elevation && p.Elevation != nil {
			fmt.Fprintf(&b, "<AltitudeMeters>%.1f</AltitudeMeters>", *p.Elevation)
		}
		b.WriteString("</Trackpoint>\n")
	}
	b.WriteString(`        </Track>
      </Lap>
    </Activity>
  </Activities>
</TrainingCenterDatabase>
`)
	return []byte(b.String())
}

// fit writes a FIT activity file holding what a watch's would: file_id, a record per point
// (position, altitude, timestamp) and a session saying the sport. Little-endian throughout;
// the header and file CRCs are FIT's CRC-16.
func fit(a parse.Activity) []byte {
	const fitEpoch = 631065600
	le := binary.LittleEndian
	var data bytes.Buffer
	put := func(v any) { must(binary.Write(&data, le, v)) }
	fitTime := func(t time.Time) uint32 { return uint32(t.Unix() - fitEpoch) }
	semicircles := func(deg float64) int32 { return int32(math.Round(deg * (math.Pow(2, 31) / 180))) }
	// define writes a definition message: local type, global message number, and each
	// field's (number, size, base type).
	define := func(local byte, global uint16, fields [][3]byte) {
		put(0x40 | local)
		put([]byte{0, 0}) // reserved, little-endian
		put(global)
		put(byte(len(fields)))
		for _, f := range fields {
			put(f[:])
		}
	}

	// file_id: type (0) = activity (4), manufacturer (1) = development (255), time_created (4).
	define(0, 0, [][3]byte{{0, 1, 0x00}, {1, 2, 0x84}, {4, 4, 0x86}})
	put(byte(0))
	put(byte(4))
	put(uint16(255))
	put(fitTime(a.Points[0].Time))

	// record: timestamp (253), position_lat (0), position_long (1), altitude (2, scale 5, offset 500).
	define(1, 20, [][3]byte{{253, 4, 0x86}, {0, 4, 0x85}, {1, 4, 0x85}, {2, 2, 0x84}})
	for _, p := range a.Points {
		put(byte(1))
		put(fitTime(p.Time))
		put(semicircles(p.Lat))
		put(semicircles(p.Lon))
		alt := uint16(0xFFFF)
		if p.Elevation != nil {
			alt = uint16(math.Round((float64(*p.Elevation) + 500) * 5))
		}
		put(alt)
	}

	// session: timestamp (253), start_time (2), sport (5) = cycling (2), sub_sport (6) = generic (0).
	define(2, 18, [][3]byte{{253, 4, 0x86}, {2, 4, 0x86}, {5, 1, 0x00}, {6, 1, 0x00}})
	put(byte(2))
	put(fitTime(a.Points[len(a.Points)-1].Time))
	put(fitTime(a.Points[0].Time))
	put(byte(2))
	put(byte(0))

	header := make([]byte, 14)
	header[0] = 14
	header[1] = 0x20 // protocol 2.0
	le.PutUint16(header[2:], 2132)
	le.PutUint32(header[4:], uint32(data.Len()))
	copy(header[8:], ".FIT")
	le.PutUint16(header[12:], fitCRC(header[:12]))

	out := append(header, data.Bytes()...)
	return le.AppendUint16(out, fitCRC(out))
}

// fitCRC is the FIT SDK's CRC-16, four bits at a time.
func fitCRC(b []byte) uint16 {
	table := [16]uint16{0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401,
		0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400}
	var crc uint16
	for _, c := range b {
		tmp := table[crc&0xF]
		crc = (crc >> 4) & 0x0FFF
		crc = crc ^ tmp ^ table[c&0xF]
		tmp = table[crc&0xF]
		crc = (crc >> 4) & 0x0FFF
		crc = crc ^ tmp ^ table[(c>>4)&0xF]
	}
	return crc
}

// untimed strips every <time> from a GPX: what a planned route, drawn rather than recorded,
// looks like.
func untimed(gpx []byte) []byte {
	var out strings.Builder
	s := string(gpx)
	for {
		i := strings.Index(s, "<time>")
		if i < 0 {
			out.WriteString(s)
			return []byte(out.String())
		}
		j := strings.Index(s[i:], "</time>")
		out.WriteString(s[:i])
		s = s[i+j+len("</time>"):]
	}
}

// length is a track's length in meters, as the crow flies between points.
func length(points []parse.Point) float64 {
	const earth = 6371008.8
	rad := math.Pi / 180
	var m float64
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		dLat, dLon := (b.Lat-a.Lat)*rad, (b.Lon-a.Lon)*rad
		h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
		m += 2 * earth * math.Asin(math.Min(1, math.Sqrt(h)))
	}
	return m
}

func demoFile(name string) []byte { return read(filepath.Join(demoDir, name)) }

func load(name string) parse.Activity {
	a, err := parse.ParseGPX(bytes.NewReader(demoFile(name)))
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	return a
}

func read(path string) []byte {
	b, err := os.ReadFile(path)
	must(err)
	return b
}

func write(name string, data []byte) {
	must(os.WriteFile(filepath.Join(outDir, name), data, 0o644))
	if verbose {
		fmt.Printf("%-24s %8d bytes\n", name, len(data))
	}
}

// writeZip writes a zip of the entries add is given, each deflated and given one fixed date,
// so rerunning the generator on unchanged input writes the same bytes.
func writeZip(name string, entries func(add func(name string, data []byte))) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries(func(entry string, data []byte) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entry, Method: zip.Deflate, Modified: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
		must(err)
		_, err = w.Write(data)
		must(err)
	})
	must(zw.Close())
	write(name, buf.Bytes())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
