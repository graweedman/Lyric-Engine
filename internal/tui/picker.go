package tui

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/svp"
)

// openFileMsg asks the app to open a .svp (parse settings) or .lyrb (editor).
type openFileMsg struct{ path string }

type pickEntry struct {
	name string
	dir  bool
	note string // e.g. "arranged" for an .svp that already has a .lyrb
}

// Picker browses directories for .svp, .ustx and .lyrb files.
type Picker struct {
	dir     string
	entries []pickEntry
	cur     int
	top     int
	err     string
	w, h    int

	title    string   // header label
	exts     []string // lower-case extensions to list
	help     string   // bottom line
	onPick   func(string) tea.Msg
	onCancel func() tea.Msg // nil: esc/q quits the program
}

// NewPicker browses for songs (.svp / .ustx / .lyrb) starting at dir.
func NewPicker(dir string) *Picker {
	p := &Picker{
		title:  "open",
		exts:   append(slices.Clone(svp.Exts), ".lyrb"),
		help:   "↑↓ select  enter open  ⌫ parent folder  q quit   ·   .svp/.ustx → parse settings, .lyrb → editor",
		onPick: func(path string) tea.Msg { return openFileMsg{path} },
	}
	p.chdir(dir, "")
	return p
}

// NewFilePicker browses for files with the given extensions; enter sends
// onPick(path), esc sends onCancel(). focus preselects a file name in dir.
func NewFilePicker(title, dir, focus string, exts []string, onPick func(string) tea.Msg, onCancel func() tea.Msg) *Picker {
	p := &Picker{
		title: title, exts: exts, onPick: onPick, onCancel: onCancel,
		help: "↑↓ select  enter choose  ⌫ parent folder  esc cancel",
	}
	p.chdir(dir, focus)
	return p
}

func (p *Picker) chdir(dir, focus string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		p.err = err.Error()
		return
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		p.err = err.Error()
		return
	}
	p.dir, p.err = abs, ""
	p.entries = p.entries[:0]
	if filepath.Dir(abs) != abs {
		p.entries = append(p.entries, pickEntry{name: "..", dir: true})
	}
	var dirs, files []pickEntry
	names := map[string]bool{}
	for _, it := range items {
		names[strings.ToLower(it.Name())] = true
	}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if it.IsDir() {
			dirs = append(dirs, pickEntry{name: name, dir: true})
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !slices.Contains(p.exts, ext) {
			continue
		}
		e := pickEntry{name: name}
		if svp.IsProject(name) && names[strings.ToLower(filepath.Base(lyr.LyrbPath(name)))] {
			e.note = "has .lyrb"
		}
		files = append(files, e)
	}
	less := func(s []pickEntry) func(i, j int) bool {
		return func(i, j int) bool { return strings.ToLower(s[i].name) < strings.ToLower(s[j].name) }
	}
	sort.Slice(dirs, less(dirs))
	sort.Slice(files, less(files))
	p.entries = append(append(p.entries, files...), dirs...)
	p.cur, p.top = 0, 0
	if len(p.entries) > 1 && p.entries[0].name == ".." {
		p.cur = 1 // start on the first file/folder, not the parent
	}
	for i, e := range p.entries {
		if e.name == focus {
			p.cur = i
		}
	}
	p.scroll()
}

func (p *Picker) Init() tea.Cmd { return nil }

func (p *Picker) listHeight() int { return max(3, p.h-4) }

func (p *Picker) scroll() {
	h := p.listHeight()
	if p.cur < p.top {
		p.top = p.cur
	}
	if p.cur >= p.top+h {
		p.top = p.cur - h + 1
	}
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.w, p.h = msg.Width, msg.Height
		p.scroll()
	case tea.KeyMsg:
		n := len(p.entries)
		switch msg.String() {
		case "ctrl+c":
			return p, tea.Quit
		case "q", "esc":
			if p.onCancel != nil {
				return p, p.onCancel
			}
			return p, tea.Quit
		case "up", "k":
			p.cur = max(0, p.cur-1)
		case "down", "j":
			p.cur = min(n-1, p.cur+1)
		case "pgup":
			p.cur = max(0, p.cur-p.listHeight())
		case "pgdown":
			p.cur = min(n-1, p.cur+p.listHeight())
		case "home":
			p.cur = 0
		case "end":
			p.cur = n - 1
		case "backspace", "left", "h":
			p.chdir(filepath.Dir(p.dir), filepath.Base(p.dir))
		case "enter", "right", "l":
			if p.cur < 0 || p.cur >= n {
				break
			}
			e := p.entries[p.cur]
			if e.name == ".." {
				p.chdir(filepath.Dir(p.dir), filepath.Base(p.dir))
			} else if e.dir {
				p.chdir(filepath.Join(p.dir, e.name), "")
			} else {
				path := filepath.Join(p.dir, e.name)
				return p, func() tea.Msg { return p.onPick(path) }
			}
		}
		p.scroll()
	}
	return p, nil
}

var (
	stDir  = lipgloss.NewStyle().Foreground(lipgloss.Color("#87afd7"))
	stSvp  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af5f"))
	stLyrb = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff"))
)

func (p *Picker) View() string {
	if p.w == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(stHeader.Width(p.w).Render(truncate("lyric-engine · "+p.title+" · "+p.dir, p.w-2)))
	b.WriteString("\n\n")
	h := p.listHeight()
	for row := 0; row < h; row++ {
		i := p.top + row
		if i >= len(p.entries) {
			if row == 0 {
				b.WriteString(stDim.Render("  (no " + strings.Join(p.exts, " or ") + " files or folders here)"))
			}
			b.WriteByte('\n')
			continue
		}
		e := p.entries[i]
		var label string
		switch {
		case e.dir:
			label = stDir.Render(e.name + "/")
		case svp.IsProject(e.name):
			label = stSvp.Render(e.name)
		case strings.EqualFold(filepath.Ext(e.name), ".wav"):
			label = stValue.Render(e.name)
		default:
			label = stLyrb.Render(e.name)
		}
		if e.note != "" {
			label += "  " + stDim.Render(e.note)
		}
		if i == p.cur {
			label = stCursor.Render(" ") + " " + label
		} else {
			label = "  " + label
		}
		b.WriteString(label)
		b.WriteByte('\n')
	}
	help := p.help
	if p.err != "" {
		help = "error: " + p.err
	}
	b.WriteString(stDim.Render(truncate(help, p.w)))
	return b.String()
}
