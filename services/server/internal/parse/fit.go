package parse

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"
)

// ParseFIT is a minimal, honest subset of the FIT binary format: enough to read `record`
// (global message 20) and `session` (global message 18, for activity_type — see
// fit_sport.go) fields out of files produced by mainstream encoders (little-endian
// architecture, no compressed-timestamp headers). It correctly walks every other message
// type it doesn't understand (reading each field's declared byte width so the stream stays
// aligned) rather than only handling those two and desyncing on anything else. It does not
// implement developer-field *values* (only skips their declared bytes) and does not verify
// CRCs. This is the parser most likely to need the most depth of the three formats — it
// covers the common case, not the full FIT SDK surface.
func ParseFIT(r io.Reader) (Activity, error) {
	br := &byteReader{r: r}

	hdr := make([]byte, 12)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return Activity{}, fmt.Errorf("parse fit: header: %w", err)
	}
	headerSize := int(hdr[0])
	if string(hdr[8:12]) != ".FIT" {
		return Activity{}, fmt.Errorf("parse fit: missing .FIT signature")
	}
	dataSize := binary.LittleEndian.Uint32(hdr[4:8])
	if headerSize > 12 {
		extra := make([]byte, headerSize-12)
		if _, err := io.ReadFull(br, extra); err != nil {
			return Activity{}, fmt.Errorf("parse fit: header CRC bytes: %w", err)
		}
	}

	defs := map[byte]*fitDef{}
	act := Activity{ActivityType: "unknown"}
	var read uint32

	const fitEpoch = 631065600 // seconds between Unix epoch and 1989-12-31T00:00:00Z

	for read < dataSize {
		recHdr, err := br.ReadByte()
		if err != nil {
			return Activity{}, fmt.Errorf("parse fit: record header: %w", err)
		}
		read++

		if recHdr&0x80 != 0 {
			return Activity{}, fmt.Errorf("parse fit: compressed timestamp headers are not supported")
		}
		localType := recHdr & 0x0F
		isDefinition := recHdr&0x40 != 0

		if isDefinition {
			hasDevFields := recHdr&0x20 != 0
			def, n, err := readDefinition(br, hasDevFields)
			if err != nil {
				return Activity{}, fmt.Errorf("parse fit: definition: %w", err)
			}
			read += n
			defs[localType] = def
			continue
		}

		def, ok := defs[localType]
		if !ok {
			return Activity{}, fmt.Errorf("parse fit: data message for undefined local type %d", localType)
		}

		fields := make(map[byte][]byte, len(def.fields))
		for _, f := range def.fields {
			buf := make([]byte, f.size)
			if _, err := io.ReadFull(br, buf); err != nil {
				return Activity{}, fmt.Errorf("parse fit: field data: %w", err)
			}
			read += uint32(f.size)
			fields[f.num] = buf
		}

		switch def.globalMesgNum {
		case 18: // session — carries the activity's own sport/sub_sport (fields 5, 6).
			// A multi-sport file can have more than one session message; last one read
			// wins, a deliberate simplification rather than an oversight — there's no
			// single obviously-correct "the whole file's type" when there's more than
			// one, and this parser covers the common single-sport case (see doc comment).
			var sport, subSport byte = 0xFF, 0xFF
			if raw := fields[5]; len(raw) > 0 {
				sport = raw[0]
			}
			if raw := fields[6]; len(raw) > 0 {
				subSport = raw[0]
			}
			act.ActivityType = sportName(sport, subSport)

		case 20: // record — the point stream
			p := Point{}
			haveLat, haveLon := false, false
			for _, f := range def.fields {
				raw := fields[f.num]
				switch f.num {
				case 0: // position_lat, sint32 semicircles
					v := int32(def.order.Uint32(raw))
					p.Lat = float64(v) * (180.0 / math.Pow(2, 31))
					haveLat = true
				case 1: // position_long
					v := int32(def.order.Uint32(raw))
					p.Lon = float64(v) * (180.0 / math.Pow(2, 31))
					haveLon = true
				case 2: // altitude, uint16, scale 5, offset 500
					v := def.order.Uint16(raw)
					if v != 0xFFFF {
						e := float32(v)/5 - 500
						p.Elevation = &e
					}
				case 3: // heart_rate, uint8
					if len(raw) > 0 && raw[0] != 0xFF {
						hr := int16(raw[0])
						p.HeartRate = &hr
					}
				case 4: // cadence, uint8
					if len(raw) > 0 && raw[0] != 0xFF {
						c := int16(raw[0])
						p.Cadence = &c
					}
				case 7: // power, uint16
					v := def.order.Uint16(raw)
					if v != 0xFFFF {
						pw := int16(v)
						p.PowerW = &pw
					}
				case 253: // timestamp, uint32 seconds since FIT epoch
					v := def.order.Uint32(raw)
					p.Time = time.Unix(int64(v)+fitEpoch, 0).UTC()
				}
			}
			if haveLat && haveLon {
				act.Points = append(act.Points, p)
			}

		default:
			// Every other message type is just skipped — its bytes were already read
			// above to keep the stream aligned, but nothing here needs its fields.
		}
	}

	if len(act.Points) == 0 {
		return Activity{}, fmt.Errorf("parse fit: no record messages with position found")
	}
	return act, nil
}

