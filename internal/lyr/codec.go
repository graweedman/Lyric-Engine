package lyr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"os"
)

// .lyrb — compact little-endian binary lyric format.
//
// Everything is fixed-size records plus one string blob, so a reader does a
// single file read and no allocation per record beyond the token strings.
// Lines are precomputed and sorted by start time so a player can binary
// search the current line without deriving anything.
//
//	Header (100 bytes; 88 in v4; 80 in v3; 76 in v2; 52 in v1, which has no import block)
//	  0  [4]  magic "LYRB"
//	  4  u16  version (5)
//	  6  u16  message count (0 in files written before messages)
//	  8  u32  token count
//	 12  u32  line count
//	 16  u32  string blob size
//	 20  u32  duration ms
//	 24  i32  audio offset ms
//	 28  u8   display.before
//	 29  u8   display.after
//	 30  u16  display.leadIn ms
//	 32  str  title       (str = u32 blob offset + u16 length)
//	 38  str  source path
//	 44  str  audio path
//	 50  u16  metadata field count (v2; 0 in files written before metadata)
//	Import settings (v2)
//	 52  u8   mode (0 bars, 1 rests)
//	 53  u8   bars per line
//	 54  u8   flags (bit0 stanza per note group)
//	 55  u8   display view (0 focus, 1 log)
//	 56  i16  track (-1 auto)
//	 58  u16  max chars
//	 60  f32  pickup beats
//	 64  f32  line gap s
//	 68  f32  stanza gap s
//	 72  u32  syllable segment count (0 in files written before segments)
//	 76  u16  backing track + 1 (v3; 0 = none)
//	 78  u16  reserved
//	 80  u32  lead lyric color 0xRRGGBB (v4; 0 = theme default)
//	 84  u32  backing lyric color 0xRRGGBB (v4; 0 = theme default)
//	Analysis (v5)
//	 88  u16  tempo point count
//	 90  u16  meter point count
//	 92  u8   key tonic pitch class (0 = C … 11 = B)
//	 93  u8   flags (bit0 minor key, bit1 analysis readout shown)
//	 94  u8   lowest sung note (MIDI; 0 = unknown)
//	 95  u8   highest sung note
//	 96  f32  key score (correlation; 0 = no key detected)
//	Lines   count × 16 bytes: start u32, end u32, first u32, count u16, flags u16
//	Tokens  count × 16 bytes: start u32, end u32, textOff u32, textLen u16, flags u8, pitch u8
//	        (flags: bit0 join, bit1 hidden, bit2 backing vocal)
//	Segs    count × 12 bytes: start u32, end u32, token u32 — syllable notes of
//	        multi-note words, grouped by token in order (single-note words have none)
//	Meta    count × 12 bytes: key str, value str (str = u32 blob offset + u16 length)
//	Msgs    count × 16 bytes: time u32, text str, flags u8 (bit0 bold, bit1 italic),
//	        reserved u8, color u32 (0xRRGGBB, 0xFF000000 = default); sorted by time
//	Tempo   count × 12 bytes: ms u32, beat f32 (quarter notes), bpm f32; sorted by time
//	Meter   count × 8 bytes: beat f32, bar u16, numerator u8, denominator u8
//	Blob    UTF-8 bytes
//	CRC32   u32 (IEEE) over everything before it
const (
	magic      = "LYRB"
	version    = 5
	headerV1   = 52
	headerV2   = 76
	headerV3   = 80
	headerV4   = 88
	headerSize = 100
	lineSize   = 16
	tokenSize  = 16
	segSize    = 12
	metaSize   = 12
	msgSize    = 16
	tempoSize  = 12
	meterSize  = 8
)

const (
	tokJoin   = 1 << 0
	tokHidden = 1 << 1
	tokBack   = 1 << 2

	lineStanza = 1 << 0

	msgBold   = 1 << 0
	msgItalic = 1 << 1

	importGroupStanza = 1 << 0

	anaMinor = 1 << 0
	anaHUD   = 1 << 1
)

var ErrFormat = errors.New("lyrb: invalid file")

