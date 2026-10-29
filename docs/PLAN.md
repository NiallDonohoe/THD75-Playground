# D75 Scope — plan

Living document. The brief is in [SPEC.md](SPEC.md); unknowns about the radio
are tracked in [../HARDWARE_QUESTIONS.md](../HARDWARE_QUESTIONS.md).

## Status

| Milestone | State |
| --- | --- |
| M0 Probe CLI | Built and green against the mock radio and synthetic source. **Waiting for the owner's hardware run** ([HARDWARE_CHECKLIST_M0.md](HARDWARE_CHECKLIST_M0.md)). |
| M1 Engine and demo page | Not started (gated on M0). |
| M2 Live radio | Not started. |
| M3 Demodulation and audio | Not started. |
| M4 Calibration and recording | Not started. |
| M5 Polish and sharing | Not started. |

## Second program: d75aprs

An APRS client (map, messaging, beacons, aprs.fi lookups) agreed on
2026-10-29. It is a separate program with its own spec and milestones A0–A4
([APRS.md](APRS.md), decisions 0004–0006), and it does not wait for M1–M5.
d75scope stays receive-only; the two programs never use the radio at the
same time, because d75aprs needs the TNC in KISS mode on the USB port.

| Milestone | State |
| --- | --- |
| A0 Packet core (`aprs`) | Done: tests and fuzzing green. No hardware check needed. |
| A1 Receive | Not started. |
| A2 Page and map | Not started. |
| A3 Messaging and beacons over IP | Not started. |
| A4 RF transmit | Not started. |

## Module layout

```
d75scope/                 Go module (go 1.27)
├── cmd/d75probe          M0 hardware probe (thin main over package probe)
├── cmd/d75scope          the program (M1+)
├── dsp                   pure Go, deterministic: oscillator, FIR design, FFT,
│                         windows, peak interpolation, spectrum averaging,
│                         synthetic IF generator, WAV reader/writer
│                         (M1+: decimator, demodulators, AGC, resampler)
├── ifsource              IF source/provider interfaces + synthetic source
│                         (M1: WAV-file source). No cgo.
├── audio                 the only cgo package (darwin): Core Audio device
│                         enumeration and capture. A !cgo stub keeps every
│                         other package buildable with CGO_ENABLED=0.
├── radio                 CAT codec, allow-list, CAT session goroutine,
│                         termios serial, USB discovery, snapshot/restore,
│                         fake TH-D75 (in-memory pipe and pseudo-terminal)
├── probe                 the probe's logic: steps, analysis, report, summary
├── server                M1+: HTTP, embedded assets, WebSocket, rigctld (M5)
├── web                   M1+: TypeScript front end (esbuild, strict)
├── schema                JSON schema of probe-report.json
└── testdata              synthetic WAV fixtures
```

### Interfaces

- `ifsource.Source` — an open, running IF stream: `Format()`, blocking
  `Read([]float32)` of interleaved frames, `Close()`. Pull-based rather than a
  block callback: the DSP goroutine pulls fixed-size blocks, which is what the
  concurrency model asks for, and it keeps the C callback out of Go entirely
  (see [decisions/0002](decisions/0002-pull-based-if-source.md)).
- `ifsource.Provider` — `Devices()` and `Open(id)`. Implemented by
  `audio.CoreAudio` (cgo) and by the synthetic provider used in tests and
  `--mock`.
- `radio.Radio` — transport-agnostic typed radio API (identify, frequency,
  mode, IF output, fine tune, fine step, auto-info, filters, squelch, VFO
  record, events). `radio.CAT` implements it over any `io.ReadWriteCloser`:
  a real serial port, a pseudo-terminal, or an in-memory pipe to the fake
  TH-D75. The "mock radio in process" is the fake D75 behind a pipe, so the
  codec, allow-list and session code are exercised by every test.
- `radio.Guard` — wraps a `Radio`, takes the snapshot before the first write,
  records every value it writes, and restores on demand.

### Concurrency

- Audio: Core Audio's AudioQueue input callback (a C thread) copies into a
  C-side single-producer/single-consumer ring with C11 atomics. No Go code
  runs on that thread. A Go goroutine drains the ring.
- CAT: one goroutine owns the port's writes and the in-flight request; one
  reader goroutine splits lines on `\r`, classifies them (reply, unsolicited
  AI line, NMEA, KISS) and routes by prefix.

