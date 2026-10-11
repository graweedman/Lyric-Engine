// Command mkdemo writes examples/demo.svp and examples/demo.wav: a short
// SynthV-style project (with a tempo change, melisma, syllable
// continuation, breath and a referenced note group) and a matching sine
// tone rendering, for trying the editor without a real project.
package main

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/graweedman/lyric-engine/internal/svp"
)

const q = int64(svp.BlicksPerQuarter)

type note struct {
	Onset    int64  `json:"onset"`
	Duration int64  `json:"duration"`
	Lyrics   string `json:"lyrics"`
	Phonemes string `json:"phonemes"`
	Pitch    int    `json:"pitch"`
}

func main() {
	out := "examples"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}

	// Verse 1 in the main group: "twin-kle twin-kle lit-tle star ..." (8th = q/2)
	var main []note
	at := int64(4 * q) // one bar of intro
	add := func(dst *[]note, words string, pitches []int, eighths []int) {
		for i, w := range strings.Fields(words) {
			d := int64(eighths[i]) * q / 2
			*dst = append(*dst, note{Onset: at, Duration: d - q/16, Lyrics: w, Pitch: pitches[i]})
			at += d
		}
	}
	add(&main, "twinkle + twinkle + little + star br",
		[]int{60, 60, 67, 67, 69, 69, 67, 0}, []int{2, 2, 2, 2, 2, 2, 3, 1})
	add(&main, "how I wonder + what you are -",
		[]int{65, 65, 64, 64, 62, 62, 60, 62}, []int{2, 2, 2, 2, 2, 2, 2, 2})
	at += 8 * q // instrumental gap -> new stanza

	// Verse 2 lives in a library group placed by reference.
	groupStart := at
	at = 0
	var grp []note
	add(&grp, "up above + the world so high br",
		[]int{67, 67, 65, 65, 64, 64, 62, 0}, []int{2, 2, 2, 2, 2, 2, 3, 1})
	add(&grp, "like a diamond + in the sky",
		[]int{67, 67, 65, 65, 64, 64, 62}, []int{2, 2, 2, 2, 2, 2, 4})

	proj := map[string]any{
		"version": 153,
		"time": map[string]any{
			"meter": []any{map[string]any{"index": 0, "numerator": 4, "denominator": 4}},
			"tempo": []any{
				map[string]any{"position": 0, "bpm": 100.0},
				map[string]any{"position": groupStart, "bpm": 120.0},
			},
		},
		"library": []any{map[string]any{"uuid": "verse-2", "name": "verse 2", "notes": grp}},
		"tracks": []any{
			map[string]any{"name": "Vocal", "mainGroup": map[string]any{"uuid": "main", "name": "main", "notes": main},
				"mainRef": map[string]any{"groupID": "main", "blickOffset": 0},
				"groups":  []any{map[string]any{"groupID": "verse-2", "blickOffset": groupStart, "pitchOffset": 0}}},
		},
	}
	data, _ := json.MarshalIndent(proj, "", "  ")
	data = append(data, 0) // SynthV writes a trailing NUL
	svpPath := filepath.Join(out, "demo.svp")
	if err := os.WriteFile(svpPath, data, 0o644); err != nil {
		log.Fatal(err)
	}

	// Render the parsed notes as sine tones so timing matches exactly.
	p, err := svp.Parse(data)
	if err != nil {
		log.Fatal(err)
	}
	const rate = 44100
	notes := p.Tracks[0].Notes
	total := int((notes[len(notes)-1].End + 2) * rate)
	pcm := make([]int16, total)
	for _, n := range notes {
		if n.Lyric == "br" {
			continue
		}
		f := 440 * math.Pow(2, float64(n.Pitch-69)/12)
		a, b := int(n.Start*rate), min(int(n.End*rate), total)
		for i := a; i < b; i++ {
			k := float64(i - a)
			env := math.Min(1, math.Min(k/400, float64(b-i)/800))
			pcm[i] += int16(0.25 * 32767 * env * math.Sin(2*math.Pi*f*k/rate))
		}
	}
	if err := writeWAV(filepath.Join(out, "demo.wav"), rate, pcm); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s and demo.wav (%d notes)", svpPath, len(notes))
}

func writeWAV(path string, rate int, pcm []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	le := binary.LittleEndian
	size := uint32(len(pcm) * 2)
	hdr := []any{
		[]byte("RIFF"), 36 + size, []byte("WAVE"),
		[]byte("fmt "), uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16),
		[]byte("data"), size,
	}
	for _, v := range hdr {
		if err := binary.Write(f, le, v); err != nil {
			return err
		}
	}
	return binary.Write(f, le, pcm)
}
