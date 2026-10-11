package audio

import (
	"os"
	"testing"
	"time"
)

// Plays the (silent) intro of the demo WAV on the real output device.
// Run with LYRIC_AUDIO_TEST=1 after `go run ./cmd/mkdemo`.
func TestPlayerRealDevice(t *testing.T) {
	if os.Getenv("LYRIC_AUDIO_TEST") == "" {
		t.Skip("set LYRIC_AUDIO_TEST=1 to use the audio device")
	}
	pcm, err := LoadWAV("../../examples/demo.wav")
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlayer(pcm, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Silent() {
		t.Fatal("expected a real device")
	}
	p.Toggle()
	time.Sleep(time.Second)
	p.Pause()
	if pos := p.Position(); pos < 850 || pos > 1150 {
		t.Errorf("position after 1s = %d ms", pos)
	}
	p.SeekTo(5000)
	if pos := p.Position(); pos != 5000 {
		t.Errorf("seek: %d", pos)
	}
}
