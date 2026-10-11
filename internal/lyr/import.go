package lyr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/graweedman/lyric-engine/internal/svp"
)

// Import arranges one track of a parsed SynthV project.
func Import(p *svp.Project, opt ImportOptions) (*Song, error) {
	ti := opt.Track
	if ti < 0 {
		ti = p.BestTrack()
	}
	if ti < 0 || ti >= len(p.Tracks) {
		return nil, fmt.Errorf("track %d not found (project has %d tracks)", ti, len(p.Tracks))
	}
	s := FromNotes(p.Tracks[ti].Notes, opt, p.BeatsToBars)
	if len(s.Tokens) == 0 {
		return nil, fmt.Errorf("track %d (%s) has no lyric notes", ti, p.Tracks[ti].Name)
	}
	s.Display.LeadColor = VoiceColor(p.Tracks[ti].Voice)
	bi := opt.BackingTrack - 1
	s.AnalyzeProject(p, ti, bi)
	if bi >= 0 && bi != ti {
		if bi >= len(p.Tracks) {
			return nil, fmt.Errorf("backing track %d not found (project has %d tracks)", bi, len(p.Tracks))
		}
		// Split the backing track into phrases at rests and breaths.
		bopt := opt
		bopt.Mode, bopt.MaxChars = ModeRests, 0
		s.AddBacking(FromNotes(p.Tracks[bi].Notes, bopt, p.BeatsToBars).Tokens)
		s.Display.BackColor = VoiceColor(p.Tracks[bi].Voice)
	}
	return s, nil
}

// LyrbPath is the default .lyrb location for a source file.
func LyrbPath(svpPath string) string {
	return strings.TrimSuffix(svpPath, filepath.Ext(svpPath)) + ".lyrb"
}

// GuessAudio returns <name>.wav next to the source file, if it exists.
func GuessAudio(svpPath string) string {
	guess := strings.TrimSuffix(svpPath, filepath.Ext(svpPath)) + ".wav"
	if _, err := os.Stat(guess); err == nil {
		return guess
	}
	return ""
}

// SetSource records the source and audio files relative to the .lyrb path,
// and defaults the title to the source file name.
func (s *Song) SetSource(lyrbPath, svpPath, wavPath string) {
	if s.Title == "" {
		s.Title = strings.TrimSuffix(filepath.Base(svpPath), filepath.Ext(svpPath))
	}
	s.SourcePath = RelTo(lyrbPath, svpPath)
	s.AudioPath = ""
	if wavPath != "" {
		s.AudioPath = RelTo(lyrbPath, wavPath)
	}
}

// Resolve turns a path stored in the song into one usable from the working
// directory; stored paths are relative to the .lyrb file.
func Resolve(lyrbPath, stored string) string {
	if stored == "" || filepath.IsAbs(stored) {
		return stored
	}
	return filepath.Join(filepath.Dir(lyrbPath), filepath.FromSlash(stored))
}

// RelTo expresses target relative to the directory of file, when possible.
func RelTo(file, target string) string {
	absFile, err1 := filepath.Abs(file)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return target
	}
	if r, err := filepath.Rel(filepath.Dir(absFile), absTarget); err == nil {
		return filepath.ToSlash(r)
	}
	return absTarget
}
