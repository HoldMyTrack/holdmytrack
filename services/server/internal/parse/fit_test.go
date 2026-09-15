package parse

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildMinimalFIT constructs one `record` (global mesg 20) definition + one data message
// with position_lat, position_long, timestamp — the fields ParseFIT actually extracts.
func buildMinimalFIT(t *testing.T, latSemi, lonSemi int32, ts uint32) []byte {
	t.Helper()
	var data bytes.Buffer

	// Definition message: local type 0, architecture 0 (LE), global mesg 20 (`record`),
	// 3 fields: lat(0,4), lon(1,4), timestamp(253,4). Base-type bytes are unused by
	// ParseFIT (only size matters), so any placeholder value works.
	data.WriteByte(0x40) // definition, local type 0
	data.Write([]byte{0x00, 0x00})
	binary.Write(&data, binary.LittleEndian, uint16(20)) // global_mesg_num = record
	data.WriteByte(3)                                    // num_fields
	data.Write([]byte{0x00, 4, 0x85})                    // position_lat
	data.Write([]byte{0x01, 4, 0x85})                    // position_long
	data.Write([]byte{0xFD, 4, 0x86})                    // timestamp (253)

	// Data message: local type 0.
	data.WriteByte(0x00)
	binary.Write(&data, binary.LittleEndian, latSemi)
	binary.Write(&data, binary.LittleEndian, lonSemi)
	binary.Write(&data, binary.LittleEndian, ts)

	body := data.Bytes()

	var hdr bytes.Buffer
	hdr.WriteByte(12)                                    // header size
	hdr.WriteByte(0x10)                                  // protocol version
	binary.Write(&hdr, binary.LittleEndian, uint16(100)) // profile version
	binary.Write(&hdr, binary.LittleEndian, uint32(len(body)))
	hdr.WriteString(".FIT")

	return append(hdr.Bytes(), body...)
}

func TestParseFIT(t *testing.T) {
	const latSemi, lonSemi int32 = 476741369, -993858314 // arbitrary but distinct sint32s
	const ts uint32 = 1000000000

	raw := buildMinimalFIT(t, latSemi, lonSemi, ts)
	act, err := ParseFIT(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseFIT: %v", err)
	}
	if len(act.Points) != 1 {
		t.Fatalf("want 1 point, got %d", len(act.Points))
	}
	p := act.Points[0]

	wantLat := float64(latSemi) * (180.0 / 2147483648.0)
	wantLon := float64(lonSemi) * (180.0 / 2147483648.0)
	if abs(p.Lat-wantLat) > 1e-6 {
		t.Errorf("lat = %v, want %v", p.Lat, wantLat)
	}
	if abs(p.Lon-wantLon) > 1e-6 {
		t.Errorf("lon = %v, want %v", p.Lon, wantLon)
	}

	wantUnix := int64(ts) + 631065600
	if p.Time.Unix() != wantUnix {
		t.Errorf("time unix = %d, want %d", p.Time.Unix(), wantUnix)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// buildFITWithSession adds a `session` (global mesg 18) definition + data message, carrying
// sport and sub_sport, ahead of the same record message buildMinimalFIT produces. Real
// encoders write the session summary *after* the records it summarizes, but ParseFIT
// doesn't care about order — it just overwrites ActivityType whenever it sees one — so
// putting it first here is a simpler test, not a claim about real file layout.
func buildFITWithSession(t *testing.T, sport, subSport byte, latSemi, lonSemi int32, ts uint32) []byte {
	t.Helper()
	var data bytes.Buffer

	// session: local type 1, global mesg 18, fields sport(5,1 byte), sub_sport(6,1 byte).
	data.WriteByte(0x41) // definition, local type 1
	data.Write([]byte{0x00, 0x00})
	binary.Write(&data, binary.LittleEndian, uint16(18)) // global_mesg_num = session
	data.WriteByte(2)                                    // num_fields
	data.Write([]byte{0x05, 1, 0x00})                    // sport
	data.Write([]byte{0x06, 1, 0x00})                    // sub_sport
	data.WriteByte(0x01)                                 // data, local type 1
	data.WriteByte(sport)
	data.WriteByte(subSport)

	// record: local type 0, same shape as buildMinimalFIT.
	data.WriteByte(0x40)
	data.Write([]byte{0x00, 0x00})
	binary.Write(&data, binary.LittleEndian, uint16(20))
	data.WriteByte(3)
	data.Write([]byte{0x00, 4, 0x85})
	data.Write([]byte{0x01, 4, 0x85})
	data.Write([]byte{0xFD, 4, 0x86})
	data.WriteByte(0x00)
	binary.Write(&data, binary.LittleEndian, latSemi)
	binary.Write(&data, binary.LittleEndian, lonSemi)
	binary.Write(&data, binary.LittleEndian, ts)

	body := data.Bytes()

	var hdr bytes.Buffer
	hdr.WriteByte(12)
	hdr.WriteByte(0x10)
	binary.Write(&hdr, binary.LittleEndian, uint16(100))
	binary.Write(&hdr, binary.LittleEndian, uint32(len(body)))
	hdr.WriteString(".FIT")

	return append(hdr.Bytes(), body...)
}

func TestParseFITSport(t *testing.T) {
	// sport=2 (cycling), sub_sport=46 (gravel_cycling) — the exact case that motivated
	// this: sub_sport has to win over sport, or "gravel" is indistinguishable from "ride".
	raw := buildFITWithSession(t, 2, 46, 476741369, -993858314, 1000000000)
	act, err := ParseFIT(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseFIT: %v", err)
	}
	if act.ActivityType != "gravel_cycling" {
		t.Fatalf("want activity type gravel_cycling, got %q", act.ActivityType)
	}
	if len(act.Points) != 1 {
		t.Fatalf("want 1 point, got %d", len(act.Points))
	}
}

func TestParseFITSportGenericSubSportFallsBackToSport(t *testing.T) {
	// sport=1 (running), sub_sport=0 (generic) — generic isn't a real distinction, so this
	// must resolve to "running", not "generic".
	raw := buildFITWithSession(t, 1, 0, 476741369, -993858314, 1000000000)
	act, err := ParseFIT(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseFIT: %v", err)
	}
	if act.ActivityType != "running" {
		t.Fatalf("want activity type running (generic sub_sport falls back to sport), got %q", act.ActivityType)
	}
}

func TestParseFITNoSession(t *testing.T) {
	// No session message at all (buildMinimalFIT's fixture) — must stay "unknown", the
	// same default GPX and TCX use when their own source data doesn't say either.
	raw := buildMinimalFIT(t, 476741369, -993858314, 1000000000)
	act, err := ParseFIT(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseFIT: %v", err)
	}
	if act.ActivityType != "unknown" {
		t.Fatalf("want activity type unknown (no session message in this fixture), got %q", act.ActivityType)
	}
}
