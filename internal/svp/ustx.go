package svp

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// rawUSTX is the part of an OpenUtau project (.ustx, YAML) that matters here.
// Positions are in ticks of Resolution per quarter note.
type rawUSTX struct {
	Resolution float64 `yaml:"resolution"`
	// Projects before ustx 0.6 have a single tempo and signature.
	BPM            float64 `yaml:"bpm"`
	BeatPerBar     int     `yaml:"beat_per_bar"`
	BeatUnit       int     `yaml:"beat_unit"`
	TimeSignatures []struct {
		BarPosition int `yaml:"bar_position"`
		BeatPerBar  int `yaml:"beat_per_bar"`
		BeatUnit    int `yaml:"beat_unit"`
	} `yaml:"time_signatures"`
	Tempos []struct {
		Position float64 `yaml:"position"`
		BPM      float64 `yaml:"bpm"`
	} `yaml:"tempos"`
	Tracks []struct {
		Singer    string `yaml:"singer"`
		TrackName string `yaml:"track_name"`
		Mute      bool   `yaml:"mute"`
	} `yaml:"tracks"`
	VoiceParts []struct {
		Name     string  `yaml:"name"`
		TrackNo  int     `yaml:"track_no"`
		Position float64 `yaml:"position"`
		Notes    []struct {
			Position float64 `yaml:"position"` // relative to the part
			Duration float64 `yaml:"duration"`
			Tone     int     `yaml:"tone"`
			Lyric    string  `yaml:"lyric"`
		} `yaml:"notes"`
	} `yaml:"voice_parts"`
}

// ParseUSTX parses an OpenUtau project. Each voice part becomes a note group
// (so parts can start stanzas like SynthV groups), and OpenUtau lyrics are
// translated to the SynthV conventions the arranger expects.
func ParseUSTX(data []byte) (*Project, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var u rawUSTX
	if err := yaml.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("ustx: %w", err)
	}
	res := u.Resolution
	if res <= 0 {
		res = 480
	}
	blicks := func(ticks float64) float64 { return ticks / res * BlicksPerQuarter }

	var f rawFile
	for _, t := range u.Tempos {
		f.Time.Tempo = append(f.Time.Tempo, rawTempo{Position: blicks(t.Position), BPM: t.BPM})
	}
	if len(f.Time.Tempo) == 0 && u.BPM > 0 {
		f.Time.Tempo = append(f.Time.Tempo, rawTempo{BPM: u.BPM})
	}
	for _, m := range u.TimeSignatures {
		f.Time.Meter = append(f.Time.Meter, rawMeter{Index: m.BarPosition, Numerator: m.BeatPerBar, Denominator: m.BeatUnit})
	}
	if len(f.Time.Meter) == 0 && u.BeatPerBar > 0 && u.BeatUnit > 0 {
		f.Time.Meter = append(f.Time.Meter, rawMeter{Numerator: u.BeatPerBar, Denominator: u.BeatUnit})
	}

	for i, t := range u.Tracks {
		name := t.TrackName
		if name == "" {
			name = fmt.Sprintf("Track%d", i+1)
		}
		tr := rawTrack{Name: name}
		tr.MainRef.Database.Name = t.Singer
		tr.MainRef.Mute = t.Mute
		f.Tracks = append(f.Tracks, tr)
	}
	for pi, vp := range u.VoiceParts {
		if vp.TrackNo < 0 || vp.TrackNo >= len(f.Tracks) {
			continue
		}
		tr := &f.Tracks[vp.TrackNo]
		g := rawGroup{UUID: fmt.Sprintf("part%d", pi), Name: vp.Name}
		for _, n := range vp.Notes {
			g.Notes = append(g.Notes, rawNote{Onset: blicks(n.Position), Duration: blicks(n.Duration),
				Lyrics: ustxLyric(n.Lyric), Pitch: n.Tone})
		}
		f.Library = append(f.Library, g)
		ref := rawRef{GroupID: g.UUID, BlickOffset: blicks(vp.Position), Mute: tr.MainRef.Mute}
		tr.Groups = append(tr.Groups, ref)
	}
	return build(f), nil
}

// vcvPrefixes are the leading vowels of UTAU VCV lyrics ("a か" sings か).
var vcvPrefixes = []string{"-", "a", "i", "u", "e", "o", "n", "N", "あ", "い", "う", "え", "お", "ん"}

// ustxLyric maps an OpenUtau lyric to the SynthV convention: "+" (and its
// "+~", "+*" variants) extends the previous note, which SynthV writes "-";
// "R" is a rest; phonetic hints ("read[r iy d]") and VCV prefixes are dropped.
func ustxLyric(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '['); i > 0 && strings.HasSuffix(s, "]") {
		s = strings.TrimSpace(s[:i])
	}
	switch {
	case strings.HasPrefix(s, "+"):
		return "-"
	case s == "R" || s == "r":
		return "sil"
	}
	if pre, rest, ok := strings.Cut(s, " "); ok && rest != "" && slices.Contains(vcvPrefixes, pre) {
		return strings.TrimSpace(rest)
	}
	return s
}