## Build order

M0 (this milestone) builds only what the probe needs, so later milestones are
not pre-empted before the hardware answers come back:

1. `radio`: codec + allow-list (with one test per blocked command, through the
   typed path and the console path), session, fake D75, pty, termios serial,
   discovery, snapshot/restore.
2. `dsp`: WAV, FFT, windows, peak interpolation, spectrum averaging, synthetic
   IF generator.
3. `ifsource`: interfaces and synthetic provider coupled to the fake radio's
   state, so the probe's analysis can be tested against injected inversion
   and offset.
4. `audio`: Core Audio enumeration and capture (cgo, darwin only).
5. `probe` + `cmd/d75probe`: read-only steps, `--allow-writes` steps, analysis,
   report, summary, `--mock` mode.
6. Docs: HARDWARE_QUESTIONS.md, docs/CAT.md (unverified until the probe runs),
   HARDWARE_CHECKLIST_M0.md, schema, CHANGELOG.

Then M1 → M5 as in the spec.

## Dependencies

M0 uses the standard library only, so nothing needed approval:

- Serial: a small termios wrapper over the standard `syscall` package
  (darwin and linux both expose `Termios`, the get/set ioctls and `TIOCEXCL`).
- Audio: Core Audio directly through cgo, no binding library.

Expected requests later (will ask first, with a decision record):

- `github.com/coder/websocket` for M1's WebSocket server.
- esbuild (npm dev dependency) and Playwright for the front end.

## Discrepancies found while reading the references

Flagged, not guessed. Each has an entry in HARDWARE_QUESTIONS.md.

1. **IO 0**: the spec says "AF"; the LA3QMA D74 page says "Off". The probe
   records both the raw value and whether audio flows when IO=0.
2. **FT and FS have no band parameter** in the D74 reference (`FT p1`,
   `FS p1`), so the spec's "every write targets band B" cannot be expressed
   in the command itself. Unknown whether they act on band B, on the control
   band, or globally. The allow-list accepts them without a band; the probe
   records FT/FS before and after so the owner can check which band changed.
3. **FV** reply format: the D74 page gives `FV 1.08`; other Kenwood radios
   reply with several comma-separated fields. The codec stores the raw text.
4. **SH** for AM has only indices 0–3 (index 4 is `N`). The codec accepts
   0–4 and leaves meaning to a table.
5. The **mode table** includes DV, DR and R-CW. The allow-list refuses
   writes of DV (1) and DR (7) — D-STAR is a non-goal — and keeps the rest.
6. **AE** (serial number and model) exists in the D74 list but is not in the
   spec's table, so it is not used; the USB serial number comes from the USB
   descriptor instead. Adding AE would need the owner's approval.
7. CHIRP #4129 used `/dev/tty.usbmodem*` at 9600 baud; the spec says
   `/dev/cu.usbmodem*`. We use `cu.` (it does not wait for carrier detect),
   and the probe tries 115200, falling back to 9600 if ID gets no answer.

## Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| IF format or centre differs from 48 kHz / 12 kHz | DSP constants wrong | Every constant derives from the measured rate and a configurable IF centre; probe measures both. |
| Stereo IF channels differ (I/Q) | Whole front end changes | Probe compares channels and flags it; we stop and ask. |
| FQ with non-step frequencies rejected or rounded | Coarse/fine tuning design | Probe reads back after every FQ; reports any rounding. |
| FT/FS act on the wrong band | Could alter band A | Probe records before/after; the owner checks band A on the display. |
| AI unsolicited line format unknown | Missed knob turns | Probe logs every unsolicited line verbatim. |
| Mode changes move the dial (FQ) | Restore order | Restore re-reads every field after all writes and retries mismatches once. |
| Microphone permission silently refused | All-zero capture | Probe detects all-zero audio and says so. |
| Port contaminated by NMEA/APRS/KISS | Unreliable CAT | Session classifies lines, holds writes until clean for 3 s. |
| Radio in KISS mode | No CAT | No reply to ID or 0xC0 seen → stop, tell the owner. Never send KISS exit. |
| Crash or unplug mid-probe | Radio left modified | Snapshot is written dirty before the first write; next run refuses writes until the previous session is restored (`--restore-previous`). |
