// Package lyr holds the lyric document model, its construction from SynthV
// notes, and the compact binary .lyrb codec.
package lyr

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/graweedman/lyric-engine/internal/svp"
)

// Break describes what separates a token from the one before it.
type Break uint8

const (
	BreakNone   Break = iota // same line
	BreakLine                // starts a new line
	BreakStanza              // starts a new line and a new stanza (clears context)
)

// Token is one displayable word/syllable with its sung time span.
type Token struct {
	Start  uint32 // ms
	End    uint32 // ms
	Text   string
	Pitch  uint8
	Break  Break
	Join   bool // render without a space before this token
	Hidden bool // keep timing, but never display
	// Backing marks a word from the backing-vocal track; runs of backing
	// words are displayed in (brackets).
	Backing bool
	// Segs are the word's syllable notes when it spans several (a word
	// note followed by "+" notes); nil for single-note words. "-" notes
	// extend the segment they follow.
	Segs []Seg
}

// Seg is the sung span of one syllable note, in ms.
type Seg struct{ Start, End uint32 }

// SungRunes is how many of the token's n runes are sung at time t (ms).
// Each syllable note animates an equal share of the runes over exactly its
// own duration, holding during rests between the word's notes.
func (t Token) SungRunes(at int64, n int) int {
	segs := t.Segs
	if len(segs) < 2 {
		segs = []Seg{{t.Start, t.End}}
	}
	k := len(segs)
	for i, sg := range segs {
		lo, hi := i*n/k, (i+1)*n/k
		start, end := int64(sg.Start), int64(sg.End)
		switch {
		case at <= start:
			return lo
		case at < end:
			return lo + int(float64(hi-lo)*float64(at-start)/float64(end-start))
		}
	}
	return n
}

// Line is a derived view: a run of tokens between breaks.
type Line struct {
	Start, End   uint32 // ms, spans visible tokens only
	First, Count int
	Stanza       bool // first line of a stanza
	Stanzanum    int
}

// ViewMode is how the final terminal output presents the lyrics.
type ViewMode uint8

const (
	ViewFocus ViewMode = iota // the current line centered with a few lines of context
	ViewLog                   // lines accumulate and scroll up like a log
)

func (v ViewMode) String() string {
	if v == ViewLog {
		return "log"
	}
	return "focus"
}

// Display holds how the final terminal output is laid out.
type Display struct {
	Before uint8  // context lines shown above the current one (focus view)
	After  uint8  // upcoming lines shown below (focus view)
	LeadIn uint16 // ms before a line starts that it becomes current
	View   ViewMode
	// Lyric colors as 0xRRGGBB; 0 uses the theme's default. Lead colors
	// the main vocal, Back the (backing vocals).
	LeadColor, BackColor uint32
	// HUD shows the analysis readout (BPM, key, bar/beat, note) on top.
	HUD bool
}

// Song is the full document.
type Song struct {
	Title         string
	SourcePath    string
	AudioPath     string
	AudioOffsetMs int32 // added to lyric times when syncing with audio
	Display       Display
	Import        ImportOptions // settings the song was last parsed with
	Meta          []MetaField   // song information besides the title, in order
	Messages      []Message     // story lines between the lyrics, sorted by time
	Analysis      Analysis      // tempo, meter, key and range
	Tokens        []Token

	lines []Line // cache; rebuilt by Lines()
	dirty bool
}

// Message is a story-telling line that pops up at a moment in the song,
// e.g. in an instrumental bar.
type Message struct {
	Time   uint32 // ms when it appears
	Text   string
	Color  uint32 // 0xRRGGBB, or NoColor for the theme's default
	Bold   bool
	Italic bool
}

// IsSpacer reports a message without text: an empty line that only the
// log view shows (the focus view ignores it).
func (m Message) IsSpacer() bool { return strings.TrimSpace(m.Text) == "" }

// NoColor marks a message that uses the theme's default color.
const NoColor = 0xFF000000

// MessageColors are the colors the editor cycles through.
var MessageColors = []struct {
	Name string
	RGB  uint32
}{
	{"default", NoColor},
	{"red", 0xff5f5f}, {"orange", 0xffaf5f}, {"yellow", 0xffd75f}, {"green", 0x87d75f},
	{"cyan", 0x5fd7ff}, {"blue", 0x5f87ff}, {"purple", 0xaf87ff}, {"pink", 0xff87d7},
	{"gray", 0x8a8a8a}, {"white", 0xffffff},
}

