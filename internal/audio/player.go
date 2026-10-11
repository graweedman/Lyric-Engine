package audio

import (
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
)

// The output device is opened once per process (beep's speaker cannot be
// re-initialised) at the first file's sample rate; later files with a
// different rate are resampled to it. One Player is active at a time.
var device struct {
	once    sync.Once
	err     error
	rate    int
	latency int64 // ms of audio buffered between us and the speaker

	mu     sync.Mutex
	active *Player
}

func openDevice(rate int) error {
	device.once.Do(func() {
		sr := beep.SampleRate(rate)
		buf := sr.N(time.Second / 25)
		if device.err = speaker.Init(sr, buf); device.err != nil {
			return
		}
		device.rate = rate
		// beep splits the buffer between driver and player; on average about
		// half of it is queued ahead of what is audible.
		device.latency = int64(sr.D(buf/2) / time.Millisecond)
		speaker.Play(beep.StreamerFunc(feed))
	})
	return device.err
}

// feed is the single device streamer; it pulls from the active player.
func feed(out [][2]float64) (int, bool) {
	device.mu.Lock()
	p := device.active
	device.mu.Unlock()
	if p == nil {
		clear(out)
		return len(out), true
	}
	p.stream(out)
	return len(out), true
}

// Player is a seekable transport with a clock. It plays the loaded WAV when
// an output device is available; otherwise (or with no audio) it runs a
// silent wall clock so the display can still be previewed.
type Player struct {
	mu       sync.Mutex
	pcm      *PCM
	pos      int // next sample index fed to the device
	playing  bool
	latency  int64
	duration int64

	// silent clock
	silent   bool
	baseMs   int64
	baseWall time.Time
}

// NewPlayer creates a player and makes it the active one. pcm may be nil;
// durationMs is used for the silent clock in that case.
func NewPlayer(pcm *PCM, durationMs int64) (*Player, error) {
	p := &Player{pcm: pcm, duration: durationMs, silent: pcm == nil}
	if pcm != nil {
		p.duration = pcm.DurationMs()
		if err := openDevice(pcm.Rate); err != nil {
			p.silent = true
			return p, err
		}
		if pcm.Rate != device.rate {
			p.pcm = pcm.Resample(device.rate)
		}
		p.latency = device.latency
	}
	device.mu.Lock()
	if old := device.active; old != nil {
		old.Pause()
	}
	device.active = nil
	if !p.silent {
		device.active = p
	}
	device.mu.Unlock()
	return p, nil
}

func (p *Player) stream(out [][2]float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range out {
		if p.playing && p.pos < len(p.pcm.Samples) {
			s := p.pcm.Samples[p.pos]
			out[i] = [2]float64{float64(s[0]), float64(s[1])}
			p.pos++
		} else {
			out[i] = [2]float64{}
		}
	}
	if p.pos >= len(p.pcm.Samples) {
		p.playing = false
	}
}

// Silent reports whether audio output is unavailable.
func (p *Player) Silent() bool { return p.silent }

// DurationMs is the audio length (or lyric length for the silent clock).
func (p *Player) DurationMs() int64 { return p.duration }

// Playing reports transport state.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.silent && p.playing && p.posLocked() >= p.duration {
		p.playing = false
	}
	return p.playing
}

// Position is the audible position in ms.
func (p *Player) Position() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.posLocked()
}

func (p *Player) posLocked() int64 {
	if p.silent {
		if !p.playing {
			return p.baseMs
		}
		return p.baseMs + time.Since(p.baseWall).Milliseconds()
	}
	ms := int64(p.pos) * 1000 / int64(p.pcm.Rate)
	if p.playing {
		ms -= p.latency
	}
	return max(ms, 0)
}

// Toggle play/pause.
func (p *Player) Toggle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.posLocked()
	if now >= p.duration && !p.playing {
		now = 0
	}
	p.playing = !p.playing
	p.seekLocked(now)
}

// Pause stops playback.
func (p *Player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing {
		now := p.posLocked()
		p.playing = false
		p.seekLocked(now)
	}
}

// SeekTo jumps to ms (clamped).
func (p *Player) SeekTo(ms int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seekLocked(ms)
}

func (p *Player) seekLocked(ms int64) {
	ms = max(0, min(ms, p.duration))
	if p.silent {
		p.baseMs, p.baseWall = ms, time.Now()
		return
	}
	p.pos = min(int(ms*int64(p.pcm.Rate)/1000), len(p.pcm.Samples))
}
