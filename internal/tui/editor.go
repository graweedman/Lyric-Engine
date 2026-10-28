// Package tui contains the arrangement editor and the full-screen player.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/audio"
	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second/30, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// displayPresets cycles with "d": context lines above/below the current line.
var displayPresets = [][2]uint8{{1, 2}, {1, 1}, {0, 1}, {0, 0}, {2, 2}, {0, 3}}

type snapshot struct {
	tokens  []lyr.Token
	display lyr.Display
	imp     lyr.ImportOptions
	msgs    []lyr.Message
	offset  int32
	cur     int
	msg     int
}

// reparseMsg asks the app to reopen parse settings for this song's source.
type reparseMsg struct{ e *Editor }

// openPickerMsg asks the app to show the file browser.
type openPickerMsg struct{}

// Editor is the arrangement editor model.
type Editor struct {
	song   *lyr.Song
	path   string
	player *audio.Player

	cur      int // cursor token
	msg      int // selected story message, -1 when a line is selected
	top      int // first row shown in the list
	follow   bool
	modified bool
	undo     []snapshot

	editing bool
	input   textinput.Model
	newMsg  bool // editing the text of a just-added message

	armed    string // key pressed once to confirm discarding changes
	status   string
	statusAt time.Time
	w, h     int
}

// NewEditor builds the editor for song, saving to path.
func NewEditor(song *lyr.Song, path string, player *audio.Player, note string) *Editor {
	ti := textinput.New()
	ti.Prompt = "text: "
	ti.CharLimit = 200
	e := &Editor{song: song, path: path, player: player, input: ti, follow: true, msg: -1}
	e.flash(note)
	return e
}

func (e *Editor) Init() tea.Cmd { return tick() }

func (e *Editor) flash(s string) { e.status, e.statusAt = s, time.Now() }

// lyricTime converts the audio clock to lyric time.
func (e *Editor) lyricTime() int64 { return e.player.Position() - int64(e.song.AudioOffsetMs) }

func (e *Editor) push() {
	e.undo = append(e.undo, snapshot{
		tokens:  append([]lyr.Token(nil), e.song.Tokens...),
		display: e.song.Display, imp: e.song.Import, offset: e.song.AudioOffsetMs, cur: e.cur,
		msgs: append([]lyr.Message(nil), e.song.Messages...), msg: e.msg,
	})
	if len(e.undo) > 500 {
		e.undo = e.undo[1:]
	}
	e.modified = true
}

func (e *Editor) changed() {
	e.song.Touch()
	e.modified = true
}

func (e *Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.w, e.h = msg.Width, msg.Height
		e.scroll()
		return e, nil
	case tickMsg:
		if e.follow && e.player.Playing() && !e.editing {
			if i := e.tokenAt(e.lyricTime()); i >= 0 {
				e.cur, e.msg = i, -1
				e.scroll()
			}
		}
		return e, tick()
	case tea.KeyMsg:
		if e.editing {
			return e.updateEditing(msg)
		}
		return e.updateKeys(msg)
	}
	return e, nil
}

func (e *Editor) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		v := strings.TrimSpace(e.input.Value())
		switch {
		case e.msg >= 0 && v == "":
			if e.newMsg || !e.song.Messages[e.msg].IsSpacer() {
				e.dropMessage() // an empty line stays an empty line
			}
		case e.msg >= 0 && v != e.song.Messages[e.msg].Text:
			if !e.newMsg {
				e.push()
			}
			e.song.Messages[e.msg].Text = v
			e.changed()
		case e.msg < 0 && v != "" && v != e.song.Tokens[e.cur].Text:
			e.push()
			e.song.Tokens[e.cur].Text = v
			e.changed()
		}
		e.stopEditing()
		return e, nil
	case "shift+tab":
		if v, ok := rotateCase(e.input.Value()); ok {
			e.input.SetValue(v)
			e.input.CursorEnd()
		}
		return e, nil
	case "esc":
		if e.newMsg {
			e.dropMessage()
		}
		e.stopEditing()
		return e, nil
	}
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return e, cmd
}

