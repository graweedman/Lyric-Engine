package tui

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/graweedman/lyric-engine/internal/audio"
	"github.com/graweedman/lyric-engine/internal/lyr"
)

// App is the interactive program: file picker → parse settings → editor.
// It owns the frame tick and routes navigation messages between screens.
type App struct {
	screen tea.Model
	picker *Picker
	editor *Editor // current editor, kept while re-parsing
	w, h   int
	err    string
}

// NewApp starts on the file picker in dir.
func NewApp(dir string) *App {
	a := &App{picker: NewPicker(dir)}
	a.screen = a.picker
	return a
}

// NewAppWith opens path directly (.svp → settings, .lyrb → editor). The
// picker starts in the file's folder. wav optionally overrides the audio.
func NewAppWith(path, wav string) (*App, error) {
	a := NewApp(filepath.Dir(path))
	if err := a.open(path, wav); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) Init() tea.Cmd { return tick() }

// show switches screens and sizes the new one.
func (a *App) show(m tea.Model) {
	a.screen = m
	if a.w > 0 {
		a.screen, _ = a.screen.Update(tea.WindowSizeMsg{Width: a.w, Height: a.h})
	}
}

// open routes a file to the right screen.
func (a *App) open(path, wav string) error {
	if strings.EqualFold(filepath.Ext(path), ".svp") {
		cfg, err := NewConfig(path, lyr.LyrbPath(path), lyr.DefaultImport, wav, false)
		if err != nil {
			return err
		}
		a.show(cfg)
		return nil
	}
	song, err := lyr.Open(path)
	if err != nil {
		return err
	}
	if wav != "" {
		song.AudioPath = lyr.RelTo(path, wav)
	}
	a.startEditor(song, path, "")
	return nil
}

func (a *App) startEditor(song *lyr.Song, path, note string) {
	p, warn := audio.Open(lyr.Resolve(path, song.AudioPath), int64(song.Duration())+2000)
	if warn != "" {
		note = warn
	}
	a.editor = NewEditor(song, path, p, note)
	a.show(a.editor)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
	case tickMsg:
		a.screen.Update(msg) // the app keeps the single tick loop
		return a, tick()

	case openFileMsg:
		if err := a.open(msg.path, ""); err != nil {
			a.picker.err = err.Error()
		}
		return a, nil
	case openPickerMsg:
		a.picker.chdir(a.picker.dir, "") // refresh ("has .lyrb" markers)
		a.show(a.picker)
		return a, nil
	case backMsg:
		if a.editorIsReparsing() {
			a.show(a.editor)
		} else {
			a.show(a.picker)
		}
		return a, nil
	case reparseMsg:
		e := msg.e
		svpPath := lyr.Resolve(e.path, e.song.SourcePath)
		cfg, err := NewConfig(svpPath, e.path, e.song.Import, lyr.Resolve(e.path, e.song.AudioPath), true)
		if err != nil {
			e.flash("re-parse: " + err.Error())
			return a, nil
		}
		a.show(cfg)
		return a, nil
	case infoMsg:
		a.show(NewInfo(msg.e.song, msg.e.path))
		return a, nil
	case infoDoneMsg:
		e := a.editor
		if e.ApplyInfo(msg.title, msg.meta) {
			e.flash("song info updated — ctrl+s to save")
		}
		if cur := lyr.Resolve(e.path, e.song.AudioPath); (cur == "") != (msg.audio == "") || !sameFile(cur, msg.audio) {
			p, warn := audio.Open(msg.audio, int64(e.song.Duration())+2000)
			stored, note := "", "audio removed — ctrl+s to save"
			if msg.audio != "" {
				stored, note = lyr.RelTo(e.path, msg.audio), "audio: "+filepath.Base(msg.audio)+" — ctrl+s to save"
				if warn != "" {
					note = warn
				}
			}
			e.SetPlayer(p, stored)
			e.flash(note)
		}
		a.show(e)
		return a, nil
	case importDoneMsg:
		if cfg, ok := a.screen.(*Config); ok && cfg.reparse && a.editor != nil {
			e := a.editor
			e.Replace(msg.song)
			if msg.song.AudioPath != e.song.AudioPath {
				p, warn := audio.Open(msg.wavPath, int64(e.song.Duration())+2000)
				e.SetPlayer(p, msg.song.AudioPath)
				if warn != "" {
					e.flash(warn)
				}
			}
			a.show(e)
			return a, nil
		}
		note := "parsed " + filepath.Base(msg.svpPath) + " — ctrl+s saves to " + filepath.Base(msg.lyrbPath)
		a.startEditor(msg.song, msg.lyrbPath, note)
		return a, nil
	}

	var cmd tea.Cmd
	a.screen, cmd = a.screen.Update(msg)
	return a, cmd
}

// editorIsReparsing reports whether the settings screen was opened from the editor.
func (a *App) editorIsReparsing() bool {
	cfg, ok := a.screen.(*Config)
	return ok && cfg.reparse
}

func (a *App) View() string { return a.screen.View() }
