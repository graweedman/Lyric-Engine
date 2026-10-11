package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/render"
)

// infoMsg asks the app to open the song info screen for this editor.
type infoMsg struct{ e *Editor }

// listRow is one row of the editor list: a lyric line or a story message,
// merged in time order (a message sorts before a line starting at the
// same moment).
type listRow struct{ line, msg int }

func (e *Editor) rows() []listRow {
	ls, ms := e.song.Lines(), e.song.Messages
	out := make([]listRow, 0, len(ls)+len(ms))
	i, j := 0, 0
	for i < len(ls) || j < len(ms) {
		if j < len(ms) && (i >= len(ls) || ms[j].Time <= ls[i].Start) {
			out = append(out, listRow{-1, j})
			j++
		} else {
			out = append(out, listRow{i, -1})
			i++
		}
	}
	return out
}

// curRow is the index in rows of the selected line or message.
func (e *Editor) curRow(rows []listRow) int {
	li := -1
	if e.msg < 0 && len(e.song.Tokens) > 0 {
		li = e.song.LineOf(e.cur)
	}
	for k, r := range rows {
		if (e.msg >= 0 && r.msg == e.msg) || (e.msg < 0 && r.line == li) {
			return k
		}
	}
	return 0
}

// selectRow selects row k of the list (clamped): a message, or the first
// word of a line. Used after a row disappears so the selection stays put.
func (e *Editor) selectRow(k int) {
	rows := e.rows()
	if len(rows) == 0 {
		e.msg = -1
		return
	}
	r := rows[max(0, min(len(rows)-1, k))]
	if r.msg >= 0 {
		e.msg = r.msg
	} else {
		e.msg, e.cur = -1, e.song.Lines()[r.line].First
	}
	e.scroll()
}

// removeMessage deletes message i (the caller records undo) and, when it
// was selected, selects the row that took its place (or the one above).
func (e *Editor) removeMessage(i int, above bool) {
	k := e.curRow(e.rows())
	if e.msg != i {
		// Deleting a row other than the selected one: keep the selection.
		k = -1
	}
	s := e.song
	s.Messages = append(s.Messages[:i:i], s.Messages[i+1:]...)
	switch {
	case k < 0 && e.msg > i:
		e.msg-- // the selected message shifted up
	case k >= 0 && above:
		e.selectRow(k - 1)
	case k >= 0:
		e.selectRow(k)
	}
	e.changed()
	e.scroll()
}

// mergeUp handles backspace on a lyric line: an empty line right above it
// is deleted (like a blank line in a text editor); otherwise the line
// joins the previous lyric line. It won't join across a story message,
// which would land in the middle of the merged line.
func (e *Editor) mergeUp() {
	s := e.song
	rows := e.rows()
	k := e.curRow(rows)
	if k > 0 {
		if prev := rows[k-1]; prev.msg >= 0 {
			if s.Messages[prev.msg].IsSpacer() {
				e.push()
				e.removeMessage(prev.msg, false)
				e.flash("empty line removed")
			} else {
				e.flash("a message sits above this line — move it (< >) or delete it (x) first")
			}
			return
		}
	}
	first := s.Lines()[s.LineOf(e.cur)].First
	if first == 0 {
		return
	}
	e.push()
	s.Tokens[first].Break = lyr.BreakNone
	e.changed()
	e.scroll()
}

// rowStep moves the selection d rows, keeping the word column on lines.
func (e *Editor) rowStep(d int) {
	rows := e.rows()
	if len(rows) == 0 {
		return
	}
	col := 0
	if e.msg < 0 && len(e.song.Tokens) > 0 {
		col = e.cur - e.song.Lines()[e.song.LineOf(e.cur)].First
	}
	r := rows[max(0, min(len(rows)-1, e.curRow(rows)+d))]
	if r.msg >= 0 {
		e.msg = r.msg
		return
	}
	l := e.song.Lines()[r.line]
	e.msg, e.cur = -1, l.First+min(col, l.Count-1)
}

// insertAt picks where a new message or empty line goes: right after the
// selected message; while stopped at the very start, just after the
// selected line; otherwise at the playhead.
func (e *Editor) insertAt() (at int64, where string) {
	switch {
	case e.msg >= 0:
		return int64(e.song.Messages[e.msg].Time), "after the selected row"
	case !e.player.Playing() && e.player.Position() == 0 && len(e.song.Tokens) > 0:
		li := e.song.LineOf(e.cur)
		return int64(e.song.Lines()[li].End) + 100, fmt.Sprintf("after line %d", li+1)
	}
	return max(0, e.lyricTime()), "at the playhead"
}

