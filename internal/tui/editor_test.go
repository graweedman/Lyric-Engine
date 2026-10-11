package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/graweedman/lyric-engine/internal/audio"
	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
	"github.com/graweedman/lyric-engine/internal/svp"
)

func demoSong(t *testing.T) *lyr.Song {
	t.Helper()
	var notes []svp.Note
	at := 1.0
	for _, w := range strings.Fields("one two three four five six") {
		notes = append(notes, svp.Note{Start: at, End: at + 0.4, Lyric: w})
		at += 0.5
	}
	opt := lyr.DefaultImport
	opt.Mode = lyr.ModeRests
	return lyr.FromNotes(notes, opt, nil)
}

func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = m.Update(msg)
	}
	return m
}

func lines(s *lyr.Song) []string {
	var out []string
	for i := range s.Lines() {
		out = append(out, render.LineText(s, i))
	}
	return out
}

func TestEditorArrange(t *testing.T) {
	song := demoSong(t)
	p, _ := audio.NewPlayer(nil, 10000)
	var m tea.Model = NewEditor(song, "x.lyrb", p, "")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m = press(m, "right", "right", "right", "enter") // break before "four"
	if got := lines(song); strings.Join(got, "|") != "one two three|four five six" {
		t.Fatalf("after break: %q", got)
	}
	m = press(m, "right", "J") // glue "five" to "four"
	if got := lines(song); got[1] != "fourfive six" {
		t.Fatalf("after join: %q", got)
	}
	m = press(m, "s") // stanza at line 2
	if !song.Lines()[1].Stanza {
		t.Fatal("expected stanza")
	}
	m = press(m, "u", "u") // undo stanza and join
	if got := lines(song); got[1] != "four five six" || song.Lines()[1].Stanza {
		t.Fatalf("after undo: %q", got)
	}
	m = press(m, "backspace") // merge back
	if got := lines(song); len(got) != 1 {
		t.Fatalf("after merge: %q", got)
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "one two three four five six") {
		t.Errorf("view missing line:\n%s", view)
	}
	for i, l := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("row %d is %d cells wide: %q", i, w, l)
		}
	}
	if os.Getenv("SHOW_VIEW") != "" {
		t.Log("\n" + view)
	}
}

func TestFrameKaraoke(t *testing.T) {
	song := demoSong(t)
	// t=1.2s: "one" (1.0–1.4) half sung, line is current.
	st := render.Resolve(song, 1200)
	if st.Current != 0 {
		t.Fatalf("current = %d", st.Current)
	}
	out := ansi.Strip(render.Frame(song, 1200, 40, 5, render.DefaultTheme))
	if !strings.Contains(out, "one two three four five six") {
		t.Errorf("frame:\n%s", out)
	}
	// Long after the end: cleared.
	if st := render.Resolve(song, 60000); st.Current != -1 {
		t.Errorf("expected cleared screen, got current %d", st.Current)
	}
}
