# lyric-engine

SynthV (`.svp`) lyrics → arranged `.lyrb` file → karaoke-style lyric display in the terminal, synced to a WAV.

## Install

```sh
go install github.com/graweedman/lyric-engine/cmd/lyric-engine@latest
```

Or build from a clone:

```sh
go build -o lyric-engine.exe ./cmd/lyric-engine
go run ./cmd/mkdemo                          # optional: examples/demo.svp + demo.wav

lyric-engine                                 # browse → parse settings → editor
lyric-engine song.lyrb                       # play it in a 7:9 window (drag-and-drop works too)
lyric-engine edit   song.svp|song.lyrb       # open a file directly
lyric-engine play   song.lyrb                # final full-screen display
lyric-engine import song.svp [-bars 4 ...]   # convert without the TUI
lyric-engine info   song.svp|song.lyrb       # list tracks / arranged lines
lyric-engine txt    song.svp|song.lyrb       # export the lyrics as a plain .txt
```

## Parsing

Choosing an `.svp` opens **parse settings**. It shows a live preview of the lines. The settings are saved in the `.lyrb`, and you can reopen them from the editor with `R`.

| setting | default | |
|---|---|---|
| Track | auto | the track with the most notes |
| Backing track | off | another track whose lyrics are merged into the lines in (brackets) |
| Line mode | every N bars | or *at rests / breaths* |
| Bars per line | 4 | the bar grid restarts at each stanza |
| Pickup | 0.5 beats | words this close before a bar line start the next line |
| Rest gap | 0.2 s | rests mode: a rest this long ends a line |
| Stanza per group | on | each SynthV note group (Verse, Chorus, …) starts a stanza |
| Stanza gap | 6 s | a rest this long starts a stanza (0 = off) |
| Max chars | off | longer lines are split into balanced halves |
| Audio | `<song>.wav` | any WAV in the folder, or none |

Notes with lyric `-` (melisma) or `+` (next syllable of a word) extend the previous word. `br`, `sil`, `sp`, `pau`, `cl`, `'`, and `.phoneme` notes are dropped. Adjacent CJK syllables are joined without spaces. A stanza clears the screen during the instrumental gap before it.

The karaoke wipe follows the SVP notes. Each syllable note of a word (the word note and each `+`) fills an equal share of its letters over exactly that note's duration. `-` notes extend the syllable before them, and the wipe pauses during rests between a word's notes.

## Text export

`lyric-engine txt song.svp` writes `song.txt` next to it: one lyric line per line and a blank line between stanzas, with backing vocals in (brackets). It takes the same parse flags as `import` (`-mode rests`, `-bars 2`, `-backing 1`, ...). Give it a `.lyrb` instead to export the lines exactly as you arranged them in the editor.

- `-timestamps`: start each line with its time as `[mm:ss.xx]`.
- `-o file.txt`: a different output file, or `-o -` to print to the terminal. `-f` overwrites an existing file.

## Backing vocals

Set **Backing track** to the track with the backing or echo vocals (`import -backing N` on the command line). Its lyrics are split into phrases at rests and breaths. Each phrase joins the lyric line it starts in, or the nearer line when it starts between two lines, and is shown in brackets:

```
reachin' out when you want to let go … (still you keep your spirits high)
```

A backing word that only doubles the lead (the same word, sung at the same time) is dropped. Backing words get their own karaoke wipe in a softer, italic color. In the editor they are ordinary words: `e` edits the text, `x` hides or shows the word, `b` moves a word in or out of the brackets, and enter/backspace move it between lines.

## Lyric colors

The lead vocal and the (backing vocals) each have a lyric color: the color of the karaoke wipe. Finished lines use a dimmed version of the lead color. When a song is parsed, each color is picked from the SynthV voice of its track:

| voice | color |
|---|---|
| Kasane Teto | red |
| GUMI | green |
| Hatsune Miku | teal |
| Megurine Luka | pink |
| Kagamine Rin / Len | yellow |
| KAITO | blue |
| anything else | the default cyan |

In the editor, `c` / `C` cycles the color of the part the cursor word belongs to: the backing vocals for a word in brackets, the lead otherwise. Re-parsing (`R`) only fills in the voice color for a part you have not colored yourself. `lyric-engine info song.svp` lists the voice of each track.

## Analysis readout

Each `.lyrb` also stores an analysis of the song:

- **Tempo map and time signatures**, from the SynthV project.
- **Key**, detected from the sung notes. Each pitch class is weighted by how long it is sung and matched against the Krumhansl–Kessler major and minor key profiles. The percentage is how well the best key fits.
- **Vocal range**, from the lowest to the highest sung note.

Press `a` in the editor (or during `play`, for that playback only) to show it as a readout above the lyrics:

```
BPM 76.0 · SIG 4/4 · BAR 009 ○●○○ · KEY B MINOR 90% · NOTE F#4
                        RANGE B3–A5
──────────────────────────────────────────────────────────────
```

BPM and the bar follow tempo and time-signature changes. The dots tick along with the beats of the bar, and NOTE is the note being sung right now. Files made before this feature get their analysis from their source `.svp` when it is opened. `lyric-engine info song.lyrb` prints it.

## Editor keys

