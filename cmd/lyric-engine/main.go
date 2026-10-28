// Command lyric-engine imports Synthesizer V and OpenUtau projects into the .lyrb lyric
// format, arranges them in a TUI editor, and plays them as a terminal lyric
// display synced to a WAV.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/graweedman/lyric-engine/internal/audio"
	"github.com/graweedman/lyric-engine/internal/lyr"
	"github.com/graweedman/lyric-engine/internal/obs"
	"github.com/graweedman/lyric-engine/internal/render"
	"github.com/graweedman/lyric-engine/internal/svp"
	"github.com/graweedman/lyric-engine/internal/tui"
	"github.com/graweedman/lyric-engine/internal/window"
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `lyric-engine — SynthV / OpenUtau lyrics → terminal lyric display

usage:
  lyric-engine                              browse for a .svp/.ustx/.lyrb (parse settings → editor)
  lyric-engine <song.lyrb>                  play it in a 7:9 window (also for drag-and-drop)
  lyric-engine edit   <song.svp|song.lyrb>  open a file directly in the TUI (.ustx works too)
  lyric-engine play   <song.lyrb> [flags]   full-screen lyric display synced to audio
  lyric-engine import <song.svp> [flags]    convert to .lyrb without the TUI
  lyric-engine info   <song.svp|song.lyrb>  print tracks / arranged lines
  lyric-engine txt    <song.svp|song.lyrb>  export the lyrics as a plain .txt
  lyric-engine version                      print the version

run "lyric-engine <command> -h" for flags.
`

