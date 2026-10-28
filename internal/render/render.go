// Package render turns a song and a playback time into the terminal lyric
// display. The editor preview and the standalone player both call Frame, so
// what you see while arranging is exactly what the final output shows.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/lyr"
)

// Theme styles the display.
type Theme struct {
	Sung       lipgloss.Style // already-sung part of the current line
	Unsung     lipgloss.Style // not-yet-sung part of the current line
	Past       lipgloss.Style // context lines above
	Upcoming   lipgloss.Style // context lines below
	Intro      lipgloss.Style // song title before the first line
	Title      lipgloss.Style // artist line under it
	Credit     lipgloss.Style // vocalist, music and lyrics credits under the artist
	Message    lipgloss.Style // story messages without their own color
	BackSung   lipgloss.Style // sung part of (backing vocals) in the current line
	BackUnsung lipgloss.Style // unsung part of (backing vocals) in the current line
	HUDLabel   lipgloss.Style // analysis readout: field names and rules
	HUDValue   lipgloss.Style // analysis readout: values

	// DECLines draws the large focus view with DEC double-size lines, which
	// the terminal running the player must support (Windows Terminal,
	// xterm, …). Without it the large view is approximated by wrapping at
	// half width and spacing the rows, as in the editor preview.
	DECLines bool
}

var DefaultTheme = Theme{
	Sung:       lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Bold(true),
	Unsung:     lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Bold(true),
	Past:       lipgloss.NewStyle().Foreground(lipgloss.Color("#5f87af")),
	Upcoming:   lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
	Intro:      lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Bold(true),
	Title:      lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")).Italic(true),
	Credit:     lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")),
	Message:    lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af87")),
	BackSung:   lipgloss.NewStyle().Foreground(lipgloss.Color("#87afd7")).Italic(true),
	BackUnsung: lipgloss.NewStyle().Foreground(lipgloss.Color("#a8a8a8")).Italic(true),
	HUDLabel:   lipgloss.NewStyle().Foreground(lipgloss.Color("#4e7a5a")),
	HUDValue:   lipgloss.NewStyle().Foreground(lipgloss.Color("#87ffaf")).Bold(true),
}

// MessageStyle is the style of story message m.
func (th Theme) MessageStyle(m lyr.Message) lipgloss.Style {
	st := th.Message
	if m.Color != lyr.NoColor {
		st = st.Foreground(hex(m.Color))
	}
	return st.Bold(m.Bold).Italic(m.Italic)
}

// ForSong applies the song's lyric colors: the sung part of the lead and
// of the (backing vocals), with finished lines in a dimmed lead color.
func (th Theme) ForSong(s *lyr.Song) Theme {
	if c := s.Display.LeadColor; c != 0 {
		th.Sung = th.Sung.Foreground(hex(c))
		th.Past = th.Past.Foreground(hex(dim(c)))
	}
	if c := s.Display.BackColor; c != 0 {
		th.BackSung = th.BackSung.Foreground(hex(c))
	}
	return th
}

func hex(rgb uint32) lipgloss.Color { return lipgloss.Color(fmt.Sprintf("#%06x", rgb&0xFFFFFF)) }

// dim darkens a color for lines that were already sung.
func dim(rgb uint32) uint32 {
	var out uint32
	for sh := 0; sh <= 16; sh += 8 {
		out |= (rgb >> sh & 0xFF) * 3 / 5 << sh
	}
	return out
}

// holdMs is how long the last line of a stanza stays up after it ends
// before the screen clears for an instrumental gap.
const holdMs = 2500

// State is the resolved view at a moment in time.
type State struct {
	Current int   // current line index, -1 before the first line
	Rows    []int // line index per display row, -1 for an empty row
}

// Resolve picks which lines are on screen at lyric time t (ms).
func Resolve(s *lyr.Song, t int64) State {
	lines := s.Lines()
	d := s.Display
	rows := make([]int, int(d.Before)+1+int(d.After))
	for i := range rows {
		rows[i] = -1
	}
	if len(lines) == 0 {
		return State{Current: -1, Rows: rows}
	}
	lead := int64(d.LeadIn)
	cur := sort.Search(len(lines), func(i int) bool { return int64(lines[i].Start)-lead > t }) - 1

	if cur < 0 {
		// Before the song: preview the first lines as upcoming.
		for k := 0; k <= int(d.After); k++ {
			if k < len(lines) && lines[k].Stanzanum == 0 {
				rows[int(d.Before)+k] = k
			}
		}
		return State{Current: -1, Rows: rows}
	}
	cl := lines[cur]
	if t > int64(cl.End)+holdMs && (cur+1 >= len(lines) || lines[cur+1].Stanza) {
		return State{Current: -1, Rows: rows} // instrumental gap: clear
	}
	for k := range rows {
		i := cur - int(d.Before) + k
		if i >= 0 && i < len(lines) && lines[i].Stanzanum == cl.Stanzanum {
			rows[k] = i
		}
	}
	return State{Current: cur, Rows: rows}
}