type fitField struct {
	num  byte
	size byte
}

type fitDef struct {
	globalMesgNum uint16
	order         binary.ByteOrder
	fields        []fitField
}

// readDefinition reads a definition message: reserved, architecture, global_mesg_num,
// num_fields, fields..., and — when the record header's bit 5 (devFields) said so — a
// trailing developer-field count and that many (num, size, dev_data_index) triples. Those
// bytes are read and skipped, not interpreted: values from developer fields on `record`
// aren't extracted, but the byte stream stays correctly aligned for every message that
// follows, which is the part that actually matters for not silently corrupting the rest of
// the parse. Returns bytes consumed after the header byte.
func readDefinition(br *byteReader, devFields bool) (*fitDef, uint32, error) {
	// reserved(1) + architecture(1) + global_mesg_num(2) = 4 bytes, not 5.
	buf := make([]byte, 4)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, 0, err
	}
	var n uint32 = 4
	arch := buf[1]
	order := binary.ByteOrder(binary.LittleEndian)
	if arch == 1 {
		order = binary.BigEndian
	}
	globalMesgNum := order.Uint16(buf[2:4])

	numFieldsB, err := br.ReadByte()
	if err != nil {
		return nil, n, err
	}
	n++
	def := &fitDef{globalMesgNum: globalMesgNum, order: order}
	for i := byte(0); i < numFieldsB; i++ {
		fb := make([]byte, 3)
		if _, err := io.ReadFull(br, fb); err != nil {
			return nil, n, err
		}
		n += 3
		def.fields = append(def.fields, fitField{num: fb[0], size: fb[1]})
	}

	if devFields {
		numDevB, err := br.ReadByte()
		if err != nil {
			return nil, n, err
		}
		n++
		for i := byte(0); i < numDevB; i++ {
			db := make([]byte, 3) // field_num, size, developer_data_index — skipped, not stored
			if _, err := io.ReadFull(br, db); err != nil {
				return nil, n, err
			}
			n += 3
			// The data-message bytes these fields occupy still need to be skipped when a
			// matching data message arrives, so they're recorded like any other field —
			// just never read out of `fields` by number in the `record` switch above.
			def.fields = append(def.fields, fitField{num: 0xFF, size: db[1]})
		}
	}
	return def, n, nil
}

// byteReader adapts an io.Reader to io.ByteReader without pulling in bufio's larger surface,
// and without ever buffering more than one byte ahead — consistent with §5.2's "stream,
// don't buffer the whole file" requirement.
type byteReader struct {
	r   io.Reader
	one [1]byte
}

func (b *byteReader) Read(p []byte) (int, error) { return b.r.Read(p) }

func (b *byteReader) ReadByte() (byte, error) {
	_, err := io.ReadFull(b.r, b.one[:])
	return b.one[0], err
}
