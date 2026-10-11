package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
	"github.com/graweedman/lyric-engine/internal/svp"
)

// importDoneMsg carries a freshly parsed song to the app.
type importDoneMsg struct {
	song              *lyr.Song
	svpPath, lyrbPath string
	wavPath           string
}

// backMsg leaves the current screen.
type backMsg struct{}

type setting struct {
	label   string
	value   func(c *Config) string
	change  func(c *Config, dir int)
	enabled func(c *Config) bool
}

func always(*Config) bool { return true }

func step(v *float64, dir int, by, lo, hi float64) {
	*v = max(lo, min(hi, *v+float64(dir)*by))
}

var settings = []setting{
	{"Track", func(c *Config) string {
		if c.opt.Track < 0 {
			if i := c.proj.BestTrack(); i >= 0 {
				return "auto: " + c.trackLabel(i)
			}
			return "auto"
		}
		return c.trackLabel(c.opt.Track)
	}, func(c *Config, dir int) {
		c.opt.Track = max(-1, min(len(c.proj.Tracks)-1, c.opt.Track+dir))
	}, always},
	{"Backing track", func(c *Config) string {
		if c.opt.BackingTrack == 0 {
			return "off"
		}
		return c.trackLabel(c.opt.BackingTrack-1) + " in ( )"
	}, func(c *Config, dir int) {
		c.opt.BackingTrack = (c.opt.BackingTrack + dir + len(c.proj.Tracks) + 1) % (len(c.proj.Tracks) + 1)
	}, always},
	{"Line mode", func(c *Config) string {
		if c.opt.Mode == lyr.ModeRests {
			return "at rests / breaths"
		}
		return "every N bars"
	}, func(c *Config, dir int) {
		c.opt.Mode = 1 - c.opt.Mode
	}, always},
	{"Bars per line", func(c *Config) string {
		return fmt.Sprint(c.opt.BarsPerLine)
	}, func(c *Config, dir int) {
		c.opt.BarsPerLine = max(1, min(32, c.opt.BarsPerLine+dir))
	}, func(c *Config) bool { return c.opt.Mode == lyr.ModeBars }},
	{"Pickup", func(c *Config) string {
		return fmt.Sprintf("%.2f beats", c.opt.Pickup)
	}, func(c *Config, dir int) {
		step(&c.opt.Pickup, dir, 0.25, 0, 4)
	}, func(c *Config) bool { return c.opt.Mode == lyr.ModeBars }},
	{"Rest gap", func(c *Config) string {
		return fmt.Sprintf("%.2f s", c.opt.LineGap)
	}, func(c *Config, dir int) {
		step(&c.opt.LineGap, dir, 0.05, 0.05, 5)
	}, func(c *Config) bool { return c.opt.Mode == lyr.ModeRests }},
	{"Stanza per group", func(c *Config) string {
		return onOff(c.opt.GroupStanza)
	}, func(c *Config, dir int) {
		c.opt.GroupStanza = !c.opt.GroupStanza
	}, always},
	{"Stanza gap", func(c *Config) string {
		if c.opt.StanzaGap <= 0 {
			return "off"
		}
		return fmt.Sprintf("%.1f s", c.opt.StanzaGap)
	}, func(c *Config, dir int) {
		step(&c.opt.StanzaGap, dir, 0.5, 0, 60)
	}, always},
	{"Max chars", func(c *Config) string {
		if c.opt.MaxChars == 0 {
			return "off"
		}
		return fmt.Sprint(c.opt.MaxChars)
	}, func(c *Config, dir int) {
		v := c.opt.MaxChars + dir*2
		if c.opt.MaxChars == 0 && dir > 0 {
			v = 20
		}
		if v < 20 {
			v = 0
		}
		c.opt.MaxChars = min(v, 200)
	}, always},
	{"Audio", func(c *Config) string {
		if c.wavs[c.wavIdx] == "" {
			return "none"
		}
		return filepath.Base(c.wavs[c.wavIdx])
	}, func(c *Config, dir int) {
		c.wavIdx = (c.wavIdx + dir + len(c.wavs)) % len(c.wavs)
	}, always},
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Config is the parse-settings screen with a live preview of the lines.
type Config struct {
	svpPath, lyrbPath string
	proj              *svp.Project
	opt               lyr.ImportOptions
	wavs              []string // "" = no audio
	wavIdx            int
	reparse           bool // re-parsing from the editor (vs a fresh import)

	field   int // len(settings) = the "open" button
	preview *lyr.Song
	perr    string
	ptop    int
	w, h    int
}

// NewConfig parses svpPath and opens the settings screen. wav preselects
// an audio file ("" = guess <name>.wav).
func NewConfig(svpPath, lyrbPath string, opt lyr.ImportOptions, wav string, reparse bool) (*Config, error) {
	proj, err := svp.ParseFile(svpPath)
	if err != nil {
		return nil, err
	}
	if opt.Track >= len(proj.Tracks) {
		opt.Track = -1
	}
	if opt.BackingTrack > len(proj.Tracks) {
		opt.BackingTrack = 0
	}
	c := &Config{svpPath: svpPath, lyrbPath: lyrbPath, proj: proj, opt: opt, reparse: reparse}
	if wav == "" && !reparse {
		wav = lyr.GuessAudio(svpPath)
	}
	c.wavs = append([]string{""}, wavFiles(filepath.Dir(svpPath))...)
	if wav != "" {
		found := false
		for i, w := range c.wavs {
			if sameFile(w, wav) {
				c.wavIdx, found = i, true
			}
		}
		if !found {
			c.wavs = append(c.wavs, wav)
			c.wavIdx = len(c.wavs) - 1
		}
	}
	c.field = len(settings)
	c.rebuild()
	return c, nil
}

func wavFiles(dir string) []string {
	items, _ := os.ReadDir(dir)
	var out []string
	for _, it := range items {
		if !it.IsDir() && strings.EqualFold(filepath.Ext(it.Name()), ".wav") {
			out = append(out, filepath.Join(dir, it.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func sameFile(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(aa, bb)
}

func (c *Config) trackLabel(i int) string {
	t := c.proj.Tracks[i]
	if t.Voice != "" {
		return fmt.Sprintf("#%d %s · %s (%d)", i, t.Name, t.Voice, len(t.Notes))
	}
	return fmt.Sprintf("#%d %s (%d)", i, t.Name, len(t.Notes))
}

func (c *Config) rebuild() {
	c.preview, c.perr = nil, ""
	s, err := lyr.Import(c.proj, c.opt)
	if err != nil {
		c.perr = err.Error()
		return
	}
	c.preview = s
	c.ptop = min(c.ptop, max(0, len(s.Lines())-1))
}

func (c *Config) Init() tea.Cmd { return nil }

func (c *Config) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.w, c.h = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return c, tea.Quit
		case "esc", "q":
			return c, func() tea.Msg { return backMsg{} }
		case "up", "k":
			c.field = c.nextField(-1)
		case "down", "j", "tab":
			c.field = c.nextField(1)
		case "left", "h", "right", "l", " ":
			dir := 1
			if k := msg.String(); k == "left" || k == "h" {
				dir = -1
			}
			if c.field < len(settings) {
				settings[c.field].change(c, dir)
				c.rebuild()
			}
		case "r":
			c.opt = lyr.DefaultImport
			c.rebuild()
		case "pgup":
			c.ptop = max(0, c.ptop-c.previewRows())
		case "pgdown":
			if c.preview != nil {
				c.ptop = max(0, min(len(c.preview.Lines())-c.previewRows(), c.ptop+c.previewRows()))
			}
		case "enter":
			if c.preview == nil {
				break
			}
			s := c.preview
			wav := c.wavs[c.wavIdx]
			s.SetSource(c.lyrbPath, c.svpPath, wav)
			done := importDoneMsg{song: s, svpPath: c.svpPath, lyrbPath: c.lyrbPath, wavPath: wav}
			return c, func() tea.Msg { return done }
		}
	}
	return c, nil
}

// nextField moves the selection, skipping settings that don't apply.
func (c *Config) nextField(dir int) int {
	f := c.field
	for range len(settings) + 1 {
		f = (f + dir + len(settings) + 1) % (len(settings) + 1)
		if f == len(settings) || settings[f].enabled(c) {
			return f
		}
	}
	return c.field
}

func (c *Config) previewRows() int { return max(3, c.h-4) }

var (
	stLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#9e9e9e"))
	stValue  = lipgloss.NewStyle().Bold(true)
	stButton = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("#005f87")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	stWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7875f"))
)

const settingsWidth = 44

func (c *Config) View() string {
	if c.w == 0 {
		return ""
	}
	title := "parse settings"
	if c.reparse {
		title = "re-parse"
	}
	var b strings.Builder
	b.WriteString(stHeader.Width(c.w).Render(truncate(fmt.Sprintf("lyric-engine · %s · %s", title, filepath.Base(c.svpPath)), c.w-2)))
	b.WriteString("\n\n")

	// Settings column.
	var left []string
	for i, st := range settings {
		label := fmt.Sprintf("%-17s", st.label)
		val := st.value(c)
		row := stLabel.Render(label) + " " + stValue.Render("‹ "+val+" ›")
		if !st.enabled(c) {
			row = stDim.Render(label + "   " + val)
		}
		if i == c.field {
			row = stCursor.Render(" ") + " " + row
		} else {
			row = "  " + row
		}
		left = append(left, truncate(row, settingsWidth))
	}
	left = append(left, "")
	btn := "Open in editor"
	if c.reparse {
		btn = "Apply to song"
	}
	if c.field == len(settings) {
		left = append(left, "  "+stButton.Render("▸ "+btn))
	} else {
		left = append(left, "  "+stDim.Render("[ "+btn+" ]"))
	}
	left = append(left, "")
	if c.reparse {
		left = append(left, stWarn.Render("  replaces the current arrangement"), stDim.Render("  (u in the editor undoes it)"))
	} else if _, err := os.Stat(c.lyrbPath); err == nil {
		left = append(left, stWarn.Render("  "+filepath.Base(c.lyrbPath)+" exists:"), stWarn.Render("  ctrl+s in the editor overwrites it"))
	} else {
		left = append(left, stDim.Render("  saves to "+truncate(filepath.Base(c.lyrbPath), settingsWidth-11)))
	}

	// Preview column.
	pw := max(20, c.w-settingsWidth-3)
	var right []string
	if c.perr != "" {
		right = append(right, stWarn.Render(c.perr))
	} else {
		s := c.preview
		ls := s.Lines()
		stanzas := 0
		for _, l := range ls {
			if l.Stanza {
				stanzas++
			}
		}
		right = append(right, stDim.Render(fmt.Sprintf("preview · %d lines · %d stanzas · %d words", len(ls), stanzas, len(s.Tokens))))
		for i := c.ptop; i < len(ls) && len(right) <= c.previewRows(); i++ {
			mark := " "
			if ls[i].Stanza {
				mark = stStanza.Render("§")
			}
			row := fmt.Sprintf("%s %3d %s  %s", mark, i+1, stDim.Render(clock(int64(ls[i].Start))), render.LineText(s, i))
			right = append(right, truncate(row, pw))
		}
	}

	leftBlock := lipgloss.NewStyle().Width(settingsWidth).Render(strings.Join(left, "\n"))
	rightBlock := lipgloss.NewStyle().Width(pw).Render(strings.Join(right, "\n"))
	bodyH := c.h - 3
	sep := stDim.Render(strings.TrimSuffix(strings.Repeat(" │ \n", bodyH), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftBlock, sep, rightBlock)
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(stDim.Render(truncate("↑↓ setting  ←→ change  enter "+strings.ToLower(btn)+"  pgup/pgdn scroll preview  r reset  esc back", c.w)))
	return b.String()
}