// introLeadMs is how long before the first line's lead-in the intro (title
// and artist) gives way to a preview of the first lines.
const introLeadMs = 3000

// msgHoldMs is the longest a story message stays up in the focus view
// when no lyric line follows it sooner.
const msgHoldMs = 8000

// ActiveMessage is the index of the story message shown as a card in the
// focus view at lyric time t, or -1. A message stays up until the next
// line becomes current (or the next message, or msgHoldMs).
func ActiveMessage(s *lyr.Song, t int64) int {
	ms := s.Messages
	i := sort.Search(len(ms), func(i int) bool { return int64(ms[i].Time) > t }) - 1
	for i >= 0 && ms[i].IsSpacer() {
		i-- // empty lines only exist in the log view
	}
	if i < 0 || t >= int64(ms[i].Time)+msgHoldMs {
		return -1
	}
	lines, lead := s.Lines(), int64(s.Display.LeadIn)
	cur := sort.Search(len(lines), func(k int) bool { return int64(lines[k].Start)-lead > t }) - 1
	if cur >= 0 && int64(lines[cur].Start)-lead > int64(ms[i].Time) {
		return -1
	}
	return i
}

// Frame renders the display for lyric time t into a w×h block.
func Frame(s *lyr.Song, t int64, w, h int, th Theme) string {
	th = th.ForSong(s)
	var hud []string
	if s.Display.HUD {
		if hud = HUD(s, t, w, th); len(hud)+1 >= h {
			hud = nil
		}
	}
	out := frame(s, t, w, h-len(hud), th)
	if hud != nil {
		out = strings.Join(hud, "\n") + "\n" + out
	}
	if th.DECLines {
		// Every row sets its own size, so rows that were double-size in
		// the previous frame go back to normal.
		rows := strings.Split(out, "\n")
		for i, r := range rows {
			if !strings.HasPrefix(r, "\x1b#") {
				rows[i] = SingleRow + r
			}
		}
		out = strings.Join(rows, "\n")
	}
	return out
}

// DEC line attributes: the top and bottom halves of a double-size row,
// and a normal row. Callers adding rows of their own below a DECLines
// frame start them with SingleRow.
const (
	decTop    = "\x1b#3"
	decBottom = "\x1b#4"
	SingleRow = "\x1b#5"
)

