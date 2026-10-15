package fog

import (
	"encoding/binary"
	"math"
)

// EncodeHeatPoints encodes a heat-points tile as a Mapbox Vector Tile (v2): one layer, "heat",
// at a 4096 extent, one point feature per HeatPoint with its weight as "w". Written here rather
// than asked of PostGIS's ST_AsMVT, which took a database round trip per tile only to pack
// points already in tile space — nothing to transform or clip. Weights are stored as floats,
// each distinct one once, as the format shares values between features. No points, no bytes:
// an empty tile.
func EncodeHeatPoints(points []HeatPoint) []byte {
	if len(points) == 0 {
		return nil
	}
	layer := make([]byte, 0, 16*len(points)+64)
	layer = appendVarintField(layer, 15, 2) // version
	layer = appendBytesField(layer, 1, []byte("heat"))
	values := map[float32]int{}
	var valueList []float32
	var feature, packed []byte
	for _, p := range points {
		w := float32(p.W)
		vi, ok := values[w]
		if !ok {
			vi = len(valueList)
			values[w] = vi
			valueList = append(valueList, w)
		}
		feature = feature[:0]
		packed = binary.AppendUvarint(packed[:0], 0)      // tags: key 0, "w" ...
		packed = binary.AppendUvarint(packed, uint64(vi)) // ... = value vi
		feature = appendBytesField(feature, 2, packed)
		feature = appendVarintField(feature, 3, 1) // type: POINT
		// MoveTo, one point, from the feature's cursor at (0, 0).
		packed = binary.AppendUvarint(packed[:0], 1|1<<3)
		packed = binary.AppendUvarint(packed, zigzag(p.X))
		packed = binary.AppendUvarint(packed, zigzag(p.Y))
		feature = appendBytesField(feature, 4, packed)
		layer = appendBytesField(layer, 2, feature)
	}
	layer = appendBytesField(layer, 3, []byte("w"))
	for _, v := range valueList {
		var value []byte
		value = protowireKey(value, 2, 5) // float, fixed32
		value = binary.LittleEndian.AppendUint32(value, math.Float32bits(v))
		layer = appendBytesField(layer, 4, value)
	}
	layer = appendVarintField(layer, 5, 4096) // extent
	return appendBytesField(nil, 3, layer)
}

func zigzag(v int) uint64 { return uint64((v << 1) ^ (v >> 63)) }

// protowireKey appends a field's key: its number and wire type.
func protowireKey(b []byte, field, wireType int) []byte {
	return binary.AppendUvarint(b, uint64(field<<3|wireType))
}

func appendVarintField(b []byte, field int, v uint64) []byte {
	return binary.AppendUvarint(protowireKey(b, field, 0), v)
}

func appendBytesField(b []byte, field int, v []byte) []byte {
	b = binary.AppendUvarint(protowireKey(b, field, 2), uint64(len(v)))
	return append(b, v...)
}
