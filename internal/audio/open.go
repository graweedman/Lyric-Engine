package audio

import "fmt"

// Open loads the WAV at path and returns a player for it. Any failure is
// reported as a warning, with a silent clock of fallbackMs as the player,
// so the lyric display keeps working without audio.
func Open(path string, fallbackMs int64) (*Player, string) {
	if path == "" {
		p, _ := NewPlayer(nil, fallbackMs)
		return p, "no audio file — using a silent clock"
	}
	pcm, err := LoadWAV(path)
	if err != nil {
		p, _ := NewPlayer(nil, fallbackMs)
		return p, fmt.Sprintf("audio %s: %v — using a silent clock", path, err)
	}
	p, err := NewPlayer(pcm, fallbackMs)
	if err != nil {
		return p, fmt.Sprintf("audio output: %v — using a silent clock", err)
	}
	return p, ""
}
