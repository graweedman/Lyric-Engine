package lyr

import (
	"reflect"
	"strings"
	"testing"

	"github.com/graweedman/lyric-engine/internal/svp"
)

func notes(spec ...any) []svp.Note {
	var out []svp.Note
	for i := 0; i < len(spec); i += 3 {
		out = append(out, svp.Note{Start: spec[i].(float64), End: spec[i+1].(float64), Lyric: spec[i+2].(string)})
	}
	return out
}

// rests is the rest-based mode with line splitting, as used by most tests.
var rests = func() ImportOptions {
	o := DefaultImport
	o.Mode, o.MaxChars = ModeRests, 40
	return o
}()

func TestBarsMode(t *testing.T) {
	// 4/4; words on beats. Group 2 starts at bar 9.5 (not on the 4-bar grid
	// from bar 0), and "so" is a pickup half a beat before bar 4.
	type w struct {
		bar   float64
		text  string
		group int
	}
	words := []w{
		{0, "a", 1}, {1, "b", 1}, {3.75, "c", 1}, {3.875, "so", 1}, {4, "d", 1}, {7.5, "e", 1},
		{9.5, "x", 2}, {13.2, "y", 2}, {13.6, "z", 2},
	}
	var ns []svp.Note
	for _, x := range words {
		sec := x.bar * 2 // 120 bpm 4/4: 2 s per bar
		ns = append(ns, svp.Note{Start: sec, End: sec + 0.2, Bar: x.bar, Lyric: x.text, Group: x.group})
	}
	opt := DefaultImport
	opt.StanzaGap = 1e9 // isolate the bar grid from gap-based stanzas
	s := FromNotes(ns, opt, nil)
	var got []string
	for i := range s.Lines() {
		got = append(got, lineText(s, i))
	}
	// "so" at bar 3.875 + 0.5 beat (0.125 bar) = 4.0 -> next line.
	want := []string{"a b c", "so d e", "x", "y z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if !s.Lines()[2].Stanza {
		t.Error("group change should start a stanza")
	}
}

func lineText(s *Song, i int) string {
	l := s.Lines()[i]
	var words []string
	for _, tk := range s.Tokens[l.First : l.First+l.Count] {
		words = append(words, tk.Text)
	}
	return strings.Join(words, " ")
}

