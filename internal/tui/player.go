package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/audio"
	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
)

// Player is the final full-screen lyric display.
type Player struct {
	song       *lyr.Song
	player     *audio.Player
	showStatus bool
	note       string // warning shown on the bottom line for a few seconds
	noteUntil  time.Time
	started    bool
	w, h       int
	aw, ah     int    // shape of the display strip (width:height)
	dock       string // where the strip sits: left, right or center

	// OnEnd, if set, runs once when playback ends or is quit, while the
	// display is still on screen.
	OnEnd func()
}

func (m *Player) quit() (tea.Model, tea.Cmd) {
	if m.OnEnd != nil {
		m.OnEnd()
		m.OnEnd = nil
	}
	return m, tea.Quit
}

// NewPlayer creates the display; it starts playing immediately. note is a
// warning (e.g. no audio) shown briefly on the bottom line. The display is
// drawn in an aw:ah strip docked "left", "right" or "center", so a wider
// window keeps the rest (the cover art's square) empty.
func NewPlayer(song *lyr.Song, p *audio.Player, showStatus bool, note string, aw, ah int, dock string) *Player {
	return &Player{song: song, player: p, showStatus: showStatus, note: note, noteUntil: time.Now().Add(5 * time.Second),
		aw: aw, ah: ah, dock: dock}
}

// stripWidth is the width of the aw:ah display strip in a window of the
// current size (terminal cells are about twice as tall as wide).
func (m *Player) stripWidth() int {
	if m.aw <= 0 || m.ah <= 0 {
		return m.w
	}
	return max(1, min(m.w, m.h*2*m.aw/m.ah))
}

func (m *Player) Init() tea.Cmd { return tick() }

func (m *Player) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		if !m.started {
			m.started = true
			m.player.Toggle()
		} else if !m.player.Playing() && m.player.Position() >= m.player.DurationMs() {
			return m.quit()
		}
		return m, tick()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m.quit()
		case " ":
			m.player.Toggle()
		case "a": // for this playback only; the editor saves it
			m.song.Display.HUD = !m.song.Display.HUD
		case "left":
			m.player.SeekTo(m.player.Position() - 5000)
		case "right":
			m.player.SeekTo(m.player.Position() + 5000)
		}
	}
	return m, nil
}

func (m *Player) View() string {
	if m.w == 0 {
		return ""
	}
	w := m.stripWidth()
	var bottom string
	switch {
	case m.note != "" && time.Now().Before(m.noteUntil):
		bottom = truncate(m.note, w)
	case m.showStatus:
		icon := "▶"
		if !m.player.Playing() {
			icon = "⏸"
		}
		bottom = fmt.Sprintf("%s %s / %s", icon, clock(m.player.Position()), clock(m.player.DurationMs()))
	}
	h := m.h
	if m.showStatus || bottom != "" {
		h--
	}
	t := m.player.Position() - int64(m.song.AudioOffsetMs)
	out := render.Frame(m.song, t, w, max(1, h), render.DefaultTheme)
	if m.showStatus || bottom != "" {
		out += "\n" + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Foreground(lipgloss.Color("#4e4e4e")).Render(bottom)
	}
	return out
}
