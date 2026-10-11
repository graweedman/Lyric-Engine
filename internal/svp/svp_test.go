package svp

import (
	"math"
	"strconv"
	"testing"
)

func TestParseTempoAndGroups(t *testing.T) {
	q := int64(BlicksPerQuarter)
	src := []byte(`{
	  "time": {"meter": [{"index": 0, "numerator": 4, "denominator": 4}, {"index": 1, "numerator": 3, "denominator": 4}],
	           "tempo": [{"position": 0, "bpm": 120}, {"position": ` + itoa(4*q) + `, "bpm": 60}]},
	  "library": [{"uuid": "g1", "notes": [{"onset": 0, "duration": ` + itoa(q) + `, "lyrics": "ref", "pitch": 60}]}],
	  "tracks": [{"name": "Vox",
	    "mainGroup": {"notes": [{"onset": ` + itoa(2*q) + `, "duration": ` + itoa(q) + `, "lyrics": " a ", "pitch": 62}]},
	    "mainRef": {"blickOffset": 0, "database": {"name": "Kasane Teto", "language": "japanese"}},
	    "groups": [{"groupID": "g1", "blickOffset": ` + itoa(5*q) + `, "pitchOffset": 2}]}]
	}` + "\x00")
	p, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if v := p.Tracks[0].Voice; v != "Kasane Teto" {
		t.Errorf("voice = %q", v)
	}
	n := p.Tracks[0].Notes
	if len(n) != 2 {
		t.Fatalf("notes = %d", len(n))
	}
	// 2 quarters at 120bpm = 1.0s
	if !near(n[0].Start, 1.0) || !near(n[0].End, 1.5) || n[0].Lyric != "a" {
		t.Errorf("note0 = %+v", n[0])
	}
	// 4 quarters at 120 = 2s, then 1 quarter at 60 = 1s -> 3s; lasts 1s
	if !near(n[1].Start, 3.0) || !near(n[1].End, 4.0) || n[1].Pitch != 62 {
		t.Errorf("note1 = %+v", n[1])
	}
	// beat 2 is in 4/4 bar 0; beat 5 is 1 beat into the 3/4 bar 1.
	if !near(n[0].Bar, 0.5) || !near(n[1].Bar, 1+1.0/3) {
		t.Errorf("bars = %v, %v", n[0].Bar, n[1].Bar)
	}
	if b := p.BeatsToBars(1.5, 1.5); !near(b, 0.5) {
		t.Errorf("BeatsToBars = %v", b)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
