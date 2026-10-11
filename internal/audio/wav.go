// Package audio decodes WAV files into memory and plays them with a
// sample-accurate clock that the lyric display syncs to.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

// PCM is decoded stereo audio as float32 frames.
type PCM struct {
	Rate    int
	Samples [][2]float32
}

// LoadWAV decodes integer PCM (8/16/24/32-bit) and IEEE float (32/64-bit)
// WAV files, including WAVE_FORMAT_EXTENSIBLE. Mono is duplicated to stereo;
// channels beyond two are dropped.
func LoadWAV(path string) (*PCM, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("wav: not a RIFF/WAVE file")
	}
	var (
		format, channels, bits uint16
		rate                   uint32
		pcm                    []byte
		haveFmt                bool
	)
	le := binary.LittleEndian
	for p := 12; p+8 <= len(data); {
		id, size := string(data[p:p+4]), int(le.Uint32(data[p+4:]))
		body := data[p+8:]
		if size > len(body) {
			size = len(body) // tolerate truncated files
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("wav: short fmt chunk")
			}
			format, channels = le.Uint16(body[0:]), le.Uint16(body[2:])
			rate, bits = le.Uint32(body[4:]), le.Uint16(body[14:])
			if format == 0xFFFE && size >= 26 {
				format = le.Uint16(body[24:]) // sub-format GUID's first two bytes
			}
			haveFmt = true
		case "data":
			pcm = body
		}
		p += 8 + size + size%2
	}
	if !haveFmt || pcm == nil {
		return nil, errors.New("wav: missing fmt or data chunk")
	}
	if channels == 0 {
		return nil, errors.New("wav: zero channels")
	}

	var read func([]byte) float32
	switch {
	case format == 1 && bits == 8:
		read = func(b []byte) float32 { return (float32(b[0]) - 128) / 128 }
	case format == 1 && bits == 16:
		read = func(b []byte) float32 { return float32(int16(le.Uint16(b))) / 32768 }
	case format == 1 && bits == 24:
		read = func(b []byte) float32 {
			v := int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24) >> 8
			return float32(v) / 8388608
		}
	case format == 1 && bits == 32:
		read = func(b []byte) float32 { return float32(int32(le.Uint32(b))) / 2147483648 }
	case format == 3 && bits == 32:
		read = func(b []byte) float32 { return math.Float32frombits(le.Uint32(b)) }
	case format == 3 && bits == 64:
		read = func(b []byte) float32 { return float32(math.Float64frombits(le.Uint64(b))) }
	default:
		return nil, fmt.Errorf("wav: unsupported format %d / %d-bit", format, bits)
	}

	bps := int(bits / 8)
	frame := bps * int(channels)
	n := len(pcm) / frame
	out := &PCM{Rate: int(rate), Samples: make([][2]float32, n)}
	for i := 0; i < n; i++ {
		f := pcm[i*frame:]
		l := read(f)
		r := l
		if channels > 1 {
			r = read(f[bps:])
		}
		out.Samples[i] = [2]float32{l, r}
	}
	return out, nil
}

// Duration in milliseconds.
func (p *PCM) DurationMs() int64 {
	return int64(len(p.Samples)) * 1000 / int64(p.Rate)
}

// Resample converts to another sample rate with linear interpolation.
func (p *PCM) Resample(rate int) *PCM {
	if rate == p.Rate || len(p.Samples) == 0 {
		return p
	}
	n := int(int64(len(p.Samples)) * int64(rate) / int64(p.Rate))
	out := &PCM{Rate: rate, Samples: make([][2]float32, n)}
	step := float64(p.Rate) / float64(rate)
	last := len(p.Samples) - 1
	for i := range out.Samples {
		x := float64(i) * step
		j := int(x)
		if j >= last {
			out.Samples[i] = p.Samples[last]
			continue
		}
		f := float32(x - float64(j))
		a, b := p.Samples[j], p.Samples[j+1]
		out.Samples[i] = [2]float32{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}
	}
	return out
}
