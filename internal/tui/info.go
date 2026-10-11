package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/graweedman/lyric-engine/internal/lyr"
)

// infoDoneMsg carries the edited song information back to the app.
type infoDoneMsg struct {
	title string
	meta  map[string]string // well-known keys → value ("" removes)
	audio string            // path usable from the working dir, "" = none
}

// wavPickedMsg / wavCancelMsg come back from the embedded WAV browser.
type wavPickedMsg struct{ path string }
type wavCancelMsg struct{}

// Info edits the song's title, metadata and audio file.
type Info struct {
	path    string // the .lyrb, for showing and resolving paths
	title   string
	vals    []string // per lyr.MetaKeys
	audio   string
	field   int
	editing bool
	input   textinput.Model
	browser *Picker // non-nil while choosing a WAV
	w, h    int
}

// NewInfo opens the info screen for song (saved at path).
func NewInfo(song *lyr.Song, path string) *Info {
	in := textinput.New()
	in.CharLimit = 300
	i := &Info{path: path, title: song.Title, audio: lyr.Resolve(path, song.AudioPath), input: in}
	for _, k := range lyr.MetaKeys {
		i.vals = append(i.vals, song.MetaValue(k.Key))
	}
	return i
}

// Field layout: 0 title, 1..n metadata, n+1 audio, n+2 done.
func (i *Info) audioField() int { return len(lyr.MetaKeys) + 1 }
func (i *Info) doneField() int  { return len(lyr.MetaKeys) + 2 }

func (i *Info) label(f int) string {
	switch {
	case f == 0:
		return "Title"
	case f <= len(lyr.MetaKeys):
		return lyr.MetaKeys[f-1].Label
	case f == i.audioField():
		return "Audio file"
	}
	return ""
}

func (i *Info) value(f int) *string {
	switch {
	case f == 0:
		return &i.title
	case f <= len(lyr.MetaKeys):
		return &i.vals[f-1]
	}
	return nil
}

func (i *Info) done() tea.Msg {
	m := map[string]string{}
	for k, key := range lyr.MetaKeys {
		m[key.Key] = strings.TrimSpace(i.vals[k])
	}
	return infoDoneMsg{title: strings.TrimSpace(i.title), meta: m, audio: i.audio}
}

func (i *Info) Init() tea.Cmd { return nil }

func (i *Info) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		i.w, i.h = msg.Width, msg.Height
		if i.browser != nil {
			i.browser.Update(msg)
		}
		return i, nil
	case wavPickedMsg:
		i.audio, i.browser = msg.path, nil
		return i, nil
	case wavCancelMsg:
		i.browser = nil
		return i, nil
	}
	if i.browser != nil {
		_, cmd := i.browser.Update(msg)
		return i, cmd
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return i, nil
	}
	if i.editing {
		switch key.String() {
		case "enter":
			*i.value(i.field) = i.input.Value()
			fallthrough
		case "esc":
			i.editing = false
			i.input.Blur()
			return i, nil
		}
		var cmd tea.Cmd
		i.input, cmd = i.input.Update(msg)
		return i, cmd
	}
	switch key.String() {
	case "ctrl+c":
		return i, tea.Quit
	case "esc", "q":
		return i, i.done
	case "up", "k", "shift+tab":
		i.field = (i.field + i.doneField()) % (i.doneField() + 1)
	case "down", "j", "tab":
		i.field = (i.field + 1) % (i.doneField() + 1)
	case "x", "delete":
		if i.field == i.audioField() {
			i.audio = ""
		} else if v := i.value(i.field); v != nil {
			*v = ""
		}
	case "enter":
		switch {
		case i.field == i.doneField():
			return i, i.done
		case i.field == i.audioField():
			dir, focus := filepath.Dir(i.path), ""
			if i.audio != "" {
				dir, focus = filepath.Dir(i.audio), filepath.Base(i.audio)
			}
			i.browser = NewFilePicker("choose audio (.wav)", dir, focus, []string{".wav"},
				func(p string) tea.Msg { return wavPickedMsg{p} },
				func() tea.Msg { return wavCancelMsg{} })
			if i.w > 0 {
				i.browser.Update(tea.WindowSizeMsg{Width: i.w, Height: i.h})
			}
		default:
			i.editing = true
			i.input.SetValue(*i.value(i.field))
			i.input.CursorEnd()
			i.input.Focus()
			return i, textinput.Blink
		}
	}
	return i, nil
}

const infoLabelWidth = 14

func (i *Info) View() string {
	if i.w == 0 {
		return ""
	}
	if i.browser != nil {
		return i.browser.View()
	}
	var b strings.Builder
	b.WriteString(stHeader.Width(i.w).Render(truncate("lyric-engine · song info · "+filepath.Base(i.path), i.w-2)))
	b.WriteString("\n\n")
	valW := max(10, i.w-infoLabelWidth-6)
	for f := 0; f < i.doneField(); f++ {
		label := stLabel.Render(padRight(i.label(f), infoLabelWidth))
		var val string
		switch {
		case f == i.field && i.editing:
			i.input.Width = valW - 2
			val = i.input.View()
		case f == i.audioField():
			if i.audio == "" {
				val = stDim.Render("none")
			} else {
				val = stValue.Render(truncate(filepath.Base(i.audio), valW))
				if _, err := os.Stat(i.audio); err != nil {
					val += "  " + stWarn.Render("(file not found)")
				}
			}
			if f == i.field {
				val += "  " + stDim.Render("enter browse · x remove")
			}
		default:
			if v := *i.value(f); v != "" {
				val = stValue.Render(truncate(v, valW))
			} else {
				val = stDim.Render("—")
			}
		}
		cursor := "  "
		if f == i.field {
			cursor = stCursor.Render(" ") + " "
		}
		b.WriteString(cursor + label + " " + val + "\n")
		if f == 0 || f == len(lyr.MetaKeys) {
			b.WriteByte('\n') // group: title | details | audio
		}
	}
	b.WriteByte('\n')
	if i.field == i.doneField() {
		b.WriteString("  " + stButton.Render("▸ Done"))
	} else {
		b.WriteString("  " + stDim.Render("[ Done ]"))
	}
	b.WriteString("\n\n")
	b.WriteString(stDim.Render(truncate("  The title and artist appear on the display before the first line.", i.w)))
	rows := strings.Count(b.String(), "\n") + 1
	b.WriteString(strings.Repeat("\n", max(1, i.h-rows)))
	b.WriteString(stDim.Render(truncate("↑↓ field  enter edit/browse  x clear  esc back (changes are kept; ctrl+s in the editor saves)", i.w)))
	return b.String()
}

func padRight(s string, n int) string {
	if w := len([]rune(s)); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
