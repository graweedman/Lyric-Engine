package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/graweedman/lyric-engine/internal/lyr"
)

func TestBackingBrackets(t *testing.T) {
	s := &lyr.Song{Display: lyr.DefaultDisplay, Tokens: []lyr.Token{
		{Start: 0, End: 500, Text: "I", Break: lyr.BreakStanza},
		{Start: 500, End: 1000, Text: "love"},
		{Start: 600, End: 900, Text: "oh", Backing: true},
		{Start: 1000, End: 1500, Text: "you"},
		{Start: 1600, End: 1800, Text: "love", Backing: true},
		{Start: 1800, End: 1900, Text: "hidden", Backing: true, Hidden: true},
		{Start: 1900, End: 2200, Text: "you", Backing: true},
	}}
	s.Touch()
	const want = "I love (oh) you (love you)"
	if got := LineText(s, 0); got != want {
		t.Fatalf("LineText = %q, want %q", got, want)
	}
	// The karaoke line shows the same text at any moment.
	for _, at := range []int64{0, 700, 1700, 3000} {
		if got := ansi.Strip(currentLine(s, 0, at, DefaultTheme)); got != want {
			t.Errorf("at %d: currentLine = %q", at, got)
		}
	}
	if !strings.Contains(ansi.Strip(Frame(s, 700, 40, 5, DefaultTheme)), want) {
		t.Error("frame should show the bracketed line")
	}
}

func TestSongColors(t *testing.T) {
	s := &lyr.Song{Display: lyr.DefaultDisplay}
	if th := DefaultTheme.ForSong(s); th.Sung.GetForeground() != DefaultTheme.Sung.GetForeground() {
		t.Error("no song color should keep the theme")
	}
	s.Display.LeadColor, s.Display.BackColor = 0xff5f5f, 0x87d75f
	th := DefaultTheme.ForSong(s)
	if th.Sung.GetForeground() != lipgloss.Color("#ff5f5f") || th.BackSung.GetForeground() != lipgloss.Color("#87d75f") ||
		th.Past.GetForeground() != lipgloss.Color("#993939") {
		t.Errorf("theme = %v %v %v", th.Sung.GetForeground(), th.BackSung.GetForeground(), th.Past.GetForeground())
	}
}

func TestIntroCredits(t *testing.T) {
	s := &lyr.Song{Display: lyr.DefaultDisplay, Title: "Song"}
	s.SetMeta("artist", "Band")
	s.SetMeta("vocal", "Teto")
	s.SetMeta("music", "Ann")
	s.SetMeta("lyrics", "Ann")
	var got []string
	for _, r := range Intro(s, DefaultTheme) {
		got = append(got, ansi.Strip(r))
	}
	if want := "Song|Band|Vocal: Teto|Music & lyrics: Ann"; strings.Join(got, "|") != want {
		t.Errorf("intro = %q, want %q", strings.Join(got, "|"), want)
	}
	s.SetMeta("lyrics", "Bob")
	if c := strings.Join(Credits(s), "|"); c != "Vocal: Teto|Music: Ann|Lyrics: Bob" {
		t.Errorf("credits = %q", c)
	}
}

func TestPlainText(t *testing.T) {
	s := &lyr.Song{Tokens: []lyr.Token{
		{Start: 1000, End: 1500, Text: "twin", Break: lyr.BreakStanza},
		{Start: 1500, End: 2000, Text: "kle", Join: true},
		{Start: 2000, End: 2500, Text: "star"},
		{Start: 2100, End: 2400, Text: "ooh", Backing: true},
		{Start: 3000, End: 3500, Text: "how", Break: lyr.BreakLine},
		{Start: 4000, End: 4200, Text: "x", Break: lyr.BreakLine, Hidden: true},
		{Start: 65430, End: 66000, Text: "up", Break: lyr.BreakStanza},
	}}
	want := "twinkle star (ooh)\nhow\n\nup\n"
	if got := PlainText(s, false); got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
	want = "[00:01.00] twinkle star (ooh)\n[00:03.00] how\n\n[01:05.43] up\n"
	if got := PlainText(s, true); got != want {
		t.Errorf("PlainText timestamps = %q, want %q", got, want)
	}
}