func (e *Editor) startEditing(text string) tea.Cmd {
	e.editing = true
	e.input.SetValue(text)
	e.input.CursorEnd()
	e.input.Focus()
	return textinput.Blink
}

func (e *Editor) stopEditing() {
	e.editing, e.newMsg = false, false
	e.input.Blur()
}

// dropMessage removes a just-added message that got no text, or deletes the
// selected one when its text was cleared.
func (e *Editor) dropMessage() {
	if e.newMsg && len(e.undo) > 0 {
		e.undo = e.undo[:len(e.undo)-1] // adding it never happened
	} else {
		e.push()
	}
	// A cancelled new message goes back to the row it was added after.
	e.removeMessage(e.msg, e.newMsg)
}

func (e *Editor) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := e.song
	n := len(s.Tokens)
	key := msg.String()
	armed := e.armed
	e.armed = ""
	// confirm guards actions that would drop unsaved changes.
	confirm := func(what string) bool {
		if !e.modified || armed == key {
			return true
		}
		e.armed = key
		e.flash("unsaved changes — press " + key + " again to " + what + ", ctrl+s to save")
		return false
	}
	switch key {
	case "ctrl+c":
		return e, tea.Quit
	case "q":
		if confirm("quit") {
			return e, tea.Quit
		}
		return e, nil
	case "o":
		if confirm("open another file") {
			e.player.Pause()
			return e, func() tea.Msg { return openPickerMsg{} }
		}
		return e, nil
	case "R":
		if e.song.SourcePath == "" {
			e.flash("this song has no source .svp to re-parse")
			return e, nil
		}
		return e, func() tea.Msg { return reparseMsg{e} }
	case "i":
		e.player.Pause()
		return e, func() tea.Msg { return infoMsg{e} }
	case "m":
		return e, e.addMessage()
	case "n":
		e.addSpacer()
		return e, nil
	case "v":
		e.push()
		e.song.Display.View = 1 - e.song.Display.View
		e.changed()
		e.flash("display view: " + e.song.Display.View.String())
		return e, nil
	case "a":
		e.push()
		e.song.Display.HUD = !e.song.Display.HUD
		e.changed()
		e.flash("analysis readout: " + onOff(e.song.Display.HUD))
		return e, nil
	case "L":
		e.push()
		e.song.Display.Large = !e.song.Display.Large
		e.changed()
		e.flash("large text: " + onOff(e.song.Display.Large))
		return e, nil
	case "up", "k", "down", "j", "pgup", "pgdown":
		d := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -e.listHeight(), "pgdown": e.listHeight()}[key]
		e.rowStep(d)
		e.follow = false
		e.scroll()
		return e, nil
	}
	if e.msg >= 0 {
		switch key {
		case "ctrl+s", "u", "d", " ", "f", ",", ".", "r", "[", "]", "{", "}":
			// shared keys, handled below
		default:
			return e, e.updateMessageKeys(key)
		}
	}
	if n == 0 {
		return e, nil
	}
	moved := false
	switch key {
	case "ctrl+s":
		if err := lyr.Save(e.path, s); err != nil {
			e.flash("save failed: " + err.Error())
		} else {
			e.modified = false
			e.flash("saved " + e.path)
		}

	// navigation
	case "left", "h":
		e.cur, moved = max(0, e.cur-1), true
	case "right", "l":
		e.cur, moved = min(n-1, e.cur+1), true
	case "home":
		e.cur, moved = s.Lines()[s.LineOf(e.cur)].First, true
	case "end":
		l := s.Lines()[s.LineOf(e.cur)]
		e.cur, moved = l.First+l.Count-1, true

	// arrangement
	case "enter":
		if e.cur > 0 && s.Tokens[e.cur].Break == lyr.BreakNone {
			e.push()
			s.Tokens[e.cur].Break = lyr.BreakLine
			e.changed()
		}
	case "backspace":
		e.mergeUp()
	case "s":
		first := s.Lines()[s.LineOf(e.cur)].First
		if first > 0 {
			e.push()
			if s.Tokens[first].Break == lyr.BreakStanza {
				s.Tokens[first].Break = lyr.BreakLine
			} else {
				s.Tokens[first].Break = lyr.BreakStanza
			}
			e.changed()
		}
	case "J":
		if e.cur > 0 {
			e.push()
			s.Tokens[e.cur].Join = !s.Tokens[e.cur].Join
			e.changed()
		}
	case "x":
		e.push()
		s.Tokens[e.cur].Hidden = !s.Tokens[e.cur].Hidden
		e.changed()
	case "b":
		e.push()
		s.Tokens[e.cur].Backing = !s.Tokens[e.cur].Backing
		e.changed()
		if s.Tokens[e.cur].Backing {
			e.flash("backing vocal: shown in (brackets)")
		} else {
			e.flash("lead vocal: shown without brackets")
		}
	case "c", "C":
		e.cycleColor(key == "C")
	case "~":
		if v, ok := rotateCase(s.Tokens[e.cur].Text); ok {
			e.push()
			s.Tokens[e.cur].Text = v
			e.changed()
			e.flash("case: " + caseName(v))
		}
	case "e":
		return e, e.startEditing(s.Tokens[e.cur].Text)
	case "u":
		if k := len(e.undo); k > 0 {
			sn := e.undo[k-1]
			e.undo = e.undo[:k-1]
			s.Tokens, s.Display, s.Import, s.AudioOffsetMs, e.cur = sn.tokens, sn.display, sn.imp, sn.offset, sn.cur
			s.Messages, e.msg = sn.msgs, sn.msg
			e.changed()
			e.flash("undo")
			moved = true
		}
	case "d":
		e.push()
		cur := [2]uint8{s.Display.Before, s.Display.After}
		next := displayPresets[0]
		for i, p := range displayPresets {
			if p == cur {
				next = displayPresets[(i+1)%len(displayPresets)]
			}
		}
		s.Display.Before, s.Display.After = next[0], next[1]
		e.changed()
		e.scroll()

	// transport & sync
	case " ":
		e.player.Toggle()
	case "g":
		e.player.SeekTo(int64(s.Tokens[e.cur].Start) + int64(s.AudioOffsetMs) - 1000)
		e.follow = true
	case "f":
		e.follow = !e.follow
		e.flash(fmt.Sprintf("follow playback: %v", e.follow))
	case ",":
		e.player.SeekTo(e.player.Position() - 5000)
	case ".":
		e.player.SeekTo(e.player.Position() + 5000)
	case "r":
		e.player.SeekTo(0)
	case "[", "]", "{", "}":
		d := map[string]int32{"[": -10, "]": 10, "{": -100, "}": 100}[key]
		e.push()
		s.AudioOffsetMs += d
		e.flash(fmt.Sprintf("audio offset %+d ms", s.AudioOffsetMs))
	}
	if moved {
		e.follow = false
		e.scroll()
	}
	return e, nil
}