// addSpacer inserts an empty line, which only the log view shows.
func (e *Editor) addSpacer() {
	at, where := e.insertAt()
	e.push()
	e.msg = e.song.AddMessage(lyr.Message{Time: uint32(at), Color: lyr.NoColor})
	e.changed()
	e.scroll()
	e.flash(fmt.Sprintf("empty line at %s (%s) — shows in the log view only", clock(at), where))
}

// addMessage inserts an empty message and starts editing its text.
func (e *Editor) addMessage() tea.Cmd {
	at, where := e.insertAt()
	e.push()
	e.msg = e.song.AddMessage(lyr.Message{Time: uint32(at), Color: lyr.NoColor})
	e.newMsg = true
	e.changed()
	e.scroll()
	e.flash(fmt.Sprintf("new message at %s (%s) — type the text, enter to keep", clock(at), where))
	return e.startEditing("")
}

// updateMessageKeys handles keys while a message row is selected.
func (e *Editor) updateMessageKeys(key string) tea.Cmd {
	s := e.song
	m := &s.Messages[e.msg]
	move := func(ms int64) {
		e.push()
		m.Time = uint32(max(0, ms))
		e.msg = s.SortMessages(e.msg)
		e.changed()
		e.scroll()
	}
	switch key {
	case "e", "enter":
		return e.startEditing(m.Text)
	case "c", "C":
		d := 1
		if key == "C" {
			d = -1
		}
		k := 0
		for i, c := range lyr.MessageColors {
			if c.RGB == m.Color {
				k = i
			}
		}
		e.push()
		c := lyr.MessageColors[(k+d+len(lyr.MessageColors))%len(lyr.MessageColors)]
		m.Color = c.RGB
		e.changed()
		e.flash("color: " + c.Name)
	case "~":
		if v, ok := rotateCase(m.Text); ok {
			e.push()
			m.Text = v
			e.changed()
			e.flash("case: " + caseName(v))
		}
	case "b":
		e.push()
		m.Bold = !m.Bold
		e.changed()
	case "I":
		e.push()
		m.Italic = !m.Italic
		e.changed()
	case "<":
		move(int64(m.Time) - 100)
	case ">":
		move(int64(m.Time) + 100)
	case "T":
		move(e.lyricTime())
	case "g":
		e.player.SeekTo(int64(m.Time) + int64(s.AudioOffsetMs) - 1000)
	case "x", "delete", "backspace":
		e.push()
		what := "message"
		if m.IsSpacer() {
			what = "empty line"
		}
		e.removeMessage(e.msg, false)
		e.flash(what + " deleted — u to undo")
	}
	return nil
}

func colorName(rgb uint32) string {
	for _, c := range lyr.MessageColors {
		if c.RGB == rgb {
			return c.Name
		}
	}
	return fmt.Sprintf("#%06x", rgb)
}

func (e *Editor) messageInfo() string {
	m := e.song.Messages[e.msg]
	if m.IsSpacer() {
		return fmt.Sprintf("empty line at %s — visible in the log view only; e adds text to make it a message", clock(int64(m.Time)))
	}
	flags := []string{colorName(m.Color)}
	if m.Bold {
		flags = append(flags, "bold")
	}
	if m.Italic {
		flags = append(flags, "italic")
	}
	return fmt.Sprintf("message at %s  %s", clock(int64(m.Time)), strings.Join(flags, ", "))
}

func (e *Editor) messageRow(mi int, isCur, isPlay bool, w int) string {
	m := e.song.Messages[mi]
	marker := "  "
	if isPlay {
		marker = stPlaying.Render("▶ ")
	}
	glyph := "  ✦"
	if m.IsSpacer() {
		glyph = "  ↵"
	}
	icon := stDim.Render(glyph)
	if isCur {
		icon = lipgloss.NewStyle().Bold(true).Render(glyph)
	}
	prefix := fmt.Sprintf("%s%s   %s %s ", marker, icon, stDim.Render(clock(int64(m.Time))), stDim.Render("│"))
	st := render.DefaultTheme.MessageStyle(m)
	text := m.Text
	if m.IsSpacer() {
		st, text = stDim.Italic(true), "(empty line — log view only)"
	}
	if isCur {
		st = st.Inherit(stCursor)
	}
	return prefix + st.Render(truncate(text, max(4, w-lipgloss.Width(prefix))))
}