func TestFromNotesMergesAndBreaks(t *testing.T) {
	s := FromNotes(notes(
		0.0, 0.5, "twinkle",
		0.5, 1.0, "+",
		1.0, 1.5, "star",
		1.5, 2.5, "-",
		2.5, 2.7, "br",
		2.7, 3.0, "how", // after a breath -> line
		3.0, 3.5, "I",
		4.2, 4.5, "wonder", // 0.7s gap -> line
		11.0, 11.5, "up", // gap > 6s -> stanza
	), rests, nil)

	var got []string
	for _, tk := range s.Tokens {
		got = append(got, tk.Text)
	}
	if want := []string{"twinkle", "star", "how", "I", "wonder", "up"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	if s.Tokens[0].End != 1000 || s.Tokens[1].End != 2500 {
		t.Errorf("merge ends = %d, %d", s.Tokens[0].End, s.Tokens[1].End)
	}
	var b []Break
	for _, tk := range s.Tokens[1:] {
		b = append(b, tk.Break)
	}
	if want := []Break{BreakNone, BreakLine, BreakNone, BreakLine, BreakStanza}; !reflect.DeepEqual(b, want) {
		t.Errorf("breaks = %v, want %v", b, want)
	}
	ls := s.Lines()
	if len(ls) != 4 || ls[3].Stanzanum != 1 || !ls[3].Stanza || ls[0].End != 2500 {
		t.Errorf("lines = %+v", ls)
	}
}

func TestGroupStanzaAndBalancedSplit(t *testing.T) {
	var ns []svp.Note
	at := 0.0
	for i, w := range strings.Fields("no one will ever change this animal i have become help me") {
		g := 1
		if i >= 10 {
			g = 2 // "help me" is in the next note group (e.g. Chorus)
		}
		ns = append(ns, svp.Note{Start: at, End: at + 0.3, Lyric: w, Group: g})
		at += 0.3
	}
	s := FromNotes(ns, rests, nil)
	var got []string
	for i := range s.Lines() {
		got = append(got, lineText(s, i))
	}
	want := []string{"no one will ever change", "this animal i have become", "help me"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if !s.Lines()[2].Stanza || s.Lines()[1].Stanza {
		t.Errorf("stanza flags wrong: %+v", s.Lines())
	}
}

func TestCodecRoundTrip(t *testing.T) {
	s := FromNotes(notes(0.0, 0.4, "夜", 0.4, 0.8, "空", 2.0, 2.5, "hello", 2.5, 3.0, "world"), rests, nil)
	s.Title, s.AudioPath, s.SourcePath = "Song", "song.wav", "song.svp"
	s.AudioOffsetMs = -35
	s.Display = Display{Before: 2, After: 0, LeadIn: 250, LeadColor: 0xff5f5f, BackColor: 0x87d75f}
	s.Tokens[3].Hidden = true
	s.Tokens[2].Backing = true
	s.Display.View = ViewLog
	s.AddMessage(Message{Time: 9000, Text: "and then…", Color: NoColor})
	s.AddMessage(Message{Time: 1500, Text: "the night falls", Color: 0x5f87ff, Bold: true, Italic: true})
	s.SetMeta("artist", "Kasane Teto")
	s.SetMeta("year", "2025")
	s.SetMeta("custom-key", "kept as is")
	s.SetMeta("year", "") // removing works
	s.Import = ImportOptions{Track: 2, Mode: ModeBars, BarsPerLine: 8, Pickup: 0.25, LineGap: 0.5, StanzaGap: 3, GroupStanza: true, MaxChars: 30, BackingTrack: 4}
	s.Touch()

	data := Encode(s)
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tokens[1].Join {
		t.Error("CJK tokens should be joined")
	}
	if !reflect.DeepEqual(got.Tokens, s.Tokens) {
		t.Errorf("tokens:\n got %+v\nwant %+v", got.Tokens, s.Tokens)
	}
	if !reflect.DeepEqual(got.Lines(), s.Lines()) {
		t.Errorf("lines:\n got %+v\nwant %+v", got.Lines(), s.Lines())
	}
	if got.Title != s.Title || got.AudioPath != s.AudioPath || got.SourcePath != s.SourcePath ||
		got.AudioOffsetMs != s.AudioOffsetMs || got.Display != s.Display || got.Import != s.Import ||
		!reflect.DeepEqual(got.Meta, []MetaField{{"artist", "Kasane Teto"}, {"custom-key", "kept as is"}}) ||
		!reflect.DeepEqual(got.Messages, s.Messages) || got.Messages[0].Text != "the night falls" {
		t.Errorf("header mismatch: %+v", got)
	}

	data[len(data)/2] ^= 0xFF
	if _, err := Decode(data); err == nil {
		t.Error("corrupted file should fail checksum")
	}
}

func TestSyllableTiming(t *testing.T) {
	// From the song: "inside" (0.738s) "+" (0.246s) "-" "-" (held 1.23s),
	// then "some" "+" with a rest between the two notes.
	s := FromNotes(notes(
		0.000, 0.738, "inside",
		0.738, 0.984, "+",
		0.984, 1.230, "-",
		1.230, 2.214, "-",
		3.000, 3.200, "somebody",
		3.500, 3.700, "+",
	), rests, nil)
	in := s.Tokens[0]
	if want := []Seg{{0, 738}, {738, 2214}}; !reflect.DeepEqual(in.Segs, want) {
		t.Fatalf("inside segs = %v, want %v", in.Segs, want)
	}
	cases := []struct {
		tok  int
		at   int64
		want int
	}{
		{0, 0, 0},
		{0, 369, 1},  // halfway through "in": 1.5 of 3 runes
		{0, 738, 3},  // "ins" done exactly when the first note ends
		{0, 1476, 4}, // the held syllable advances over its whole length
		{0, 2214, 6},
		{1, 3100, 2}, // halfway through the first of two notes (8 runes)
		{1, 3350, 4}, // in the rest: holds at the syllable boundary
		{1, 3600, 6}, // halfway through the second note
		{1, 9999, 8},
	}
	for _, c := range cases {
		if got := s.Tokens[c.tok].SungRunes(c.at, len([]rune(s.Tokens[c.tok].Text))); got != c.want {
			t.Errorf("token %d at %dms: %d runes, want %d", c.tok, c.at, got, c.want)
		}
	}

	// Segments survive the file format; single-note words stay nil.
	s.Tokens = append(s.Tokens, Token{Start: 4000, End: 4500, Text: "x"})
	s.Touch()
	got, err := Decode(Encode(s))
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Tokens {
		if !reflect.DeepEqual(got.Tokens[i].Segs, s.Tokens[i].Segs) {
			t.Errorf("token %d segs = %v, want %v", i, got.Tokens[i].Segs, s.Tokens[i].Segs)
		}
	}
}

func TestAddBacking(t *testing.T) {
	// Lead: two lines. Backing (phrases at rests): an echo in the gap just
	// after line 1, a double of "come" (dropped), a word in the gap just
	// before line 2, and one overlapping "home" with other words.
	s := FromNotes(notes(0.0, 0.5, "I", 0.5, 1.0, "love", 1.0, 1.5, "you", 4.0, 4.5, "come", 4.5, 5.0, "home"), rests, nil)
	back := FromNotes(notes(1.6, 1.9, "love", 1.9, 2.2, "you", 3.6, 3.8, "oh", 4.0, 4.5, "Come", 4.7, 4.9, "sweet"), rests, nil).Tokens
	s.AddBacking(back)
	var got []string
	for i := range s.Lines() {
		l := s.Lines()[i]
		var ws []string
		for _, tk := range s.Tokens[l.First : l.First+l.Count] {
			if tk.Backing {
				tk.Text = "(" + tk.Text + ")"
			}
			ws = append(ws, tk.Text)
		}
		got = append(got, strings.Join(ws, " "))
	}
	want := []string{"I love you (love) (you)", "(oh) come home (sweet)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if s.Tokens[0].Break != BreakStanza || s.Tokens[5].Break != BreakLine || s.Tokens[6].Break != BreakNone {
		t.Errorf("breaks: the line break should move to the first token of each line: %+v", s.Tokens)
	}
	if l := s.Lines()[1]; l.Start != 3600 {
		t.Errorf("line 2 starts at %d, want 3600 (the backing word)", l.Start)
	}

	// A phrase stays together on the line it starts in, even when it runs
	// past the next lead word.
	s = FromNotes(notes(0.0, 0.5, "a", 0.5, 1.0, "b", 1.0, 1.5, "c"), rests, nil)
	s.AddBacking(FromNotes(notes(0.2, 0.6, "x", 0.6, 1.2, "y"), rests, nil).Tokens)
	var order []string
	for _, tk := range s.Tokens {
		order = append(order, tk.Text)
	}
	if want := []string{"a", "x", "y", "b", "c"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %q, want %q", order, want)
	}
}

func TestVoiceColor(t *testing.T) {
	cases := map[string]uint32{
		"Kasane Teto": 0xff5f5f, "重音テト": 0xff5f5f, "GUMI AI 2": 0x87d75f, "gumi (English)": 0x87d75f,
		"Natsuki Karin": 0, "Solaria": 0, "": 0,
	}
	for voice, want := range cases {
		if got := VoiceColor(voice); got != want {
			t.Errorf("VoiceColor(%q) = %06x, want %06x", voice, got, want)
		}
	}
}
