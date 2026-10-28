package svp

import "testing"

func TestParseUSTX(t *testing.T) {
	src := []byte(`name: Song
ustx_version: "0.6"
resolution: 480
time_signatures:
- bar_position: 0
  beat_per_bar: 4
  beat_unit: 4
- bar_position: 1
  beat_per_bar: 3
  beat_unit: 4
tempos:
- position: 0
  bpm: 120
- position: 1920
  bpm: 60
tracks:
- singer: 重音テト
  track_name: Vox
  mute: false
- singer: Backing
  mute: true
voice_parts:
- name: Chorus
  track_no: 0
  position: 1920
  notes:
  - {position: 480, duration: 480, tone: 62, lyric: "a か"}
  - {position: 960, duration: 240, tone: 62, lyric: "+~"}
- name: Verse
  track_no: 0
  position: 0
  notes:
  - {position: 960, duration: 480, tone: 60, lyric: "read[r iy d]"}
- name: Muted
  track_no: 1
  position: 0
  notes:
  - {position: 0, duration: 480, tone: 60, lyric: la}
`)
	p, err := ParseUSTX(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tracks) != 2 || p.Tracks[0].Name != "Vox" || p.Tracks[0].Voice != "重音テト" || p.Tracks[1].Name != "Track2" {
		t.Fatalf("tracks = %+v", p.Tracks)
	}
	if len(p.Tracks[1].Notes) != 0 {
		t.Errorf("muted track has notes: %+v", p.Tracks[1].Notes)
	}
	n := p.Tracks[0].Notes
	if len(n) != 3 {
		t.Fatalf("notes = %+v", n)
	}
	// 2 quarters at 120bpm = 1.0s, in the Verse part.
	if !near(n[0].Start, 1.0) || !near(n[0].End, 1.5) || n[0].Lyric != "read" || n[0].GroupName != "Verse" {
		t.Errorf("note0 = %+v", n[0])
	}
	// Beat 5: 4 quarters at 120 = 2s, then 1 quarter at 60 = 1s -> 3s.
	if !near(n[1].Start, 3.0) || !near(n[1].End, 4.0) || n[1].Lyric != "か" || n[1].Pitch != 62 ||
		!near(n[1].Bar, 1+1.0/3) || n[1].GroupName != "Chorus" {
		t.Errorf("note1 = %+v", n[1])
	}
	if n[2].Lyric != "-" || !near(n[2].End, 4.5) {
		t.Errorf("note2 = %+v", n[2])
	}
}

func TestParseUSTXOldTempo(t *testing.T) {
	p, err := ParseUSTX([]byte(`resolution: 480
bpm: 60
beat_per_bar: 3
beat_unit: 4
tracks: [{singer: x}]
voice_parts:
- {track_no: 0, position: 0, notes: [{position: 1440, duration: 480, tone: 60, lyric: R}, {position: 1920, duration: 480, tone: 60, lyric: la}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	n := p.Tracks[0].Notes
	if len(n) != 2 || n[0].Lyric != "sil" || !near(n[1].Start, 4) || !near(n[1].Bar, 4.0/3) {
		t.Errorf("notes = %+v", n)
	}
}
