package tui

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
)

// writeWAV writes a tiny silent 16-bit mono WAV.
func writeWAV(t *testing.T, path string) {
	t.Helper()
	const n = 4410
	hdr := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00data")
	size := binary.LittleEndian.AppendUint32(nil, n*2)
	if err := os.WriteFile(path, append(append(hdr, size...), make([]byte, n*2)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func typeText(m tea.Model, s string) tea.Model {
	return drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// openEditor parses the 8-bar test song and opens it in the editor.
func openEditor(t *testing.T, dir string) (*App, tea.Model, *Editor) {
	t.Helper()
	writeSVP(t, dir)
	var m tea.Model = NewApp(dir)
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(m, key("enter")) // song.svp → settings
	m = drive(m, key("enter")) // → editor
	app := m.(*App)
	ed, ok := app.screen.(*Editor)
	if !ok {
		t.Fatalf("expected editor, got %T", app.screen)
	}
	return app, m, ed
}

func TestInfoScreen(t *testing.T) {
	dir := t.TempDir()
	app, m, ed := openEditor(t, dir)
	writeWAV(t, filepath.Join(dir, "take2.wav"))

	m = drive(m, key("i"))
	info, ok := app.screen.(*Info)
	if !ok {
		t.Fatalf("expected info screen, got %T", app.screen)
	}
	// Title: replace it.
	m = drive(m, key("enter"))
	m = drive(m, tea.KeyMsg{Type: tea.KeyCtrlU}) // clear the input
	m = typeText(m, "Twinkle")
	m = drive(m, key("enter"))
	// Artist: next field.
	m = drive(m, key("down"))
	m = drive(m, key("enter"))
	m = typeText(m, "Kasane Teto")
	m = drive(m, key("enter"))
	// Audio: browse and pick take2.wav (the picker starts on the first file).
	info.field = info.audioField()
	m = drive(m, key("enter"))
	if info.browser == nil || !strings.Contains(ansi.Strip(app.View()), "take2.wav") {
		t.Fatalf("expected the wav browser:\n%s", ansi.Strip(app.View()))
	}
	m = drive(m, key("enter"))
	if filepath.Base(info.audio) != "take2.wav" {
		t.Fatalf("picked audio = %q", info.audio)
	}
	v := ansi.Strip(app.View())
	for _, want := range []string{"Twinkle", "Kasane Teto", "take2.wav"} {
		if !strings.Contains(v, want) {
			t.Errorf("info view missing %q:\n%s", want, v)
		}
	}

	m = drive(m, key("esc")) // back to the editor, keeping changes
	if app.screen != ed {
		t.Fatalf("expected editor, got %T", app.screen)
	}
	s := ed.song
	if s.Title != "Twinkle" || s.MetaValue("artist") != "Kasane Teto" || s.AudioPath != "take2.wav" || !ed.modified {
		t.Fatalf("song = title %q artist %q audio %q modified %v", s.Title, s.MetaValue("artist"), s.AudioPath, ed.modified)
	}
	// The intro card shows title and artist before the first line (this
	// song starts singing at 0:00, so look a few seconds before that).
	frame := ansi.Strip(render.Frame(s, -4000, 40, 6, render.DefaultTheme))
	if !strings.Contains(frame, "Twinkle") || !strings.Contains(frame, "Kasane Teto") {
		t.Errorf("intro frame:\n%s", frame)
	}

	drive(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	saved, err := lyr.Load(ed.path)
	if err != nil || saved.Title != "Twinkle" || saved.MetaValue("artist") != "Kasane Teto" || saved.AudioPath != "take2.wav" {
		t.Fatalf("saved = %+v, err %v", saved, err)
	}
}

func TestMessagesAndLogView(t *testing.T) {
	app, m, ed := openEditor(t, t.TempDir())
	_ = app
	s := ed.song // 8 words, one per bar (2 s each), 4 bars per line: lines at 0 s and 8 s

	// Paused at 0: m adds a message after the selected (first) line.
	m = drive(m, key("m"))
	m = typeText(m, "a storm rolls in")
	m = drive(m, key("enter"))
	if len(s.Messages) != 1 || s.Messages[0].Text != "a storm rolls in" || ed.msg != 0 {
		t.Fatalf("messages = %+v sel %d", s.Messages, ed.msg)
	}
	at := s.Messages[0].Time
	if at <= s.Lines()[0].End || at >= s.Lines()[1].Start {
		t.Fatalf("message at %d, want between lines (%d..%d)", at, s.Lines()[0].End, s.Lines()[1].Start)
	}
	m = drive(m, key("c")) // red
	m = drive(m, key("b"))
	if s.Messages[0].Color != 0xff5f5f || !s.Messages[0].Bold {
		t.Fatalf("style = %+v", s.Messages[0])
	}
	m = drive(m, key(">"))
	if s.Messages[0].Time != at+100 {
		t.Fatalf("time = %d, want %d", s.Messages[0].Time, at+100)
	}

	// Focus view: the message pops up as a card in the gap.
	t0 := int64(s.Messages[0].Time) + 50
	if f := ansi.Strip(render.Frame(s, t0, 40, 7, render.DefaultTheme)); !strings.Contains(f, "a storm rolls in") || strings.Contains(f, "one") {
		t.Errorf("focus frame should show only the message:\n%s", f)
	}
	// The list shows it between the two lines.
	v := ansi.Strip(app.View())
	if i, j := strings.Index(v, "a storm rolls in"), strings.Index(v, "five six"); i < 0 || j < 0 || i > j {
		t.Errorf("message row should come before line 2:\n%s", v)
	}

	// Log view: lines and the message pile up in order.
	m = drive(m, key("v"))
	if s.Display.View != lyr.ViewLog {
		t.Fatal("v should switch to the log view")
	}
	f := ansi.Strip(render.Frame(s, 9500, 40, 10, render.DefaultTheme))
	a, b, c := strings.Index(f, "one two"), strings.Index(f, "a storm"), strings.Index(f, "five six")
	if a < 0 || b < 0 || c < 0 || !(a < b && b < c) {
		t.Errorf("log frame order wrong:\n%s", f)
	}
	// It scrolls: with 2 rows only the newest entries remain.
	if f := ansi.Strip(render.Frame(s, 9500, 40, 2, render.DefaultTheme)); strings.Contains(f, "one two") || !strings.Contains(f, "five six") {
		t.Errorf("log should scroll:\n%s", f)
	}

	// Delete + undo.
	m = drive(m, key("x"))
	if len(s.Messages) != 0 {
		t.Fatal("x should delete the selected message")
	}
	m = drive(m, key("u"))
	if len(s.Messages) != 1 || ed.msg != 0 {
		t.Fatalf("undo should restore the message: %+v sel %d", s.Messages, ed.msg)
	}

	// A new message cancelled with esc leaves nothing behind (and no undo step).
	undos := len(ed.undo)
	m = drive(m, key("m"))
	m = drive(m, key("esc"))
	if len(s.Messages) != 1 || len(ed.undo) != undos {
		t.Fatalf("cancelled message left state: %d messages, %d undos (want 1, %d)", len(s.Messages), len(ed.undo), undos)
	}

	// Saved and loaded intact.
	drive(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	saved, err := lyr.Load(ed.path)
	if err != nil || len(saved.Messages) != 1 || saved.Messages[0] != s.Messages[0] || saved.Display.View != lyr.ViewLog {
		t.Fatalf("saved = %+v / %+v err %v", saved.Messages, saved.Display, err)
	}
}

func TestEmptyLines(t *testing.T) {
	_, m, ed := openEditor(t, t.TempDir())
	s := ed.song // lines "one two three four" (0–8 s) and "five six seven eight" (8–16 s)

	// Stopped at 0:00 with line 1 selected: two empty lines go after it.
	m = drive(m, key("n"))
	m = drive(m, key("n")) // with the first one selected: right after it
	if len(s.Messages) != 2 || !s.Messages[0].IsSpacer() || !s.Messages[1].IsSpacer() {
		t.Fatalf("messages = %+v", s.Messages)
	}

	// Focus view ignores them: between the lines nothing pops up.
	gap := int64(s.Messages[0].Time) + 10
	if render.ActiveMessage(s, gap) != -1 {
		t.Error("an empty line must not become the focus card")
	}
	// ...and they don't cut a real message short.
	s.AddMessage(lyr.Message{Time: s.Messages[0].Time - 50, Text: "storm", Color: lyr.NoColor})
	if mi := render.ActiveMessage(s, gap); mi < 0 || s.Messages[mi].Text != "storm" {
		t.Errorf("message should stay up past empty lines, got %d", mi)
	}

	// Log view: two blank rows between the message and line 2.
	s.Display.View = lyr.ViewLog
	rows := strings.Split(ansi.Strip(render.Frame(s, 9000, 30, 12, render.DefaultTheme)), "\n")
	var got []string
	for _, r := range rows {
		got = append(got, strings.TrimSpace(r))
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "storm|||five six seven eight") {
		t.Errorf("log rows = %q", got)
	}

	// Editing an empty line and confirming nothing keeps it.
	ed.msg = 1
	m = drive(m, key("e"))
	m = drive(m, key("enter"))
	if len(s.Messages) != 3 {
		t.Fatalf("empty line was removed: %+v", s.Messages)
	}
	_ = m
}

func TestBackspaceAroundEmptyLines(t *testing.T) {
	_, m, ed := openEditor(t, t.TempDir())
	s := ed.song // line 1 "one two three four" (0–8 s), line 2 "five six seven eight" (8–16 s)

	// Put an empty line after line 1.
	m = drive(m, key("n"))
	if len(s.Messages) != 1 {
		t.Fatal("expected one empty line")
	}
	m = drive(m, key("down")) // onto line 2
	if ed.msg != -1 || s.LineOf(ed.cur) != 1 {
		t.Fatalf("expected line 2 selected, got msg %d line %d", ed.msg, s.LineOf(ed.cur)+1)
	}

	// Backspace on line 2 removes the empty line above instead of merging.
	m = drive(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(s.Messages) != 0 || len(s.Lines()) != 2 || s.LineOf(ed.cur) != 1 {
		t.Fatalf("after backspace: %d messages, %d lines, cursor line %d", len(s.Messages), len(s.Lines()), s.LineOf(ed.cur)+1)
	}
	// A second backspace merges the lines as before.
	m = drive(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(s.Lines()) != 1 {
		t.Fatalf("second backspace should merge, got %d lines", len(s.Lines()))
	}
	m = drive(m, key("u"))

	// Backspace below a story message does not merge across it.
	s.AddMessage(lyr.Message{Time: 7000, Text: "storm", Color: lyr.NoColor})
	s.Touch()
	m = drive(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(s.Lines()) != 2 || len(s.Messages) != 1 {
		t.Fatalf("must not merge across a message: %d lines, %d messages", len(s.Lines()), len(s.Messages))
	}
	s.Messages = nil

	// Deleting an empty line keeps the selection there, not on an old word:
	// cursor on line 1, empty line added at the playhead near the end.
	ed.cur, ed.msg = 0, -1
	ed.player.SeekTo(12000)
	m = drive(m, key("n"))
	if ed.msg < 0 {
		t.Fatal("new empty line should be selected")
	}
	m = drive(m, key("x"))
	if ed.msg != -1 || s.LineOf(ed.cur) != 1 {
		t.Fatalf("after deleting, selection should be the row below (line 2), got msg %d line %d", ed.msg, s.LineOf(ed.cur)+1)
	}
	_ = m
}
