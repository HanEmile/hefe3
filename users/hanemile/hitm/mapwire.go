package main

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
)

// ============================================================================
// HITM Map v2 — wire encoders (Track B)
//
// This file owns the HMAP binary column format (see MAPV2_CONTRACT.md §A), the
// JSON debugging fallback for the same columns, and the ETag/304 plumbing all
// /map/* handlers share. Nothing here parses flows or builds graphs; it only
// serializes columnar data that Track A produced.
// ============================================================================

const (
	hmapMagic   uint32 = 0x484D4150 // "HMAP"
	hmapVersion uint32 = 1
)

// HMAP flag bits (header flags field).
const (
	hmapFlagHasClusters uint32 = 1 << 0 // bit0: response carries cluster columns
)

// encodeHMAP serializes node + edge columns into the little-endian HMAP binary
// format. The caller passes already-built, parallel column slices. Node columns
// must all have length nodeN; edge columns must all have length edgeN. The gen
// argument is the low 32 bits of mapGen captured at snapshot-build time.
//
// Layout (no interior padding; the JS decoder slices each column by absolute
// byte offset):
//
//	header 32 bytes: magic, version, gen, nodeN, edgeN, flags, reserved(u64)
//	node cols: x f32[N], y f32[N], r f32[N], color u32[N], ntype u8[N], status i16[N]
//	edge cols: esrc u32[M], edst u32[M], ecolor u32[M], eflags u8[M]
func encodeHMAP(
	gen uint32,
	x, y, r []float32,
	color []uint32,
	ntype []uint8,
	status []int16,
	esrc, edst []uint32,
	ecolor []uint32,
	eflags []uint8,
) []byte {
	nodeN := len(x)
	edgeN := len(esrc)

	flags := uint32(0)
	// Cluster bubble nodes are ntype==2; if any node is a cluster, advertise it.
	for _, t := range ntype {
		if t == 2 {
			flags |= hmapFlagHasClusters
			break
		}
	}

	// Exact byte budget: header + node columns + edge columns.
	size := 32 +
		nodeN*4 + // x
		nodeN*4 + // y
		nodeN*4 + // r
		nodeN*4 + // color
		nodeN*1 + // ntype
		nodeN*2 + // status
		edgeN*4 + // esrc
		edgeN*4 + // edst
		edgeN*4 + // ecolor
		edgeN*1 // eflags

	buf := make([]byte, size)
	le := binary.LittleEndian

	// Header.
	le.PutUint32(buf[0:], hmapMagic)
	le.PutUint32(buf[4:], hmapVersion)
	le.PutUint32(buf[8:], gen)
	le.PutUint32(buf[12:], uint32(nodeN))
	le.PutUint32(buf[16:], uint32(edgeN))
	le.PutUint32(buf[20:], flags)
	le.PutUint64(buf[24:], 0) // reserved

	off := 32

	// Node columns, contiguous, in contract order.
	for i := 0; i < nodeN; i++ {
		le.PutUint32(buf[off+i*4:], floatBits(x, i))
	}
	off += nodeN * 4
	for i := 0; i < nodeN; i++ {
		le.PutUint32(buf[off+i*4:], floatBits(y, i))
	}
	off += nodeN * 4
	for i := 0; i < nodeN; i++ {
		le.PutUint32(buf[off+i*4:], floatBits(r, i))
	}
	off += nodeN * 4
	for i := 0; i < nodeN; i++ {
		le.PutUint32(buf[off+i*4:], colorAt(color, i))
	}
	off += nodeN * 4
	for i := 0; i < nodeN; i++ {
		buf[off+i] = byteAt(ntype, i)
	}
	off += nodeN
	for i := 0; i < nodeN; i++ {
		le.PutUint16(buf[off+i*2:], uint16(int16At(status, i)))
	}
	off += nodeN * 2

	// Edge columns.
	for i := 0; i < edgeN; i++ {
		le.PutUint32(buf[off+i*4:], esrc[i])
	}
	off += edgeN * 4
	for i := 0; i < edgeN; i++ {
		le.PutUint32(buf[off+i*4:], edst[i])
	}
	off += edgeN * 4
	for i := 0; i < edgeN; i++ {
		le.PutUint32(buf[off+i*4:], colorAt(ecolor, i))
	}
	off += edgeN * 4
	for i := 0; i < edgeN; i++ {
		buf[off+i] = byteAt(eflags, i)
	}

	return buf
}

