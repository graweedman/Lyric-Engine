// Package svp parses singing synth projects — Synthesizer V Studio (.svp)
// and OpenUtau (.ustx) — into a flat, time-sorted list of notes with
// absolute times in seconds.
package svp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// BlicksPerQuarter is Synthesizer V's internal tick resolution.
const BlicksPerQuarter = 705600000.0

// Note is a single sung note resolved to wall-clock time.
type Note struct {
	Start     float64 // seconds
	End       float64 // seconds
	Bar       float64 // onset in bars from the start (0-based, fractional)
	Lyric     string
	Pitch     int
	Track     int
	Group     int    // placement within the track: 0 = main group, 1.. = group references
	GroupName string // e.g. "Verse 1", "Chorus"
}

// Track summarises one track of the project.
type Track struct {
	Index int
	Name  string
	Voice string // voice database, e.g. "Kasane Teto", "GUMI AI 2"; "" when none
	Notes []Note
}

// Project is the parsed result.
type Project struct {
	Tracks []Track
	meter  meterMap
	tempo  tempoMap
}

// TempoChange is a tempo marking: from Sec seconds (Quarter quarter notes
// into the song) on, the song runs at BPM.
type TempoChange struct{ Sec, Quarter, BPM float64 }

// Tempo returns the tempo map in time order, starting at 0.
func (p *Project) Tempo() []TempoChange {
	out := make([]TempoChange, len(p.tempo))
	for i, t := range p.tempo {
		out[i] = TempoChange{t.sec, t.blick / BlicksPerQuarter, t.bpm}
	}
	return out
}

// MeterChange is a time signature Num/Den starting at measure Bar, which
// is Quarter quarter notes into the song.
type MeterChange struct {
	Bar      int
	Quarter  float64
	Num, Den int
}

// Meter returns the time signatures in order, starting at bar 0.
func (p *Project) Meter() []MeterChange {
	out := make([]MeterChange, len(p.meter))
	for i, m := range p.meter {
		out[i] = MeterChange{m.bar, m.beat, m.num, m.den}
	}
	return out
}

// BeatsToBars converts a length in quarter-note beats to bars at bar position bar.
func (p *Project) BeatsToBars(bar, beats float64) float64 {
	return beats / p.meter.at(bar).quarters
}

type rawFile struct {
	Time struct {
		Meter []rawMeter `json:"meter"`
		Tempo []rawTempo `json:"tempo"`
	} `json:"time"`
	Library []rawGroup `json:"library"`
	Tracks  []rawTrack `json:"tracks"`
}

type rawMeter struct {
	Index       int `json:"index"` // measure where the signature starts
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

type rawTempo struct {
	Position float64 `json:"position"` // blicks
	BPM      float64 `json:"bpm"`
}

type rawTrack struct {
	Name      string   `json:"name"`
	MainGroup rawGroup `json:"mainGroup"`
	MainRef   rawRef   `json:"mainRef"`
	Groups    []rawRef `json:"groups"`
}

type rawGroup struct {
	UUID  string    `json:"uuid"`
	Name  string    `json:"name"`
	Notes []rawNote `json:"notes"`
}

type rawRef struct {
	GroupID     string  `json:"groupID"`
	BlickOffset float64 `json:"blickOffset"`
	PitchOffset int     `json:"pitchOffset"`
	Database    struct {
		Name string `json:"name"`
	} `json:"database"`
	IsInstrumental bool `json:"isInstrumental"`
	Mute           bool `json:"mute"`
	// Visible range of the group on the timeline (newer versions; absent
	// in older files, and End is -1 when unbounded).
	AbsBegin *float64 `json:"blickAbsoluteBegin"`
	AbsEnd   *float64 `json:"blickAbsoluteEnd"`
}

// visible reports whether absolute blick position b is inside the ref's crop range.
func (r rawRef) visible(b float64) bool {
	if r.AbsBegin != nil && b < *r.AbsBegin {
		return false
	}
	if r.AbsEnd != nil && *r.AbsEnd >= 0 && b >= *r.AbsEnd {
		return false
	}
	return true
}

type rawNote struct {
	Onset    float64 `json:"onset"`
	Duration float64 `json:"duration"`
	Lyrics   string  `json:"lyrics"`
	Pitch    int     `json:"pitch"`
}

// Exts are the project file extensions ParseFile understands.
var Exts = []string{".svp", ".ustx"}

// IsProject reports whether path has a project extension (.svp or .ustx).
func IsProject(path string) bool {
	return slices.Contains(Exts, strings.ToLower(filepath.Ext(path)))
}

// ParseFile reads and parses an .svp or OpenUtau .ustx file.
func ParseFile(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".ustx") {
		return ParseUSTX(data)
	}
	return Parse(data)
}

// Parse parses .svp JSON bytes.
func Parse(data []byte) (*Project, error) {
	// SynthV writes a trailing NUL byte; some files also carry a BOM.
	data = bytes.TrimRight(data, "\x00 \r\n\t")
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var f rawFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("svp: %w", err)
	}
	return build(f), nil
}