| key | action |
|---|---|
| ←→ ↑↓ / hjkl, home/end, pgup/pgdn | move cursor |
| enter | start a new line at the cursor |
| backspace | merge the line into the previous one; directly below an empty line, delete that empty line instead |
| s | toggle a stanza break at the cursor's line |
| J | glue the word to the previous one (no space) |
| x | hide/show the word |
| b | toggle the word as a backing vocal, shown in (brackets) |
| c / C | next / previous lyric color for the word's part (lead or backing) |
| ~ | rotate the word's case: lower → Capitalized → ALL CAPS (also works on a selected story message, and shift+tab does the same while editing text) |
| e | edit the word's text |
| d | cycle the layout (context lines above/below) |
| u | undo |
| space / g / f | play-pause / play from cursor / follow playback |
| , . / r | seek ±5 s / restart |
| [ ] { } | audio sync offset ±10 / ±100 ms |
| m | add a story message (after the selected message; at 0:00 stopped, after the selected line; otherwise at the playhead) |
| n | add an empty line (log view only; placed like `m`) |
| v | switch the display between the focus view and the log view |
| a | show/hide the analysis readout (BPM, key, bar/beat, note, range) |
| i | song info: title, artist, album, vocalist, lyrics/music by, year, comment, audio file |
| R | re-parse with different settings (undo with u) |
| o | open another file |
| ctrl+s / q | save / quit |

The preview on the left has the 7:9 shape of the final display window, and the line list uses the remaining width. The preview uses the same renderer as `play`, so what you see while editing is the final output.

## Story messages

Story messages are free text lines that pop up at a set moment, for example in an empty bar, during an instrumental, or between two notes. They show in the editor's list in time order, marked `✦`. With one selected:

| key | action |
|---|---|
| e / enter | edit the text |
| c / C | next / previous color (red, orange, yellow, green, cyan, blue, purple, pink, gray, white, or default) |
| b / I | bold / italic |
| < > | move it 0.1 s earlier / later |
| T | move it to the playhead |
| g | play from just before it |
| x | delete |

### Empty lines

`n` adds an empty line, shown in the list as `↵ (empty line — log view only)`. In the log view it adds a blank row at that moment, on top of the blank row a new stanza already gets. The focus view ignores empty lines completely, so they never replace a message card or the lyrics. They move, delete, and undo like messages. `e` adds text and turns one into a normal message.

## Display views

- **focus** (default): the current line is centered, with context lines above and below (`d` changes how many). A story message pops up alone as a card until the next line starts, for at most 8 s.
- **log**: lines pile up from the top and scroll up when the screen is full. The line being sung gets the karaoke wipe, finished lines dim, stanzas are separated by a blank line, and story messages stay in the scrollback in their colors.

Both views show the title, artist and credits (vocalist, music by, lyrics by; one line when the same person wrote both) before the first line. In the focus view they appear as a card until 3 s before singing starts; in the log view they stay at the top until the log scrolls past them.

## Song info

Press `i` in the editor to edit the title, metadata and audio file. On the Audio field, `enter` opens a file browser that lists `.wav` files and `x` removes the audio. A new WAV starts playing in the editor right away. `esc` goes back and keeps your changes, and `ctrl+s` in the editor saves them to the `.lyrb`.

## Playback window

`lyric-engine song.lyrb` (or `play song.lyrb`) resizes its terminal window to **7:9** at the full height of the screen, minus the taskbar, docked to the right. On a 16:9 monitor that is the strip left beside a 9:9 square. The window goes back to its previous size and position when playback ends.

- `-aspect W:H`: a different ratio, or `-aspect off` to leave the window alone. The lyrics are always drawn in a strip of this shape, so a wider window keeps the square free.
- `-dock left|right|center`: where the window (and the strip) goes.
- `-no-status`: hide the time line. `-no-audio` / `-audio file.wav` work as before.

This works in Windows Terminal and the classic console. If the window can't be resized, a short warning appears on the bottom line and playback continues.

### Recording with OBS

When OBS Studio (28 or later) is running, playback records itself: recording starts just before the song starts and stops when it ends or you quit. Turn on **Tools → WebSocket Server Settings → Enable WebSocket server** in OBS. If authentication is on, set the password once with `setx OBS_WEBSOCKET_PASSWORD yourpassword` (open a new terminal afterwards), or pass `-obs-password`. If OBS can't be reached, a short warning appears and playback continues without recording.

- `-obs host:port`: a different address (default `localhost:4455`), or `-obs off` to never record.

## `.lyrb` format (v5, little-endian)

The file has a 100-byte header, which includes the parse settings and the key analysis, then a precomputed line table (16 B per line, sorted by time), then token records (16 B each), then syllable segments for multi-note words (12 B each), then metadata fields (12 B each), then story messages (16 B each), then the tempo map (12 B per change) and time signatures (8 B each), then a UTF-8 string blob, and ends with a CRC32. A reader parses it with one file read and fixed-offset reads, and finds the current line by binary search. The exact layout is documented in `internal/lyr/codec.go`.

## Audio

WAV only: PCM 8/16/24/32-bit or float 32/64-bit, mono or stereo. If the file or the audio device is unavailable, the program runs on a silent clock.

## License

[MIT](LICENSE)