// floatBits returns the IEEE-754 bit pattern of the i-th float, treating a short
// or nil slice as 0 so callers may omit optional columns without panicking.
func floatBits(s []float32, i int) uint32 {
	if i >= len(s) {
		return 0
	}
	return math.Float32bits(s[i])
}

func colorAt(s []uint32, i int) uint32 {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

func byteAt(s []uint8, i int) uint8 {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

func int16At(s []int16, i int) int16 {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

// hmapJSON is the JSON-fallback shape: the exact same columns as the binary,
// emitted as plain arrays for debugging when the client sends
// Accept: application/json.
type hmapJSON struct {
	Gen    uint32    `json:"gen"`
	NodeN  int       `json:"nodeN"`
	EdgeN  int       `json:"edgeN"`
	X      []float32 `json:"x"`
	Y      []float32 `json:"y"`
	R      []float32 `json:"r"`
	Color  []uint32  `json:"color"`
	NType  []uint8   `json:"ntype"`
	Status []int16   `json:"status"`
	ESrc   []uint32  `json:"esrc"`
	EDst   []uint32  `json:"edst"`
	EColor []uint32  `json:"ecolor"`
	EFlags []uint8   `json:"eflags"`
}

// encodeHMAPJSON produces the JSON-fallback object for the same columns the
// binary encoder would emit. Nil slices are normalized to empty arrays so the
// JS decoder always sees arrays.
func encodeHMAPJSON(
	gen uint32,
	x, y, r []float32,
	color []uint32,
	ntype []uint8,
	status []int16,
	esrc, edst []uint32,
	ecolor []uint32,
	eflags []uint8,
) []byte {
	j := hmapJSON{
		Gen:    gen,
		NodeN:  len(x),
		EdgeN:  len(esrc),
		X:      nz32(x),
		Y:      nz32(y),
		R:      nz32(r),
		Color:  nzU32(color),
		NType:  nzU8(ntype),
		Status: nzI16(status),
		ESrc:   nzU32(esrc),
		EDst:   nzU32(edst),
		EColor: nzU32(ecolor),
		EFlags: nzU8(eflags),
	}
	out, _ := json.Marshal(j)
	return out
}

func nz32(s []float32) []float32 {
	if s == nil {
		return []float32{}
	}
	return s
}
func nzU32(s []uint32) []uint32 {
	if s == nil {
		return []uint32{}
	}
	return s
}
func nzU8(s []uint8) []uint8 {
	if s == nil {
		return []uint8{}
	}
	return s
}
func nzI16(s []int16) []int16 {
	if s == nil {
		return []int16{}
	}
	return s
}

// ============================================================================
// ETag / conditional-request plumbing
// ============================================================================

// etagFor builds the ETag for a /map/* response. The map snapshot is immutable
// per generation, so (gen, endpoint, params) uniquely identifies a body; we hash
// the params so query strings of any length collapse to a short, header-safe
// token. The returned value is already quoted per RFC 7232.
func etagFor(gen uint64, endpoint, params string) string {
	sum := sha1.Sum([]byte(params))
	return `"` + strconv.FormatUint(gen, 10) + "-" + endpoint + "-" + hex.EncodeToString(sum[:8]) + `"`
}

// writeWithETag writes body with the given Content-Type and ETag, honoring an
// inbound If-None-Match: if it matches, it writes 304 with no body. Returns true
// if a 304 was written (so the caller can stop). Snapshots are immutable per
// gen, so a matching ETag is always safe to answer 304.
func writeWithETag(w http.ResponseWriter, r *http.Request, contentType, etag string, body []byte) bool {
	w.Header().Set("ETag", etag)
	// Allow caches/proxies to revalidate; the body changes only when gen changes.
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(body)
	return false
}

// etagMatches reports whether the inbound If-None-Match header (which may be a
// comma-separated list, may include a weak "W/" prefix, or may be "*") matches
// our strong etag. We compare on the opaque-tag portion so weak/strong markers
// don't cause spurious misses.
func etagMatches(header, etag string) bool {
	if header == "*" {
		return true
	}
	target := stripWeak(etag)
	start := 0
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == ',' {
			cand := trimSpace(header[start:i])
			if cand != "" && stripWeak(cand) == target {
				return true
			}
			start = i + 1
		}
	}
	return false
}

func stripWeak(s string) string {
	if len(s) >= 2 && s[0] == 'W' && s[1] == '/' {
		return s[2:]
	}
	return s
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