// Replace swaps in a re-parsed arrangement (undoable), keeping the layout,
// sync offset and title.
func (e *Editor) Replace(parsed *lyr.Song) {
	e.push()
	e.song.Tokens = parsed.Tokens
	e.song.Import = parsed.Import
	e.song.SourcePath = parsed.SourcePath
	e.song.Analysis = parsed.Analysis
	// Voice colors fill in parts that have no color of their own.
	if d := &e.song.Display; d.LeadColor == 0 {
		d.LeadColor = parsed.Display.LeadColor
	}
	if d := &e.song.Display; d.BackColor == 0 {
		d.BackColor = parsed.Display.BackColor
	}
	e.changed()
	e.cur, e.msg = min(e.cur, max(0, len(e.song.Tokens)-1)), -1
	e.scroll()
	e.flash(fmt.Sprintf("re-parsed: %d lines — u to undo", len(e.song.Lines())))
}

// cycleColor changes the lyric color of the cursor word's part: the
// backing vocals for a word in (brackets), the lead otherwise.
func (e *Editor) cycleColor(back bool) {
	d := &e.song.Display
	c, part := &d.LeadColor, "lead"
	if e.song.Tokens[e.cur].Backing {
		c, part = &d.BackColor, "backing"
	}
	k := 0
	for i, mc := range lyr.MessageColors {
		if mc.RGB == *c || (mc.RGB == lyr.NoColor && *c == 0) {
			k = i
		}
	}
	step := 1
	if back {
		step = -1
	}
	e.push()
	next := lyr.MessageColors[(k+step+len(lyr.MessageColors))%len(lyr.MessageColors)]
	*c = next.RGB
	if next.RGB == lyr.NoColor {
		*c = 0 // lyric colors use 0 for the theme default
	}
	e.changed()
	e.flash(part + " lyric color: " + next.Name)
}

