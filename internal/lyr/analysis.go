package lyr

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/graweedman/lyric-engine/internal/svp"
)

// Analysis is musical data about the song, shown by the analysis readout:
// the tempo map and time signatures from the SynthV project, the key
// detected from the sung notes, and the vocal range.
type Analysis struct {
	Tempo     []TempoPoint // sorted by time; empty when unknown
	Meter     []MeterPoint // sorted by beat; empty means 4/4
	Key       Key
	Low, High uint8 // MIDI range of the sung notes; 0 when unknown
}

// TempoPoint: from Ms on (which is Beat quarter notes into the song) the
// song runs at BPM.
type TempoPoint struct {
	Ms   uint32
	Beat float32
	BPM  float32
}

// MeterPoint: a time signature Num/Den starting at measure Bar, which is
// Beat quarter notes into the song.
type MeterPoint struct {
	Beat     float32
	Bar      uint16
	Num, Den uint8
}

// Key is a detected key: the tonic as a pitch class (0 = C … 11 = B) and
// the mode. Score is the correlation with the key's profile (0 = unknown).
type Key struct {
	Tonic uint8
	Minor bool
	Score float32
}

// Known reports whether a key was detected.
func (k Key) Known() bool { return k.Score > 0 }

var pitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func (k Key) String() string {
	if !k.Known() {
		return "?"
	}
	if k.Minor {
		return pitchNames[k.Tonic%12] + " minor"
	}
	return pitchNames[k.Tonic%12] + " major"
}

// NoteName names a MIDI note, e.g. 60 → "C4".
func NoteName(midi uint8) string {
	return fmt.Sprintf("%s%d", pitchNames[midi%12], int(midi)/12-1)
}

// Krumhansl–Kessler key profiles: how strongly each scale degree belongs
// to a major and a minor key.
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// DetectKey finds the key whose profile best correlates with how long each
// pitch class is sung (the Krumhansl–Schmuckler algorithm). hist[pc] is
// the total duration of pitch class pc.
func DetectKey(hist [12]float64) Key {
	var best Key
	bestR := 0.0
	for tonic := range 12 {
		for _, minor := range []bool{false, true} {
			prof := majorProfile
			if minor {
				prof = minorProfile
			}
			var rot [12]float64
			for i := range 12 {
				rot[(i+tonic)%12] = prof[i]
			}
			if r := correlation(hist, rot); r > bestR {
				bestR, best = r, Key{Tonic: uint8(tonic), Minor: minor, Score: float32(r)}
			}
		}
	}
	return best
}

func correlation(a, b [12]float64) float64 {
	var ma, mb float64
	for i := range 12 {
		ma += a[i] / 12
		mb += b[i] / 12
	}
	var num, da, db float64
	for i := range 12 {
		num += (a[i] - ma) * (b[i] - mb)
		da += (a[i] - ma) * (a[i] - ma)
		db += (b[i] - mb) * (b[i] - mb)
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}

// noteStats accumulates the pitch-class histogram and range of notes.
type noteStats struct {
	hist      [12]float64
	low, high uint8
}

func (n *noteStats) add(pitch int, dur float64) {
	if pitch <= 0 || pitch > 127 || dur <= 0 {
		return
	}
	p := uint8(pitch)
	n.hist[p%12] += dur
	if n.low == 0 || p < n.low {
		n.low = p
	}
	n.high = max(n.high, p)
}

func (n *noteStats) apply(a *Analysis) {
	a.Key = DetectKey(n.hist)
	a.Low, a.High = n.low, n.high
}

// AnalyzeProject fills the analysis from a SynthV project: its tempo map
// and time signatures, and the key and range of the given tracks' notes.
func (s *Song) AnalyzeProject(p *svp.Project, tracks ...int) {
	a := &s.Analysis
	a.Tempo, a.Meter = nil, nil
	for _, t := range p.Tempo() {
		a.Tempo = append(a.Tempo, TempoPoint{Ms: ms(t.Sec), Beat: float32(t.Quarter), BPM: float32(t.BPM)})
	}
	for _, m := range p.Meter() {
		a.Meter = append(a.Meter, MeterPoint{Beat: float32(m.Quarter), Bar: uint16(clamp(m.Bar, 0, 0xFFFF)),
			Num: uint8(clamp(m.Num, 1, 255)), Den: uint8(clamp(m.Den, 1, 255))})
	}
	var st noteStats
	for _, ti := range tracks {
		if ti < 0 || ti >= len(p.Tracks) {
			continue
		}
		for _, n := range p.Tracks[ti].Notes {
			if skip, _ := skipLyric(n.Lyric); !skip {
				st.add(n.Pitch, n.End-n.Start)
			}
		}
	}
	st.apply(a)
}

// AnalyzeTokens detects the key and range from the words alone, for songs
// whose SynthV project is not available.
func (s *Song) AnalyzeTokens() {
	var st noteStats
	for _, t := range s.Tokens {
		st.add(int(t.Pitch), float64(t.End-t.Start))
	}
	st.apply(&s.Analysis)
}

// Open loads a .lyrb file. Files saved before the analysis existed get it
// from their source .svp when it is still there, or from the words.
func Open(path string) (*Song, error) {
	s, err := Load(path)
	if err != nil || len(s.Analysis.Tempo) > 0 {
		return s, err
	}
	if src := Resolve(path, s.SourcePath); src != "" {
		if _, statErr := os.Stat(src); statErr == nil {
			if p, perr := svp.ParseFile(src); perr == nil {
				ti := s.Import.Track
				if ti < 0 || ti >= len(p.Tracks) {
					ti = p.BestTrack()
				}
				s.AnalyzeProject(p, ti, s.Import.BackingTrack-1)
				return s, nil
			}
		}
	}
	if !s.Analysis.Key.Known() {
		s.AnalyzeTokens()
	}
	return s, nil
}

// BeatAt is the position in quarter notes at lyric time t (ms), or false
// when the tempo is unknown.
func (a *Analysis) BeatAt(t int64) (float64, bool) {
	tp, ok := a.tempoAt(t)
	if !ok {
		return 0, false
	}
	return float64(tp.Beat) + float64(max(t, 0)-int64(tp.Ms))/60000*float64(tp.BPM), true
}

// BPMAt is the tempo at lyric time t (ms), or 0 when unknown.
func (a *Analysis) BPMAt(t int64) float64 {
	tp, _ := a.tempoAt(t)
	return float64(tp.BPM)
}

func (a *Analysis) tempoAt(t int64) (TempoPoint, bool) {
	if len(a.Tempo) == 0 {
		return TempoPoint{}, false
	}
	i := sort.Search(len(a.Tempo), func(i int) bool { return int64(a.Tempo[i].Ms) > t }) - 1
	return a.Tempo[max(i, 0)], true
}

// BarAt is the time signature in effect at quarter-note position beat, the
// 0-based bar number, and the 0-based beat within the bar (in the
// signature's beat unit, fractional).
func (a *Analysis) BarAt(beat float64) (m MeterPoint, bar int, inBar float64) {
	m = MeterPoint{Num: 4, Den: 4}
	if i := sort.Search(len(a.Meter), func(i int) bool { return float64(a.Meter[i].Beat) > beat }) - 1; i >= 0 &&
		a.Meter[i].Num > 0 && a.Meter[i].Den > 0 {
		m = a.Meter[i]
	}
	quarters := float64(m.Num) * 4 / float64(m.Den)
	into := max(beat-float64(m.Beat), 0)
	n := math.Floor(into / quarters)
	return m, int(m.Bar) + int(n), (into - n*quarters) * float64(m.Den) / 4
}