func main() {
	var err error
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		err = runTUI(tui.NewApp("."))
	case "import":
		err = cmdImport(os.Args[2:])
	case "edit":
		err = cmdEdit(os.Args[2:])
	case "play":
		err = cmdPlay(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "txt":
		err = cmdTxt(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("lyric-engine", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		// A bare file (also what Windows passes for drag-and-drop or a file
		// association): .lyrb plays, .svp / .ustx opens the editor.
		switch {
		case strings.EqualFold(filepath.Ext(cmd), ".lyrb"):
			err = cmdPlay(os.Args[1:])
		case svp.IsProject(cmd):
			err = cmdEdit(os.Args[1:])
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
			os.Exit(2)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runTUI(m tea.Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	out := fs.String("o", "", "output .lyrb (default: next to the .svp)")
	force := fs.Bool("f", false, "overwrite an existing output file")
	wav := fs.String("audio", "", "WAV file to sync with (default: <song>.wav next to the .svp, if present)")
	title := fs.String("title", "", "song title (default: file name)")
	importOptions := importFlags(fs)
	in, err := parseWithFile(fs, args)
	if err != nil {
		return err
	}
	opt, err := importOptions()
	if err != nil {
		return err
	}
	if *out == "" {
		*out = lyr.LyrbPath(in)
	}
	if _, err := os.Stat(*out); err == nil && !*force {
		return fmt.Errorf("%s exists (use -f to overwrite)", *out)
	}
	proj, err := svp.ParseFile(in)
	if err != nil {
		return err
	}
	song, err := lyr.Import(proj, opt)
	if err != nil {
		return err
	}
	if *wav == "" {
		*wav = lyr.GuessAudio(in)
	}
	song.Title = *title
	song.SetSource(*out, in, *wav)
	if err := lyr.Save(*out, song); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d words in %d lines", *out, len(song.Tokens), len(song.Lines()))
	if song.AudioPath != "" {
		fmt.Printf(", audio %s", song.AudioPath)
	}
	fmt.Println()
	return nil
}

// importFlags registers the .svp parse settings on fs. The returned function
// builds the options once fs has been parsed.
func importFlags(fs *flag.FlagSet) func() (lyr.ImportOptions, error) {
	opt := lyr.DefaultImport
	mode := fs.String("mode", "bars", `line mode: "bars" (every N bars) or "rests" (at rests/breaths)`)
	fs.IntVar(&opt.Track, "track", opt.Track, "track index (-1 = the track with the most notes)")
	fs.IntVar(&opt.BarsPerLine, "bars", opt.BarsPerLine, "bars per line (bars mode)")
	fs.Float64Var(&opt.Pickup, "pickup", opt.Pickup, "beats before a bar line that count as the next line (bars mode)")
	fs.Float64Var(&opt.LineGap, "line-gap", opt.LineGap, "seconds of rest that start a new line (rests mode)")
	fs.Float64Var(&opt.StanzaGap, "stanza-gap", opt.StanzaGap, "seconds of rest that start a new stanza (0 = off)")
	fs.BoolVar(&opt.GroupStanza, "group-stanza", opt.GroupStanza, "start a stanza at each SynthV note group (Verse, Chorus, ...)")
	fs.IntVar(&opt.MaxChars, "max-chars", opt.MaxChars, "split longer lines in balanced parts (0 = off)")
	backing := fs.Int("backing", -1, "track index whose lyrics are merged into the lines in (brackets) (-1 = none)")
	return func() (lyr.ImportOptions, error) {
		opt.BackingTrack = *backing + 1
		switch *mode {
		case "bars":
			opt.Mode = lyr.ModeBars
		case "rests":
			opt.Mode = lyr.ModeRests
		default:
			return opt, fmt.Errorf("-mode must be bars or rests")
		}
		return opt, nil
	}
}

// cmdTxt writes the lyrics of a .svp (parsed with the import flags) or of an
// arranged .lyrb as a plain text file.
func cmdTxt(args []string) error {
	fs := flag.NewFlagSet("txt", flag.ExitOnError)
	out := fs.String("o", "", `output .txt (default: next to the input; "-" = stdout)`)
	force := fs.Bool("f", false, "overwrite an existing output file")
	stamps := fs.Bool("timestamps", false, "prefix each line with its start time as [mm:ss.xx]")
	importOptions := importFlags(fs)
	in, err := parseWithFile(fs, args)
	if err != nil {
		return err
	}
	var song *lyr.Song
	if svp.IsProject(in) {
		opt, err := importOptions()
		if err != nil {
			return err
		}
		proj, err := svp.ParseFile(in)
		if err != nil {
			return err
		}
		if song, err = lyr.Import(proj, opt); err != nil {
			return err
		}
	} else if song, err = lyr.Open(in); err != nil {
		return err
	}
	text := render.PlainText(song, *stamps)
	if *out == "-" {
		_, err := fmt.Print(text)
		return err
	}
	if *out == "" {
		*out = strings.TrimSuffix(in, filepath.Ext(in)) + ".txt"
	}
	if _, err := os.Stat(*out); err == nil && !*force {
		return fmt.Errorf("%s exists (use -f to overwrite)", *out)
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d lines\n", *out, strings.Count(text, "\n"))
	return nil
}

func cmdEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	wav := fs.String("audio", "", "WAV file to use")
	in, err := parseWithFile(fs, args)
	if err != nil {
		return err
	}
	app, err := tui.NewAppWith(in, *wav)
	if err != nil {
		return err
	}
	return runTUI(app)
}

func cmdPlay(args []string) error {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	wav := fs.String("audio", "", "override the WAV file")
	noAudio := fs.Bool("no-audio", false, "run on a silent clock")
	noStatus := fs.Bool("no-status", false, "hide the time/progress line")
	large := fs.Bool("large", false, "draw the focus view in double-size text (as the editor's L)")
	aspect := fs.String("aspect", "7:9", `resize the terminal window to width:height at full screen height ("off" keeps the window); the display always uses this shape`)
	dock := fs.String("dock", "right", "where the resized window goes: left, right or center")
	obsAddr := fs.String("obs", "localhost:4455", `OBS WebSocket address to record the playback with ("off" disables)`)
	obsPass := fs.String("obs-password", os.Getenv("OBS_WEBSOCKET_PASSWORD"), "OBS WebSocket password (default $OBS_WEBSOCKET_PASSWORD)")
	in, err := parseWithFile(fs, args)
	if err != nil {
		return err
	}
	aw, ah, resize := 7, 9, *aspect != "off"
	if resize {
		if _, err := fmt.Sscanf(*aspect, "%d:%d", &aw, &ah); err != nil || aw <= 0 || ah <= 0 {
			return fmt.Errorf(`-aspect must look like 7:9 or be "off"`)
		}
	}
	song, err := lyr.Open(in)
	if err != nil {
		return err
	}
	if *wav != "" {
		song.AudioPath = lyr.RelTo(in, *wav)
	}
	if *noAudio {
		song.AudioPath = ""
	}
	if *large {
		song.Display.Large = true
	}
	p, warn := audio.Open(lyr.Resolve(in, song.AudioPath), int64(song.Duration())+2000)
	if resize {
		restore, err := window.Fit(aw, ah, *dock)
		if err != nil {
			warn = strings.TrimPrefix(warn+"; window resize: "+err.Error(), "; ")
		} else {
			defer restore()
		}
	}
	// Record with OBS from just before playback starts until it ends or
	// is quit.
	var rec *obs.Client
	if *obsAddr != "off" {
		c, err := obs.Connect(*obsAddr, *obsPass)
		if err == nil {
			defer c.Close()
			if err = c.StartRecord(); err == nil {
				rec = c
			}
		}
		if err != nil {
			warn = strings.TrimPrefix(warn+"; no recording: "+err.Error(), "; ")
		}
	}
	pl := tui.NewPlayer(song, p, !*noStatus, warn, aw, ah, *dock)
	if rec != nil {
		var stopErr error
		pl.OnEnd = func() { stopErr = rec.StopRecord() }
		defer func() {
			if pl.OnEnd != nil { // the display failed before playback ended
				pl.OnEnd()
			}
			if stopErr != nil {
				fmt.Fprintln(os.Stderr, "OBS:", stopErr)
			}
		}()
	}
	return runTUI(pl)
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	in, err := parseWithFile(fs, args)
	if err != nil {
		return err
	}
	if svp.IsProject(in) {
		proj, err := svp.ParseFile(in)
		if err != nil {
			return err
		}
		for _, t := range proj.Tracks {
			fmt.Printf("track %d  %-24q %4d notes  %s\n", t.Index, t.Name, len(t.Notes), t.Voice)
		}
		return nil
	}
	song, err := lyr.Open(in)
	if err != nil {
		return err
	}
	im := song.Import
	fmt.Printf("title: %s\nsource: %s\naudio: %s (offset %+d ms)\ndisplay: %d above / %d below, lead-in %d ms\n",
		song.Title, song.SourcePath, song.AudioPath, song.AudioOffsetMs, song.Display.Before, song.Display.After, song.Display.LeadIn)
	fmt.Printf("parsed with: %s mode, %d bars/line, pickup %.2f beats, stanza per group %v, stanza gap %.1fs\n",
		im.Mode, im.BarsPerLine, im.Pickup, im.GroupStanza, im.StanzaGap)
	if im.BackingTrack > 0 {
		fmt.Printf("backing vocals: track %d, in (brackets)\n", im.BackingTrack-1)
	}
	if d := song.Display; d.LeadColor != 0 || d.BackColor != 0 {
		fmt.Printf("lyric colors: lead #%06x, backing #%06x (000000 = default)\n", d.LeadColor, d.BackColor)
	}
	if an := song.Analysis; len(an.Tempo) > 0 || an.Key.Known() {
		fmt.Printf("analysis: %.1f bpm", an.BPMAt(0))
		if len(an.Tempo) > 1 {
			fmt.Printf(" (%d tempo changes)", len(an.Tempo)-1)
		}
		m, _, _ := an.BarAt(0)
		fmt.Printf(", %d/%d, key %s (%.0f%%)", m.Num, m.Den, an.Key, an.Key.Score*100)
		if an.Low > 0 {
			fmt.Printf(", range %s–%s", lyr.NoteName(an.Low), lyr.NoteName(an.High))
		}
		fmt.Println()
	}
	if song.Display.Large {
		fmt.Printf("view: %s, large text\n", song.Display.View)
	} else {
		fmt.Printf("view: %s\n", song.Display.View)
	}
	for _, m := range song.Meta {
		fmt.Printf("%s: %s\n", m.Key, m.Value)
	}
	fmt.Println()
	// Lines and story messages in time order.
	msgs := song.Messages
	printMsgs := func(before uint32) {
		for len(msgs) > 0 && msgs[0].Time <= before {
			if msgs[0].IsSpacer() {
				fmt.Printf("  ↵       %7.2fs  (empty line, log view)\n", float64(msgs[0].Time)/1000)
			} else {
				fmt.Printf("  ✦       %7.2fs  %s\n", float64(msgs[0].Time)/1000, msgs[0].Text)
			}
			msgs = msgs[1:]
		}
	}
	defer printMsgs(^uint32(0))
	for i, l := range song.Lines() {
		printMsgs(l.Start)
		mark := " "
		if l.Stanza {
			mark = "§"
		}
		fmt.Printf("%s %3d  %7.2fs  %s\n", mark, i+1, float64(l.Start)/1000, render.LineText(song, i))
	}
	return nil
}

// parseWithFile parses flags that may appear before or after the one
// positional file argument.
func parseWithFile(fs *flag.FlagSet, args []string) (string, error) {
	var file string
	for {
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		if fs.NArg() == 0 {
			break
		}
		if file != "" {
			return "", errors.New("expected exactly one file argument")
		}
		file, args = fs.Arg(0), fs.Args()[1:]
	}
	if file == "" {
		return "", fmt.Errorf("%s: missing file argument", fs.Name())
	}
	return file, nil
}
