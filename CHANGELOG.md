# Changelog

## M0 — probe CLI (2026-10-23, waiting for the hardware run)

- `d75probe`: read-only survey of the radio (audio devices and formats, USB
  identity, serial port, ID FV FO FQ MD IO FT FS AI SH BY) and an
  as-found recording; `--allow-writes` for the full IF survey with snapshot
  and verified restore. Writes `probe-report.json`, WAV files and
  `summary.txt`. `--mock` runs it against a fake TH-D75 on a
  pseudo-terminal and a synthetic IF. `--list` lists devices only.
- `radio`: CAT codec, allow-list with hard blocks, session goroutine with
  prefix routing, 500 ms timeout and one retry, NMEA/APRS/KISS detection
  that holds writes, termios serial with TIOCEXCL, USB discovery from the
  I/O Registry, atomic snapshot and restore with the owner-changed rule,
  and the fake radio.
- `dsp`: WAV I/O, FFT, Blackman–Harris window, spectrum averaging, peak
  interpolation, level statistics, Kaiser FIR design, synthetic IF.
- `audio`: Core Audio listing and capture by device UID (cgo).
- Docs: PLAN, SPEC, HARDWARE_QUESTIONS, CAT, M0 checklist, decisions
  0001–0003, probe report schema.
