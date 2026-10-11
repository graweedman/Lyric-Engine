package lyr

import (
	"math"
	"reflect"
	"testing"
)

func TestDetectKey(t *testing.T) {
	scale := func(tonic int, steps []int, weights []float64) (h [12]float64) {
		p := tonic
		for i, w := range weights {
			h[p%12] += w
			p += steps[i%len(steps)]
		}
		return h
	}
	major, minor := []int{2, 2, 1, 2, 2, 2, 1}, []int{2, 1, 2, 2, 1, 2, 2}
	tonicHeavy := []float64{4, 1, 2, 1, 3, 1, 1} // tonic, third and fifth sung longest
	if k := DetectKey(scale(0, major, tonicHeavy)); k.String() != "C major" || k.Score < 0.7 {
		t.Errorf("C major scale: %v (%.2f)", k, k.Score)
	}
	if k := DetectKey(scale(9, minor, tonicHeavy)); k.String() != "A minor" {
		t.Errorf("A minor scale: %v (%.2f)", k, k.Score)
	}
	if k := DetectKey([12]float64{}); k.Known() {
		t.Errorf("no notes should give no key, got %v", k)
	}
	if NoteName(60) != "C4" || NoteName(70) != "A#4" {
		t.Errorf("note names: %s %s", NoteName(60), NoteName(70))
	}
}

func TestBeatsAndBars(t *testing.T) {
	// 120 bpm, then 60 bpm from beat 8 (4 s); 4/4, then 3/4 from bar 2.
	a := Analysis{
		Tempo: []TempoPoint{{0, 0, 120}, {4000, 8, 60}},
		Meter: []MeterPoint{{0, 0, 4, 4}, {8, 2, 3, 4}},
	}
	cases := []struct {
		ms        int64
		bpm, beat float64
		bar       int
		inBar     float64
	}{
		{0, 120, 0, 0, 0},
		{1250, 120, 2.5, 0, 2.5},
		{4000, 60, 8, 2, 0},
		{7500, 60, 11.5, 3, 0.5},
	}
	for _, c := range cases {
		beat, ok := a.BeatAt(c.ms)
		_, bar, inBar := a.BarAt(beat)
		if !ok || a.BPMAt(c.ms) != c.bpm || math.Abs(beat-c.beat) > 1e-6 || bar != c.bar || math.Abs(inBar-c.inBar) > 1e-6 {
			t.Errorf("at %d ms: bpm %v beat %v bar %d+%v", c.ms, a.BPMAt(c.ms), beat, bar, inBar)
		}
	}
	if _, ok := (&Analysis{}).BeatAt(0); ok {
		t.Error("no tempo map should report unknown")
	}
}

func TestAnalysisRoundTrip(t *testing.T) {
	s := FromNotes(notes(0.0, 0.5, "a", 0.5, 1.0, "b"), rests, nil)
	s.Analysis = Analysis{
		Tempo: []TempoPoint{{0, 0, 122}, {60000, 122, 90.5}},
		Meter: []MeterPoint{{0, 0, 4, 4}, {96, 24, 6, 8}},
		Key:   Key{Tonic: 1, Minor: true, Score: 0.87},
		Low:   55, High: 81,
	}
	s.Display.HUD = true
	got, err := Decode(Encode(s))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Analysis, s.Analysis) || !got.Display.HUD {
		t.Errorf("analysis:\n got %+v\nwant %+v", got.Analysis, s.Analysis)
	}
}