// build resolves a project to wall-clock notes.
func build(f rawFile) *Project {
	tm := newTempoMap(f)
	lib := make(map[string]rawGroup, len(f.Library))
	for _, g := range f.Library {
		lib[g.UUID] = g
	}

	p := &Project{meter: newMeterMap(f), tempo: tm}
	for ti, t := range f.Tracks {
		tr := Track{Index: ti, Name: t.Name, Voice: strings.TrimSpace(t.MainRef.Database.Name)}
		add := func(g rawGroup, ref rawRef, gi int) {
			if ref.Mute || ref.IsInstrumental {
				return
			}
			for _, n := range g.Notes {
				on := n.Onset + ref.BlickOffset
				if !ref.visible(on) {
					continue
				}
				tr.Notes = append(tr.Notes, Note{
					Start:     tm.seconds(on),
					End:       tm.seconds(on + n.Duration),
					Bar:       p.meter.bar(on / BlicksPerQuarter),
					Lyric:     strings.TrimSpace(n.Lyrics),
					Pitch:     n.Pitch + ref.PitchOffset,
					Track:     ti,
					Group:     gi,
					GroupName: g.Name,
				})
			}
		}
		add(t.MainGroup, t.MainRef, 0)
		for i, ref := range t.Groups {
			if g, ok := lib[ref.GroupID]; ok {
				add(g, ref, i+1)
			}
		}
		sort.SliceStable(tr.Notes, func(i, j int) bool { return tr.Notes[i].Start < tr.Notes[j].Start })
		p.Tracks = append(p.Tracks, tr)
	}
	return p
}

// BestTrack returns the index of the track with the most notes.
func (p *Project) BestTrack() int {
	best, n := -1, -1
	for i, t := range p.Tracks {
		if len(t.Notes) > n {
			best, n = i, len(t.Notes)
		}
	}
	return best
}

type tempoPoint struct {
	blick, bpm, sec float64
}

type tempoMap []tempoPoint

func newTempoMap(f rawFile) tempoMap {
	var tm tempoMap
	for _, t := range f.Time.Tempo {
		if t.BPM > 0 {
			tm = append(tm, tempoPoint{blick: t.Position, bpm: t.BPM})
		}
	}
	sort.Slice(tm, func(i, j int) bool { return tm[i].blick < tm[j].blick })
	if len(tm) == 0 || tm[0].blick > 0 {
		bpm := 120.0
		if len(tm) > 0 {
			bpm = tm[0].bpm
		}
		tm = append(tempoMap{{blick: 0, bpm: bpm}}, tm...)
	}
	for i := 1; i < len(tm); i++ {
		prev := tm[i-1]
		tm[i].sec = prev.sec + (tm[i].blick-prev.blick)/BlicksPerQuarter*60/prev.bpm
	}
	return tm
}

func (tm tempoMap) seconds(blick float64) float64 {
	i := sort.Search(len(tm), func(i int) bool { return tm[i].blick > blick }) - 1
	if i < 0 {
		i = 0
	}
	p := tm[i]
	return p.sec + (blick-p.blick)/BlicksPerQuarter*60/p.bpm
}

type meterPoint struct {
	bar      int     // measure index where it starts
	beat     float64 // quarter-note position of that measure
	quarters float64 // quarter notes per bar
	num, den int     // the signature, e.g. 6/8
}

type meterMap []meterPoint

func newMeterMap(f rawFile) meterMap {
	var mm meterMap
	for _, m := range f.Time.Meter {
		if m.Numerator > 0 && m.Denominator > 0 && m.Index >= 0 {
			mm = append(mm, meterPoint{bar: m.Index, quarters: float64(m.Numerator) * 4 / float64(m.Denominator),
				num: m.Numerator, den: m.Denominator})
		}
	}
	sort.Slice(mm, func(i, j int) bool { return mm[i].bar < mm[j].bar })
	if len(mm) == 0 || mm[0].bar > 0 {
		first := meterPoint{quarters: 4, num: 4, den: 4}
		if len(mm) > 0 {
			first = mm[0]
		}
		first.bar = 0
		mm = append(meterMap{first}, mm...)
	}
	for i := 1; i < len(mm); i++ {
		prev := mm[i-1]
		mm[i].beat = prev.beat + float64(mm[i].bar-prev.bar)*prev.quarters
	}
	return mm
}

// bar converts a quarter-note position to a fractional bar index.
func (mm meterMap) bar(beat float64) float64 {
	i := sort.Search(len(mm), func(i int) bool { return mm[i].beat > beat }) - 1
	m := mm[max(i, 0)]
	return float64(m.bar) + (beat-m.beat)/m.quarters
}

// at returns the signature in effect at fractional bar b.
func (mm meterMap) at(b float64) meterPoint {
	i := sort.Search(len(mm), func(i int) bool { return float64(mm[i].bar) > b }) - 1
	return mm[max(i, 0)]
}