// Encode serialises the song.
func Encode(s *Song) []byte {
	lines := s.Lines()
	var blob []byte
	addStr := func(str string) (uint32, uint16) {
		if len(str) > 0xFFFF {
			str = str[:0xFFFF]
		}
		off := uint32(len(blob))
		blob = append(blob, str...)
		return off, uint16(len(str))
	}

	type ref struct {
		off uint32
		n   uint16
	}
	var title, src, audio ref
	title.off, title.n = addStr(s.Title)
	src.off, src.n = addStr(s.SourcePath)
	audio.off, audio.n = addStr(s.AudioPath)

	toks := make([]byte, len(s.Tokens)*tokenSize)
	for i, t := range s.Tokens {
		off, n := addStr(t.Text)
		b := toks[i*tokenSize:]
		le.PutUint32(b[0:], t.Start)
		le.PutUint32(b[4:], t.End)
		le.PutUint32(b[8:], off)
		le.PutUint16(b[12:], n)
		var f uint8
		if t.Join {
			f |= tokJoin
		}
		if t.Hidden {
			f |= tokHidden
		}
		if t.Backing {
			f |= tokBack
		}
		b[14] = f
		b[15] = t.Pitch
	}

	meta := s.Meta[:min(len(s.Meta), 0xFFFF)]
	metas := make([]byte, len(meta)*metaSize)
	for i, m := range meta {
		b := metas[i*metaSize:]
		off, n := addStr(m.Key)
		le.PutUint32(b[0:], off)
		le.PutUint16(b[4:], n)
		off, n = addStr(m.Value)
		le.PutUint32(b[6:], off)
		le.PutUint16(b[10:], n)
	}

	msgs := s.Messages[:min(len(s.Messages), 0xFFFF)]
	mrecs := make([]byte, len(msgs)*msgSize)
	for i, m := range msgs {
		b := mrecs[i*msgSize:]
		off, n := addStr(m.Text)
		le.PutUint32(b[0:], m.Time)
		le.PutUint32(b[4:], off)
		le.PutUint16(b[8:], n)
		if m.Bold {
			b[10] |= msgBold
		}
		if m.Italic {
			b[10] |= msgItalic
		}
		le.PutUint32(b[12:], m.Color)
	}

	out := make([]byte, headerSize, headerSize+len(lines)*lineSize+len(toks)+len(blob)+4)
	copy(out[0:], magic)
	le.PutUint16(out[4:], version)
	le.PutUint32(out[8:], uint32(len(s.Tokens)))
	le.PutUint32(out[12:], uint32(len(lines)))
	le.PutUint32(out[16:], uint32(len(blob)))
	le.PutUint32(out[20:], s.Duration())
	le.PutUint32(out[24:], uint32(s.AudioOffsetMs))
	out[28] = s.Display.Before
	out[29] = s.Display.After
	le.PutUint16(out[30:], s.Display.LeadIn)
	for i, r := range []ref{title, src, audio} {
		le.PutUint32(out[32+i*6:], r.off)
		le.PutUint16(out[36+i*6:], r.n)
	}
	le.PutUint16(out[50:], uint16(len(meta)))
	le.PutUint16(out[6:], uint16(len(msgs)))
	out[55] = uint8(s.Display.View)
	im := s.Import
	out[52] = uint8(im.Mode)
	out[53] = uint8(clamp(im.BarsPerLine, 0, 255))
	if im.GroupStanza {
		out[54] |= importGroupStanza
	}
	le.PutUint16(out[56:], uint16(int16(clamp(im.Track, -1, 0x7FFF))))
	le.PutUint16(out[58:], uint16(clamp(im.MaxChars, 0, 0xFFFF)))
	le.PutUint32(out[60:], math.Float32bits(float32(im.Pickup)))
	le.PutUint32(out[64:], math.Float32bits(float32(im.LineGap)))
	le.PutUint32(out[68:], math.Float32bits(float32(im.StanzaGap)))
	var segs []byte
	for i, t := range s.Tokens {
		for _, sg := range t.Segs {
			segs = le.AppendUint32(segs, sg.Start)
			segs = le.AppendUint32(segs, sg.End)
			segs = le.AppendUint32(segs, uint32(i))
		}
	}
	le.PutUint32(out[72:], uint32(len(segs)/segSize))
	le.PutUint16(out[76:], uint16(clamp(im.BackingTrack, 0, 0xFFFF)))
	le.PutUint32(out[80:], s.Display.LeadColor)
	le.PutUint32(out[84:], s.Display.BackColor)
	an := s.Analysis
	tempo := an.Tempo[:min(len(an.Tempo), 0xFFFF)]
	meter := an.Meter[:min(len(an.Meter), 0xFFFF)]
	le.PutUint16(out[88:], uint16(len(tempo)))
	le.PutUint16(out[90:], uint16(len(meter)))
	out[92] = an.Key.Tonic
	if an.Key.Minor {
		out[93] |= anaMinor
	}
	if s.Display.HUD {
		out[93] |= anaHUD
	}
	out[94], out[95] = an.Low, an.High
	le.PutUint32(out[96:], math.Float32bits(an.Key.Score))
	var anaRecs []byte
	for _, t := range tempo {
		anaRecs = le.AppendUint32(anaRecs, t.Ms)
		anaRecs = le.AppendUint32(anaRecs, math.Float32bits(t.Beat))
		anaRecs = le.AppendUint32(anaRecs, math.Float32bits(t.BPM))
	}
	for _, m := range meter {
		anaRecs = le.AppendUint32(anaRecs, math.Float32bits(m.Beat))
		anaRecs = le.AppendUint16(anaRecs, m.Bar)
		anaRecs = append(anaRecs, m.Num, m.Den)
	}
	for _, l := range lines {
		var b [lineSize]byte
		le.PutUint32(b[0:], l.Start)
		le.PutUint32(b[4:], l.End)
		le.PutUint32(b[8:], uint32(l.First))
		le.PutUint16(b[12:], uint16(min(l.Count, 0xFFFF)))
		if l.Stanza {
			le.PutUint16(b[14:], lineStanza)
		}
		out = append(out, b[:]...)
	}
	out = append(out, toks...)
	out = append(out, segs...)
	out = append(out, metas...)
	out = append(out, mrecs...)
	out = append(out, anaRecs...)
	out = append(out, blob...)
	return le.AppendUint32(out, crc32.ChecksumIEEE(out))
}

