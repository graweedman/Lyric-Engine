package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/svp"
)

// writeSVP writes a 4/4, 120 bpm project with one word per bar for 8 bars.
func writeSVP(t *testing.T, dir string) string {
	t.Helper()
	q := int64(svp.BlicksPerQuarter)
	var notes []string
	for i, w := range strings.Fields("one two three four five six seven eight") {
		notes = append(notes, fmt.Sprintf(`{"onset": %d, "duration": %d, "lyrics": %q, "pitch": 60}`, int64(i)*4*q, 2*q, w))
	}
	src := `{"time": {"meter": [{"index": 0, "numerator": 4, "denominator": 4}], "tempo": [{"position": 0, "bpm": 120}]},
	  "library": [], "tracks": [{"name": "Vox", "mainGroup": {"notes": [` + strings.Join(notes, ",") + `]}, "mainRef": {}, "groups": []}]}`
	path := filepath.Join(dir, "song.svp")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// drive sends a message and runs any resulting commands (one level deep,
// which is all the app's navigation messages need).
func drive(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	if cmd != nil {
		if next := cmd(); next != nil {
			if _, isTick := next.(tickMsg); !isTick {
				m = drive(m, next)
			}
		}
	}
	return m
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestAppFlow(t *testing.T) {
	dir := t.TempDir()
	writeSVP(t, dir)

	var m tea.Model = NewApp(dir)
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	app := m.(*App)

	// Picker lists the .svp; ".." comes first.
	if v := ansi.Strip(app.View()); !strings.Contains(v, "song.svp") {
		t.Fatalf("picker:\n%s", v)
	}
	m = drive(m, key("down"))
	m = drive(m, key("enter"))
	cfg, ok := app.screen.(*Config)
	if !ok {
		t.Fatalf("expected settings screen, got %T", app.screen)
	}
	// Default: 4 bars per line -> 2 lines.
	if n := len(cfg.preview.Lines()); n != 2 {
		t.Fatalf("default preview lines = %d", n)
	}
	// Select "Bars per line" (from the button: down wraps to Track, then
	// Backing track, Line mode, then Bars per line) and lower it to 2.
	m = drive(m, key("down"))
	m = drive(m, key("down"))
	m = drive(m, key("down"))
	m = drive(m, key("down"))
	m = drive(m, key("left"))
	m = drive(m, key("left"))
	if cfg.opt.BarsPerLine != 2 || len(cfg.preview.Lines()) != 4 {
		t.Fatalf("bars=%d lines=%d", cfg.opt.BarsPerLine, len(cfg.preview.Lines()))
	}
	v := ansi.Strip(app.View())
	if !strings.Contains(v, "five six") || !strings.Contains(v, "Bars per line") {
		t.Fatalf("settings view:\n%s", v)
	}

	m = drive(m, key("enter"))
	ed, ok := app.screen.(*Editor)
	if !ok {
		t.Fatalf("expected editor, got %T", app.screen)
	}
	if ed.song.Import.BarsPerLine != 2 || ed.path != filepath.Join(dir, "song.lyrb") {
		t.Fatalf("editor song import=%+v path=%s", ed.song.Import, ed.path)
	}

	// Save, then re-parse with 1 bar per line from the editor.
	m = drive(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	saved, err := lyr.Load(ed.path)
	if err != nil || saved.Import.BarsPerLine != 2 {
		t.Fatalf("saved import=%+v err=%v", saved.Import, err)
	}
	m = drive(m, key("R"))
	cfg, ok = app.screen.(*Config)
	if !ok || !cfg.reparse || cfg.opt.BarsPerLine != 2 {
		t.Fatalf("expected re-parse settings with stored options, got %T", app.screen)
	}
	cfg.field = 2 // Bars per line
	m = drive(m, key("left"))
	m = drive(m, key("enter"))
	if app.screen != ed {
		t.Fatalf("expected to return to the same editor, got %T", app.screen)
	}
	if n := len(ed.song.Lines()); n != 8 {
		t.Fatalf("after re-parse lines = %d", n)
	}
	m = drive(m, key("u"))
	if n := len(ed.song.Lines()); n != 4 || ed.song.Import.BarsPerLine != 2 {
		t.Fatalf("after undo lines = %d import=%+v", n, ed.song.Import)
	}

	// esc from a re-parse returns to the editor; o returns to the picker
	// (twice, because there are unsaved changes).
	m = drive(m, key("R"))
	m = drive(m, key("esc"))
	if app.screen != ed {
		t.Fatalf("esc should return to the editor, got %T", app.screen)
	}
	m = drive(m, key("o"))
	if app.screen != ed {
		t.Fatal("first o should only warn about unsaved changes")
	}
	m = drive(m, key("o"))
	if _, ok := app.screen.(*Picker); !ok {
		t.Fatalf("expected picker, got %T", app.screen)
	}
	if v := ansi.Strip(app.View()); !strings.Contains(v, "has .lyrb") {
		t.Errorf("picker should mark the saved song:\n%s", v)
	}
	for i, l := range strings.Split(ansi.Strip(app.View()), "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("row %d too wide (%d)", i, w)
		}
	}
}