// lyricColorName names a lyric color (0 = the theme default).
func lyricColorName(rgb uint32) string {
	if rgb == 0 {
		return "default color"
	}
	return colorName(rgb)
}

// ApplyInfo sets the title and well-known metadata; it reports whether
// anything changed.
func (e *Editor) ApplyInfo(title string, meta map[string]string) bool {
	s := e.song
	changed := s.Title != title
	s.Title = title
	for _, k := range lyr.MetaKeys {
		if s.MetaValue(k.Key) != meta[k.Key] {
			s.SetMeta(k.Key, meta[k.Key])
			changed = true
		}
	}
	if changed {
		e.modified = true
	}
	return changed
}

// SetPlayer switches the audio (e.g. after choosing another WAV).
func (e *Editor) SetPlayer(p *audio.Player, audioPath string) {
	e.player = p
	if e.song.AudioPath != audioPath {
		e.song.AudioPath = audioPath
		e.modified = true
	}
}

// tokenAt is the last visible token that has started by lyric time t.
func (e *Editor) tokenAt(t int64) int {
	idx := -1
	for i, tok := range e.song.Tokens {
		if int64(tok.Start) > t {
			break
		}
		if !tok.Hidden {
			idx = i
		}
	}
	return idx
}

// minListWidth keeps the line list usable next to the preview.
const minListWidth = 40

// bodyHeight is the height of the preview | list row:
// everything except header 1, transport 1, info 1 and help 2.
func (e *Editor) bodyHeight() int { return max(3, e.h-5) }

// previewAspect is the shape of the final display window (play -aspect):
// the strip a 16:9 screen has left beside a 9:9 square.
const previewAspectW, previewAspectH = 7, 9

// previewWidth sizes the preview box to the final display's aspect ratio
// (terminal cells are about twice as tall as wide); the line list gets
// the rest.
func (e *Editor) previewWidth() int {
	pw := e.bodyHeight() * 2 * previewAspectW / previewAspectH
	if room := e.w - minListWidth - 1; pw > room {
		pw = max(room, min(20, e.w/2))
	}
	return max(pw, 4)
}

func (e *Editor) listHeight() int { return e.bodyHeight() }

func (e *Editor) scroll() {
	rows := e.rows()
	r := e.curRow(rows)
	h := e.listHeight()
	if r < e.top {
		e.top = r
	}
	if r >= e.top+h {
		e.top = r - h + 1
	}
	e.top = max(0, min(e.top, len(rows)-1))
}

var (
	stHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#005f87")).Padding(0, 1)
	stBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#444444"))
	stDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6c6c6c"))
	stCursor   = lipgloss.NewStyle().Reverse(true).Bold(true)
	stPlaying  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Bold(true)
	stHidden   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4e4e4e")).Strikethrough(true)
	stStanza   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af5f")).Bold(true)
	stJoin     = lipgloss.NewStyle().Foreground(lipgloss.Color("#d75f87"))
	stBacking  = lipgloss.NewStyle().Foreground(lipgloss.Color("#a8a8a8")).Italic(true)
	stStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af5f"))
	stBarFill  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff"))
	stBarEmpty = lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a"))
)