var le = binary.LittleEndian

// Decode parses a .lyrb buffer.
func Decode(data []byte) (*Song, error) {
	if len(data) < headerV1+4 || string(data[:4]) != magic {
		return nil, ErrFormat
	}
	hdr := headerSize
	switch v := le.Uint16(data[4:]); v {
	case 1:
		hdr = headerV1
	case 2:
		hdr = headerV2
	case 3:
		hdr = headerV3
	case 4:
		hdr = headerV4
	case version:
	default:
		return nil, fmt.Errorf("lyrb: unsupported version %d", v)
	}
	if len(data) < hdr+4 {
		return nil, ErrFormat
	}
	body, sum := data[:len(data)-4], le.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, fmt.Errorf("lyrb: checksum mismatch")
	}
	nTok := int(le.Uint32(data[8:]))
	nLine := int(le.Uint32(data[12:]))
	nBlob := int(le.Uint32(data[16:]))
	lineOff := hdr
	tokOff := lineOff + nLine*lineSize
	nSeg, nMeta, nMsg := 0, 0, 0
	if hdr >= headerV2 {
		nSeg = int(le.Uint32(data[72:]))
		nMeta = int(le.Uint16(data[50:]))
		nMsg = int(le.Uint16(data[6:]))
	}
	segOff := tokOff + nTok*tokenSize
	metaOff := segOff + nSeg*segSize
	msgOff := metaOff + nMeta*metaSize
	nTempo, nMeter := 0, 0
	if hdr >= headerSize {
		nTempo, nMeter = int(le.Uint16(data[88:])), int(le.Uint16(data[90:]))
	}
	tempoOff := msgOff + nMsg*msgSize
	meterOff := tempoOff + nTempo*tempoSize
	blobOff := meterOff + nMeter*meterSize
	if blobOff+nBlob != len(body) {
		return nil, ErrFormat
	}
	blob := body[blobOff:]
	str := func(off uint32, n uint16) (string, error) {
		end := int(off) + int(n)
		if end > len(blob) {
			return "", ErrFormat
		}
		return string(blob[off:end]), nil
	}

	s := &Song{
		AudioOffsetMs: int32(le.Uint32(data[24:])),
		Display:       Display{Before: data[28], After: data[29], LeadIn: le.Uint16(data[30:])},
		Import:        DefaultImport,
		Tokens:        make([]Token, nTok),
	}
	if hdr >= headerV2 {
		s.Display.View = ViewMode(data[55])
		s.Import = ImportOptions{
			Mode:        LineMode(data[52]),
			BarsPerLine: int(data[53]),
			GroupStanza: data[54]&importGroupStanza != 0,
			Track:       int(int16(le.Uint16(data[56:]))),
			MaxChars:    int(le.Uint16(data[58:])),
			Pickup:      float64(math.Float32frombits(le.Uint32(data[60:]))),
			LineGap:     float64(math.Float32frombits(le.Uint32(data[64:]))),
			StanzaGap:   float64(math.Float32frombits(le.Uint32(data[68:]))),
		}
	}
	if hdr >= headerV3 {
		s.Import.BackingTrack = int(le.Uint16(data[76:]))
	}
	if hdr >= headerV4 {
		s.Display.LeadColor = le.Uint32(data[80:])
		s.Display.BackColor = le.Uint32(data[84:])
	}
	if hdr >= headerSize {
		an := &s.Analysis
		an.Key = Key{Tonic: data[92] % 12, Minor: data[93]&anaMinor != 0,
			Score: math.Float32frombits(le.Uint32(data[96:]))}
		s.Display.HUD = data[93]&anaHUD != 0
		an.Low, an.High = data[94], data[95]
		for i := range nTempo {
			b := data[tempoOff+i*tempoSize:]
			an.Tempo = append(an.Tempo, TempoPoint{Ms: le.Uint32(b[0:]),
				Beat: math.Float32frombits(le.Uint32(b[4:])), BPM: math.Float32frombits(le.Uint32(b[8:]))})
		}
		for i := range nMeter {
			b := data[meterOff+i*meterSize:]
			an.Meter = append(an.Meter, MeterPoint{Beat: math.Float32frombits(le.Uint32(b[0:])),
				Bar: le.Uint16(b[4:]), Num: b[6], Den: b[7]})
		}
	}
	var err error
	strs := []*string{&s.Title, &s.SourcePath, &s.AudioPath}
	for i, dst := range strs {
		if *dst, err = str(le.Uint32(data[32+i*6:]), le.Uint16(data[36+i*6:])); err != nil {
			return nil, err
		}
	}

	for i := range s.Tokens {
		b := data[tokOff+i*tokenSize:]
		t := &s.Tokens[i]
		t.Start, t.End = le.Uint32(b[0:]), le.Uint32(b[4:])
		if t.Text, err = str(le.Uint32(b[8:]), le.Uint16(b[12:])); err != nil {
			return nil, err
		}
		t.Join = b[14]&tokJoin != 0
		t.Hidden = b[14]&tokHidden != 0
		t.Backing = b[14]&tokBack != 0
		t.Pitch = b[15]
	}

	for i := 0; i < nSeg; i++ {
		b := data[segOff+i*segSize:]
		ti := int(le.Uint32(b[8:]))
		if ti >= nTok {
			return nil, ErrFormat
		}
		s.Tokens[ti].Segs = append(s.Tokens[ti].Segs, Seg{le.Uint32(b[0:]), le.Uint32(b[4:])})
	}

	for i := 0; i < nMeta; i++ {
		b := data[metaOff+i*metaSize:]
		var m MetaField
		if m.Key, err = str(le.Uint32(b[0:]), le.Uint16(b[4:])); err != nil {
			return nil, err
		}
		if m.Value, err = str(le.Uint32(b[6:]), le.Uint16(b[10:])); err != nil {
			return nil, err
		}
		s.Meta = append(s.Meta, m)
	}

	for i := 0; i < nMsg; i++ {
		b := data[msgOff+i*msgSize:]
		m := Message{Time: le.Uint32(b[0:]), Color: le.Uint32(b[12:]),
			Bold: b[10]&msgBold != 0, Italic: b[10]&msgItalic != 0}
		if m.Text, err = str(le.Uint32(b[4:]), le.Uint16(b[8:])); err != nil {
			return nil, err
		}
		s.Messages = append(s.Messages, m)
	}

	// Restore breaks from the line table; the line table is then reused as-is.
	s.lines = make([]Line, nLine)
	stanza := -1
	for i := range s.lines {
		b := data[lineOff+i*lineSize:]
		l := Line{
			Start: le.Uint32(b[0:]), End: le.Uint32(b[4:]),
			First: int(le.Uint32(b[8:])), Count: int(le.Uint16(b[12:])),
			Stanza: le.Uint16(b[14:])&lineStanza != 0,
		}
		if l.First >= nTok || l.First+l.Count > nTok {
			return nil, ErrFormat
		}
		if l.Stanza {
			stanza++
		}
		l.Stanzanum = stanza
		s.lines[i] = l
		if l.Stanza {
			s.Tokens[l.First].Break = BreakStanza
		} else {
			s.Tokens[l.First].Break = BreakLine
		}
	}
	return s, nil
}

// Load reads a .lyrb file.
func Load(path string) (*Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// Save writes a .lyrb file atomically.
func Save(path string, s *Song) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, Encode(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