// AddMessage inserts m keeping Messages sorted, and returns its index.
func (s *Song) AddMessage(m Message) int {
	i := sort.Search(len(s.Messages), func(i int) bool { return s.Messages[i].Time > m.Time })
	s.Messages = slices.Insert(s.Messages, i, m)
	return i
}

// SortMessages restores time order after a message's time changed and
// returns the new index of the message that was at index i.
func (s *Song) SortMessages(i int) int {
	m := s.Messages[i]
	s.Messages = slices.Delete(s.Messages, i, i+1)
	return s.AddMessage(m)
}

// voiceColors are the default lyric colors for well-known voices, matched
// against the words of the voice database name, ignoring case.
var voiceColors = []struct {
	voice string
	rgb   uint32
}{
	{"teto", 0xff5f5f},  // red
	{"gumi", 0x87d75f},  // green
	{"miku", 0x5fd7d7},  // teal
	{"luka", 0xff87d7},  // pink
	{"rin", 0xffd75f},   // yellow
	{"len", 0xffd75f},   // yellow
	{"kaito", 0x5f87ff}, // blue
}

// VoiceColor is the default lyric color for a SynthV voice, or 0 (the
// theme's default) for voices without one.
func VoiceColor(voice string) uint32 {
	words := strings.FieldsFunc(strings.ToLower(voice), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, c := range voiceColors {
		if slices.Contains(words, c.voice) {
			return c.rgb
		}
	}
	return 0
}

// MetaField is one piece of song information, e.g. {"artist", "Kasane Teto"}.
type MetaField struct{ Key, Value string }

// Well-known metadata keys, in the order the info screen shows them. Other
// keys found in a file are kept as they are.
var MetaKeys = []struct{ Key, Label string }{
	{"artist", "Artist"},
	{"album", "Album"},
	{"vocal", "Vocalist"},
	{"lyrics", "Lyrics by"},
	{"music", "Music by"},
	{"year", "Year"},
	{"comment", "Comment"},
}

// MetaValue returns the value for key, or "".
func (s *Song) MetaValue(key string) string {
	for _, m := range s.Meta {
		if m.Key == key {
			return m.Value
		}
	}
	return ""
}

// SetMeta sets key to value; an empty value removes the field.
func (s *Song) SetMeta(key, value string) {
	for i, m := range s.Meta {
		if m.Key == key {
			if value == "" {
				s.Meta = append(s.Meta[:i:i], s.Meta[i+1:]...)
			} else {
				s.Meta[i].Value = value
			}
			return
		}
	}
	if value != "" {
		s.Meta = append(s.Meta, MetaField{key, value})
	}
}

// DefaultDisplay is used for newly imported songs.
var DefaultDisplay = Display{Before: 1, After: 2, LeadIn: 400}

// LineMode selects how imported words are grouped into lines.
type LineMode uint8

const (
	ModeBars  LineMode = iota // a new line every N bars
	ModeRests                 // a new line at each rest/breath
)

func (m LineMode) String() string {
	if m == ModeRests {
		return "rests"
	}
	return "bars"
}

// ImportOptions control automatic line arrangement on import. They are
// stored in the .lyrb so a song can be re-parsed with the same settings.
type ImportOptions struct {
	Track       int      // track index, -1 = the track with the most notes
	Mode        LineMode //
	BarsPerLine int      // bars mode: bars per line
	Pickup      float64  // bars mode: beats before a bar line that already count as the next line
	LineGap     float64  // rests mode: seconds of rest that end a line
	StanzaGap   float64  // seconds of rest that start a new stanza (0 = off)
	GroupStanza bool     // each SynthV note group (Verse, Chorus, …) starts a stanza
	MaxChars    int      // longer lines are split in balanced parts (0 = never)
	// BackingTrack is the track whose lyrics are merged into the lines in
	// (brackets): track index + 1, so 0 (the zero value) means none.
	BackingTrack int
}

var DefaultImport = ImportOptions{
	Track: -1, Mode: ModeBars, BarsPerLine: 4, Pickup: 0.5,
	LineGap: 0.2, StanzaGap: 6, GroupStanza: true, MaxChars: 0,
}

// BeatsToBars converts a length in beats to bars at a bar position; it
// lets the arrangement honour time-signature changes.
type BeatsToBars func(bar, beats float64) float64

func fourFour(_, beats float64) float64 { return beats / 4 }

// skipLyric reports SynthV lyrics that are not displayable words, and
// whether they mark a phrase boundary (breaths and silences).
func skipLyric(s string) (skip, phrase bool) {
	switch strings.ToLower(s) {
	case "br", "sil", "sp", "pau":
		return true, true
	case "", "cl", "'":
		return true, false
	}
	return strings.HasPrefix(s, "."), false // raw phoneme input
}

// FromNotes builds a Song from SynthV notes, merging melismas ("-") and
// syllable continuations ("+") into the preceding word, then arranges lines
// (every N bars, or at rests), starts stanzas at note-group changes and long
// rests, and optionally splits lines that are too long. conv may be nil (4/4).
func FromNotes(notes []svp.Note, opt ImportOptions, conv BeatsToBars) *Song {
	if conv == nil {
		conv = fourFour
	}
	s := &Song{Display: DefaultDisplay, Import: opt}
	var group []int               // note group per token
	var bar []float64             // bar position per token
	phraseAfter := map[int]bool{} // token index followed by a breath/silence note
	for _, n := range notes {
		start, end := ms(n.Start), ms(n.End)
		lyric := n.Lyric
		if lyric == "-" || strings.HasPrefix(lyric, "+") {
			if k := len(s.Tokens) - 1; k >= 0 && end > s.Tokens[k].End {
				tk := &s.Tokens[k]
				tk.End = end
				if lyric == "-" {
					tk.Segs[len(tk.Segs)-1].End = end // held: same syllable
				} else {
					tk.Segs = append(tk.Segs, Seg{start, end}) // next syllable
				}
			}
			continue
		}
		if skip, phrase := skipLyric(lyric); skip {
			if phrase && len(s.Tokens) > 0 {
				phraseAfter[len(s.Tokens)-1] = true
			}
			continue
		}
		s.Tokens = append(s.Tokens, Token{Start: start, End: end, Text: lyric, Pitch: uint8(clamp(n.Pitch, 0, 127)),
			Segs: []Seg{{start, end}}})
		group = append(group, n.Group)
		bar = append(bar, n.Bar)
	}
	for i := range s.Tokens {
		if len(s.Tokens[i].Segs) < 2 {
			s.Tokens[i].Segs = nil // single syllable: Start..End says it all
		}
	}

	per := max(opt.BarsPerLine, 1)
	anchor, prevLine := 0, 0
	for i := range s.Tokens {
		t := &s.Tokens[i]
		gridBar := int(math.Floor(bar[i] + conv(bar[i], opt.Pickup)))
		gap := 0.0
		if i > 0 {
			prev := s.Tokens[i-1]
			t.Join = isCJK(prev.Text) && isCJK(t.Text)
			gap = float64(int64(t.Start)-int64(prev.End)) / 1000
		}
		switch {
		case i == 0, opt.StanzaGap > 0 && gap >= opt.StanzaGap, opt.GroupStanza && group[i] != group[i-1]:
			t.Break = BreakStanza
			anchor, prevLine = gridBar, 0 // the bar grid restarts at each stanza
		case opt.Mode == ModeRests:
			if gap >= opt.LineGap || phraseAfter[i-1] {
				t.Break = BreakLine
			}
		default:
			line := floorDiv(gridBar-anchor, per)
			if line != prevLine {
				t.Break = BreakLine
				prevLine = line
			}
		}
	}

	if opt.MaxChars > 0 {
		for lo := 0; lo < len(s.Tokens); {
			hi := lo + 1
			for hi < len(s.Tokens) && s.Tokens[hi].Break == BreakNone {
				hi++
			}
			s.splitLong(lo, hi, opt.MaxChars)
			lo = hi
		}
	}
	s.Touch()
	return s
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// splitLong breaks tokens [lo,hi) into lines of at most maxChars, choosing
// split points that balance the halves and favour small rests.
func (s *Song) splitLong(lo, hi, maxChars int) {
	total := s.width(lo, hi)
	if total <= maxChars || hi-lo < 2 {
		return
	}
	best, bestScore := -1, 0.0
	for k := lo + 1; k < hi; k++ {
		gap := float64(int64(s.Tokens[k].Start)-int64(s.Tokens[k-1].End)) / 1000
		imbalance := float64(abs(s.width(lo, k)-s.width(k, hi))) / float64(total)
		score := 4*max(gap, 0) - imbalance
		if s.Tokens[k].Join {
			score -= 10 // never split a glued word
		}
		if best < 0 || score > bestScore {
			best, bestScore = k, score
		}
	}
	s.Tokens[best].Break = BreakLine
	s.splitLong(lo, best, maxChars)
	s.splitLong(best, hi, maxChars)
}

// width is the display length in runes of tokens [lo,hi) joined as a line.
func (s *Song) width(lo, hi int) int {
	n := 0
	for i := lo; i < hi; i++ {
		n += len([]rune(s.Tokens[i].Text))
		if i > lo && !s.Tokens[i].Join {
			n++
		}
	}
	return n
}

func abs(v int) int { return max(v, -v) }

// AddBacking merges backing-vocal tokens into the arranged lines. back is
// split into phrases at its breaks (each token with a Break starts one).
// Backing words that only double a lead word (same text, sung at the same
// time) are dropped. Each phrase joins, as a whole, the line its first word
// falls in; one starting in the gap between two lines joins the nearer of
// them. Within a line, lead words and phrases are ordered by start time
// (lead first on ties), and the line's break moves to whichever comes first.
func (s *Song) AddBacking(back []Token) {
	lines := s.Lines()
	if len(lines) == 0 {
		return
	}
	var phrases [][]Token
	for i, b := range back {
		if i == 0 || b.Break != BreakNone {
			phrases = append(phrases, nil)
		}
		if !s.doubles(b) {
			b.Backing, b.Break = true, BreakNone
			phrases[len(phrases)-1] = append(phrases[len(phrases)-1], b)
		}
	}

	// A line's parts: single lead tokens and whole backing phrases.
	parts := make([][][]Token, len(lines))
	for i, l := range lines {
		for _, t := range s.Tokens[l.First : l.First+l.Count] {
			parts[i] = append(parts[i], []Token{t})
		}
	}
	for _, ph := range phrases {
		if len(ph) == 0 {
			continue
		}
		at := ph[0].Start
		li := sort.Search(len(lines), func(i int) bool { return lines[i].Start > at }) - 1
		switch {
		case li < 0:
			li = 0
		case li+1 < len(lines) && at >= lines[li].End && lines[li+1].Start-at < at-lines[li].End:
			li++ // in a gap, closer to the next line
		}
		parts[li] = append(parts[li], ph)
	}

	toks := make([]Token, 0, len(s.Tokens)+len(back))
	for _, ps := range parts {
		brk := ps[0][0].Break
		sort.SliceStable(ps, func(a, b int) bool { return ps[a][0].Start < ps[b][0].Start })
		first := len(toks)
		for _, p := range ps {
			for k, t := range p {
				t.Break = BreakNone
				if k == 0 && len(toks) > first && (t.Backing || toks[len(toks)-1].Backing) {
					t.Join = isCJK(toks[len(toks)-1].Text) && isCJK(t.Text)
				}
				toks = append(toks, t)
			}
		}
		toks[first].Break = brk
		if toks[first].Backing {
			toks[first].Join = false
		}
	}
	s.Tokens = toks
	s.Touch()
}

// doubles reports whether backing token b sings the same word as a lead
// token at the same time (a harmony or double, not a separate lyric).
func (s *Song) doubles(b Token) bool {
	for _, t := range s.Tokens {
		if t.Start < b.End && t.End > b.Start && strings.EqualFold(t.Text, b.Text) {
			return true
		}
	}
	return false
}

// Touch invalidates derived data after edits to Tokens.
func (s *Song) Touch() { s.dirty = true }

// Lines returns the derived line list (cached).
func (s *Song) Lines() []Line {
	if s.lines != nil && !s.dirty {
		return s.lines
	}
	s.lines = s.lines[:0]
	stanza := -1
	seen := false // current line has a visible token
	for i, t := range s.Tokens {
		if i == 0 || t.Break != BreakNone {
			st := i == 0 || t.Break == BreakStanza
			if st {
				stanza++
			}
			var at uint32 // lines with only hidden tokens sit at the previous end
			if n := len(s.lines); n > 0 {
				at = s.lines[n-1].End
			}
			s.lines = append(s.lines, Line{Start: at, End: at, First: i, Stanza: st, Stanzanum: stanza})
			seen = false
		}
		l := &s.lines[len(s.lines)-1]
		l.Count++
		if t.Hidden {
			continue
		}
		if !seen {
			l.Start, l.End, seen = t.Start, t.End, true
		} else {
			l.Start = min(l.Start, t.Start)
			l.End = max(l.End, t.End)
		}
	}
	s.dirty = false
	return s.lines
}

// LineOf returns the index of the line containing token i.
func (s *Song) LineOf(tok int) int {
	ls := s.Lines()
	lo, hi := 0, len(ls)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if ls[mid].First <= tok {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// Duration is the end of the last token in ms.
func (s *Song) Duration() uint32 {
	var d uint32
	for _, t := range s.Tokens {
		d = max(d, t.End)
	}
	return d
}

func ms(sec float64) uint32 {
	if sec <= 0 {
		return 0
	}
	return uint32(sec*1000 + 0.5)
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

func isCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}