func (e *Editor) View() string {
	if e.w == 0 {
		return ""
	}
	s := e.song
	w := e.w
	t := e.lyricTime()
	var b strings.Builder

	mod := ""
	if e.modified {
		mod = " ●"
	}
	title := s.Title
	if title == "" {
		title = "untitled"
	}
	b.WriteString(stHeader.Width(w).Render(truncate(fmt.Sprintf("lyric-engine · %s · %s%s", title, e.path, mod), w-2)))
	b.WriteByte('\n')

	// Body: square preview (the exact final output) | line list.
	bh, pw := e.bodyHeight(), e.previewWidth()
	preview := stBox.Padding(0, 1).Render(render.Frame(s, t, max(1, pw-4), bh-2, render.DefaultTheme))

	lw := max(1, w-pw-1)
	playLine, playMsg := -1, render.ActiveMessage(s, t)
	if st := render.Resolve(s, t); st.Current >= 0 && playMsg < 0 {
		playLine = st.Current
	}
	all := e.rows()
	cr := e.curRow(all)
	rows := make([]string, bh)
	for row := range rows {
		k := e.top + row
		if k >= len(all) {
			continue
		}
		if r := all[k]; r.msg >= 0 {
			rows[row] = e.messageRow(r.msg, k == cr, r.msg == playMsg, lw)
		} else {
			rows[row] = e.lineRow(r.line, k == cr, r.line == playLine, t, lw)
		}
	}
	list := lipgloss.NewStyle().Width(lw).Render(strings.Join(rows, "\n"))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, preview, " ", list))
	b.WriteByte('\n')

	b.WriteString(e.transport(w))
	b.WriteByte('\n')

	// Token info / edit input.
	if e.editing {
		b.WriteString(e.input.View())
	} else if e.msg >= 0 {
		b.WriteString(stDim.Render(truncate(e.messageInfo(), w)))
	} else if len(s.Tokens) > 0 {
		tok := s.Tokens[e.cur]
		var flags []string
		if tok.Join {
			flags = append(flags, "joined")
		}
		if tok.Hidden {
			flags = append(flags, "hidden")
		}
		part, c := "lead", s.Display.LeadColor
		if tok.Backing {
			part, c = "backing (in brackets)", s.Display.BackColor
		}
		flags = append(flags, part+" "+lyricColorName(c))
		if len(tok.Segs) > 1 {
			flags = append(flags, fmt.Sprintf("%d syllable notes", len(tok.Segs)))
		}
		info := fmt.Sprintf("“%s”  %s → %s  (%d ms)  %s", tok.Text, clock(int64(tok.Start)), clock(int64(tok.End)),
			tok.End-tok.Start, strings.Join(flags, ", "))
		b.WriteString(stDim.Render(truncate(info, w)))
	}
	b.WriteByte('\n')

	if e.status != "" && time.Since(e.statusAt) < 4*time.Second {
		b.WriteString(stStatus.Render(truncate(e.status, w)))
	} else {
		help := "←→↑↓ move  enter break  ⌫ join lines  s stanza  J glue  x hide  b (backing)  c color  ~ case  e edit  u undo  m message  n empty line  v view  a analysis  d layout  i info  R re-parse  o open"
		if e.msg >= 0 {
			help = "message: e edit text  ~ case  c/C color  b bold  I italic  < > move ±0.1s  T to playhead  g play  x delete  n empty line after  u undo"
			if e.song.Messages[e.msg].IsSpacer() {
				help = "empty line: < > move ±0.1s  T to playhead  g play  x delete  n another  e add text  u undo"
			}
		}
		b.WriteString(stDim.Render(truncate(help, w)))
	}
	b.WriteByte('\n')
	b.WriteString(stDim.Render(truncate("ctrl+s save  q quit  space play  g play at cursor  f follow  , . ±5s  r restart  [ ] { } sync offset", w)))
	return b.String()
}