// largeFrame lays out the focus view's rows in double-size text: wrapped
// to half the width, each row drawn as a top and a bottom half. Without
// DEC lines each row is followed by a blank one instead.
func largeFrame(out []string, w, h int, th Theme) string {
	// One cell short of the half width, so a row never ends on the last
	// column, where the terminal's erase-to-end-of-line would clear it.
	wrap := lipgloss.NewStyle().Width(w/2 - 1).Align(lipgloss.Center)
	var rows []string
	for _, o := range out {
		for _, r := range strings.Split(wrap.Render(o), "\n") {
			if th.DECLines {
				r = strings.TrimRight(r, " ")
				rows = append(rows, decTop+r, decBottom+r)
			} else {
				rows = append(rows, lipgloss.PlaceHorizontal(w, lipgloss.Center, r), "")
			}
		}
	}
	if !th.DECLines && len(rows) > 0 {
		rows = rows[:len(rows)-1] // no spacer below the last row
	}
	top := max(0, (h-len(rows))/2)
	rows = append(make([]string, top, h), rows...)
	for len(rows) < h {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}

func frame(s *lyr.Song, t int64, w, h int, th Theme) string {
	if s.Display.View == lyr.ViewLog {
		return logFrame(s, t, w, h, th)
	}
	var out []string
	if mi := ActiveMessage(s, t); mi >= 0 {
		m := s.Messages[mi]
		out = []string{th.MessageStyle(m).Render(m.Text)}
	} else if intro := Intro(s, th); intro != nil && (len(s.Lines()) == 0 ||
		t < int64(s.Lines()[0].Start)-int64(s.Display.LeadIn)-introLeadMs) {
		out = intro
	} else {
		st := Resolve(s, t)
		for _, li := range st.Rows {
			switch {
			case li < 0:
				out = append(out, "")
			case li == st.Current:
				out = append(out, currentLine(s, li, t, th))
			case li < st.Current:
				out = append(out, th.Past.Render(LineText(s, li)))
			default:
				out = append(out, th.Upcoming.Render(LineText(s, li)))
			}
		}
	}
	if s.Display.Large && w >= 8 {
		return largeFrame(out, w, h, th)
	}
	block := lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(strings.Join(out, "\n"))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
}

// logFrame renders the log view: lines and messages pile up in time order
// as they arrive and scroll up once the screen is full. The line being sung
// gets the karaoke wipe; finished lines dim; messages keep their colors.
func logFrame(s *lyr.Song, t int64, w, h int, th Theme) string {
	type item struct {
		at        int64
		line, msg int
	}
	lines, lead := s.Lines(), int64(s.Display.LeadIn)
	var items []item
	for i, l := range lines {
		items = append(items, item{int64(l.Start) - lead, i, -1})
	}
	for i, m := range s.Messages {
		items = append(items, item{int64(m.Time), -1, i})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at < items[j].at })

	tw := max(1, w-4) // two columns of margin on each side
	wrap := lipgloss.NewStyle().Width(tw)
	var rows []string
	add := func(styled string) {
		for _, r := range strings.Split(wrap.Render(styled), "\n") {
			rows = append(rows, "  "+r)
		}
	}
	if intro := Intro(s, th); intro != nil {
		for _, r := range intro {
			add(r)
		}
		rows = append(rows, "")
	}
	spacerLast := false // the last row is an empty line the user placed
	for _, it := range items {
		if it.at > t {
			break
		}
		if it.msg >= 0 {
			m := s.Messages[it.msg]
			if spacerLast = m.IsSpacer(); spacerLast {
				rows = append(rows, "")
			} else {
				add(th.MessageStyle(m).Render(m.Text))
			}
			continue
		}
		l := lines[it.line]
		// A stanza starts after a blank row, on top of any placed empty lines.
		if l.Stanza && it.line > 0 && len(rows) > 0 && (rows[len(rows)-1] != "" || spacerLast) {
			rows = append(rows, "")
		}
		spacerLast = false
		if t >= int64(l.End) {
			add(th.Past.Render(LineText(s, it.line)))
		} else {
			add(currentLine(s, it.line, t, th))
		}
	}
	if len(rows) > h {
		rows = rows[len(rows)-h:]
	}
	return lipgloss.Place(w, h, lipgloss.Left, lipgloss.Top, strings.Join(rows, "\n"))
}

// HUD is the analysis readout at lyric time t: tempo, time signature,
// bar and beat, key, the note being sung and the vocal range, wrapped to
// width w and closed by a rule.
func HUD(s *lyr.Song, t int64, w int, th Theme) []string {
	a := &s.Analysis
	field := func(label, value string) string {
		return th.HUDLabel.Render(label+" ") + th.HUDValue.Render(value)
	}
	var fields []string
	if beat, ok := a.BeatAt(t); ok {
		m, bar, inBar := a.BarAt(beat)
		fields = append(fields, field("BPM", fmt.Sprintf("%.1f", a.BPMAt(t))), field("SIG", fmt.Sprintf("%d/%d", m.Num, m.Den)))
		cur := int(inBar)
		dots := fmt.Sprintf("%d/%d", cur+1, m.Num)
		if m.Num <= 8 {
			var d strings.Builder
			for i := range int(m.Num) {
				if i == cur {
					d.WriteString(th.HUDValue.Render("●"))
				} else {
					d.WriteString(th.HUDLabel.Render("○"))
				}
			}
			dots = d.String()
		}
		fields = append(fields, field("BAR", fmt.Sprintf("%03d", bar+1))+" "+dots)
	}
	if a.Key.Known() {
		fields = append(fields, field("KEY", fmt.Sprintf("%s %.0f%%", strings.ToUpper(a.Key.String()), a.Key.Score*100)))
	}
	note := "---"
	if p := sungPitch(s, t); p > 0 {
		note = lyr.NoteName(p)
	}
	fields = append(fields, field("NOTE", fmt.Sprintf("%-4s", note)))
	if a.Low > 0 {
		fields = append(fields, field("RANGE", lyr.NoteName(a.Low)+"–"+lyr.NoteName(a.High)))
	}

	// Wrap the fields into left-aligned rows.
	var rows []string
	row := ""
	for _, f := range fields {
		switch {
		case row == "":
			row = f
		case lipgloss.Width(row)+3+lipgloss.Width(f) <= w:
			row += th.HUDLabel.Render(" · ") + f
		default:
			rows = append(rows, row)
			row = f
		}
	}
	rows = append(rows, row)
	left := lipgloss.NewStyle().Width(w).Align(lipgloss.Left)
	for i, r := range rows {
		rows[i] = left.Render(r)
	}
	return append(rows, th.HUDLabel.Render(strings.Repeat("─", w)))
}

// sungPitch is the MIDI note being sung at time t (the lead's when lead
// and backing overlap), or 0. Backing phrases sit together inside lines,
// so the tokens are not strictly in time order.
func sungPitch(s *lyr.Song, t int64) uint8 {
	var back uint8
	for _, tok := range s.Tokens {
		if tok.Hidden || t < int64(tok.Start) || t >= int64(tok.End) || tok.Pitch == 0 {
			continue
		}
		if !tok.Backing {
			return tok.Pitch
		}
		back = tok.Pitch
	}
	return back
}

// Intro is the title card shown before the first line: the title, the
// artist and the credits (nil when the song has none of them).
func Intro(s *lyr.Song, th Theme) []string {
	var out []string
	if s.Title != "" {
		out = append(out, th.Intro.Render(s.Title))
	}
	if a := s.MetaValue("artist"); a != "" {
		out = append(out, th.Title.Render(a))
	}
	for _, c := range Credits(s) {
		out = append(out, th.Credit.Render(c))
	}
	return out
}

// Credits are the vocalist, music and lyrics lines of the title card. One
// person who wrote both music and lyrics gets a single line.
func Credits(s *lyr.Song) []string {
	var out []string
	if v := s.MetaValue("vocal"); v != "" {
		out = append(out, "Vocal: "+v)
	}
	music, words := s.MetaValue("music"), s.MetaValue("lyrics")
	switch {
	case music != "" && music == words:
		out = append(out, "Music & lyrics: "+music)
	default:
		if music != "" {
			out = append(out, "Music: "+music)
		}
		if words != "" {
			out = append(out, "Lyrics: "+words)
		}
	}
	return out
}

// LineText is the plain text of line li, with backing vocals in (brackets).
func LineText(s *lyr.Song, li int) string {
	l := s.Lines()[li]
	var b strings.Builder
	back := false // the previous visible token is a backing vocal
	for i := l.First; i < l.First+l.Count; i++ {
		t := s.Tokens[i]
		if t.Hidden {
			continue
		}
		if back && !t.Backing {
			b.WriteByte(')')
		}
		if b.Len() > 0 && !t.Join {
			b.WriteByte(' ')
		}
		if t.Backing && !back {
			b.WriteByte('(')
		}
		back = t.Backing
		b.WriteString(t.Text)
	}
	if back {
		b.WriteByte(')')
	}
	return b.String()
}

// PlainText is the song's lyrics as plain text: one line per lyric line and
// a blank line between stanzas. With timestamps, each line is prefixed with
// its start time as [mm:ss.xx].
func PlainText(s *lyr.Song, timestamps bool) string {
	var b strings.Builder
	for i, l := range s.Lines() {
		text := LineText(s, i)
		if text == "" {
			continue
		}
		if l.Stanza && b.Len() > 0 {
			b.WriteByte('\n')
		}
		if timestamps {
			cs := l.Start / 10
			fmt.Fprintf(&b, "[%02d:%02d.%02d] ", cs/6000, cs/100%60, cs%100)
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

// currentLine renders line li with a karaoke wipe at time t.
func currentLine(s *lyr.Song, li int, t int64, th Theme) string {
	l := s.Lines()[li]
	var b strings.Builder
	first := true
	back := -1 // the previous visible token, when it is a backing vocal
	// closeBack ends a run of backing vocals; the bracket is sung once the
	// run's last word is.
	closeBack := func() {
		if back >= 0 {
			st := th.BackUnsung
			if t >= int64(s.Tokens[back].End) {
				st = th.BackSung
			}
			b.WriteString(st.Render(")"))
		}
	}
	for i := l.First; i < l.First+l.Count; i++ {
		tok := s.Tokens[i]
		if tok.Hidden {
			continue
		}
		if !tok.Backing {
			closeBack()
		}
		if !first && !tok.Join {
			b.WriteString(sep(s, i, t, th))
		}
		first = false
		sung, unsung := th.Sung, th.Unsung
		if tok.Backing {
			sung, unsung = th.BackSung, th.BackUnsung
			if back < 0 {
				if t >= int64(tok.Start) {
					b.WriteString(sung.Render("("))
				} else {
					b.WriteString(unsung.Render("("))
				}
			}
			back = i
		} else {
			back = -1
		}
		r := []rune(tok.Text)
		n := tok.SungRunes(t, len(r))
		if n > 0 {
			b.WriteString(sung.Render(string(r[:n])))
		}
		if n < len(r) {
			b.WriteString(unsung.Render(string(r[n:])))
		}
	}
	closeBack()
	return b.String()
}

// sep styles the space before token i as sung once that token has started,
// so the wipe reads as continuous.
func sep(s *lyr.Song, i int, t int64, th Theme) string {
	if t >= int64(s.Tokens[i].Start) {
		return th.Sung.Render(" ")
	}
	return th.Unsung.Render(" ")
}