func (e *Editor) transport(w int) string {
	p := e.player
	icon := "⏸"
	if p.Playing() {
		icon = "▶"
	}
	pos, dur := p.Position(), p.DurationMs()
	follow := "     "
	if e.follow {
		follow = "follow"
	}
	audio := ""
	if p.Silent() {
		audio = " [no audio]"
	}
	left := fmt.Sprintf(" %s %s / %s ", icon, clock(pos), clock(dur))
	view := fmt.Sprintf("ctx %d/%d", e.song.Display.Before, e.song.Display.After)
	if e.song.Display.View == lyr.ViewLog {
		view = "log view"
	} else if e.song.Display.Large {
		view += " large"
	}
	right := fmt.Sprintf(" offset %+dms  %s  %s%s ", e.song.AudioOffsetMs, view, follow, audio)
	barW := w - lipgloss.Width(left) - lipgloss.Width(right)
	if barW < 4 {
		return truncate(left+right, w)
	}
	fill := 0
	if dur > 0 {
		fill = int(int64(barW) * min(pos, dur) / dur)
	}
	return left + stBarFill.Render(strings.Repeat("━", fill)) + stBarEmpty.Render(strings.Repeat("━", barW-fill)) + right
}

func (e *Editor) lineRow(li int, isCur, isPlay bool, t int64, w int) string {
	s := e.song
	l := s.Lines()[li]
	marker := "  "
	if isPlay {
		marker = stPlaying.Render("▶ ")
	}
	stanza := " "
	if l.Stanza {
		stanza = stStanza.Render("§")
	}
	num := fmt.Sprintf("%3d", li+1)
	if isCur {
		num = lipgloss.NewStyle().Bold(true).Render(num)
	} else {
		num = stDim.Render(num)
	}
	prefix := fmt.Sprintf("%s%s %s %s %s ", marker, num, stanza, stDim.Render(clock(int64(l.Start))), stDim.Render("│"))
	avail := w - lipgloss.Width(prefix)

	var parts []string
	curPart := -1
	for i := l.First; i < l.First+l.Count; i++ {
		tok := s.Tokens[i]
		txt := tok.Text
		var st lipgloss.Style
		switch {
		case tok.Hidden:
			st = stHidden
		case isPlay && t >= int64(tok.Start):
			st = stPlaying
		case tok.Backing:
			st = stBacking
			if c := s.Display.BackColor; c != 0 {
				st = st.Foreground(lipgloss.Color(fmt.Sprintf("#%06x", c)))
			}
		default:
			st = lipgloss.NewStyle()
			if c := s.Display.LeadColor; c != 0 {
				st = st.Foreground(lipgloss.Color(fmt.Sprintf("#%06x", c)))
			}
		}
		if i == e.cur {
			st = st.Inherit(stCursor)
			curPart = len(parts)
		}
		piece := st.Render(txt)
		// Runs of backing vocals are bracketed, as on the display.
		if tok.Backing && (i == l.First || !s.Tokens[i-1].Backing) {
			piece = stBacking.Render("(") + piece
		}
		if tok.Backing && (i == l.First+l.Count-1 || !s.Tokens[i+1].Backing) {
			piece += stBacking.Render(")")
		}
		if i > l.First {
			if tok.Join {
				piece = stJoin.Render("·") + piece
			} else {
				piece = " " + piece
			}
		}
		parts = append(parts, piece)
	}
	return prefix + fitParts(parts, curPart, avail)
}

// fitParts joins parts into at most avail cells, scrolling so that part
// focus stays visible.
func fitParts(parts []string, focus, avail int) string {
	total := 0
	for _, p := range parts {
		total += lipgloss.Width(p)
	}
	if total <= avail || avail < 8 {
		return strings.Join(parts, "")
	}
	start := 0
	if focus > 0 {
		// Drop leading parts until focus fits.
		for start < focus {
			wsum := 1
			for _, p := range parts[start : focus+1] {
				wsum += lipgloss.Width(p)
			}
			if wsum <= avail-1 {
				break
			}
			start++
		}
	}
	out := ""
	used := 0
	if start > 0 {
		out, used = "…", 1
	}
	for i := start; i < len(parts); i++ {
		pw := lipgloss.Width(parts[i])
		if used+pw > avail-1 && i > focus {
			out += "…"
			break
		}
		out += parts[i]
		used += pw
	}
	return out
}

func clock(ms int64) string {
	neg := ""
	if ms < 0 {
		neg, ms = "-", -ms
	}
	return fmt.Sprintf("%s%02d:%02d.%02d", neg, ms/60000, ms/1000%60, ms/10%100)
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
