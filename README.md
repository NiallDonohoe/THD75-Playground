# D75 Scope

A panadapter for the Kenwood TH-D75A: the radio streams band B's 15 kHz IF
over USB audio, and D75 Scope turns it into a waterfall in Chrome, with
software SSB, CW and AM demodulation and tuning over the USB serial (CAT)
port. Receive only: nothing in it can make the radio transmit.

**Status: milestone M0.** The hardware probe (`d75probe`) is built and
tested against a fake radio. Next it needs a run on the real radio
([docs/HARDWARE_CHECKLIST_M0.md](docs/HARDWARE_CHECKLIST_M0.md)). The
waterfall program (`d75scope`) starts in M1. See [docs/PLAN.md](docs/PLAN.md).

A second, separate program, `d75aprs`, is in progress: an APRS client using
the radio's KISS TNC, with a map, messaging and beacons. Unlike d75scope it
can transmit, but only when that is switched on. See [docs/APRS.md](docs/APRS.md).
So far its page runs on made-up traffic:

```sh
make build
./bin/d75aprs --demo --call YOURCALL-7 --lat 53.35 --lon -6.26
# then open http://127.0.0.1:8076/ in Chrome
```

What to check is in [docs/CHECKLIST_A1.md](docs/CHECKLIST_A1.md).

A third program, `d75ft8`, decodes FT8 itself, from band B's audio in USB
mode, without WSJT-X. Receive only. See [docs/FT8.md](docs/FT8.md). So far
the decoder works on recordings: 15-second WAV files such as the ones
WSJT-X saves (Save → All).

```sh
make build
./bin/d75ft8 decode ~/Library/Application\ Support/WSJT-X/save/*.wav
./bin/d75ft8 encode --freq 1500 "CQ YOURCALL IO63"   # writes ft8.wav
```

## Requirements

- An Apple Silicon Mac (Linux compiles but is untested), Go 1.27, and the
  Xcode command-line tools (for the Core Audio part).
- A TH-D75A on a USB-C data cable.

## Radio setup

| Menu | Setting | Why |
| --- | --- | --- |
| 980 | COM+AF/IF Output | Exposes the serial port and USB audio (not Mass Storage) |
| 981, 982, 983 | Bluetooth | Keeps GPS, APRS and KISS traffic off the USB serial port |
| KISS mode | Off | CAT does not answer in KISS mode |
| 91A | Start at 4; adjust until the IF peaks below −6 dBFS | USB audio level |
| 102 | Leave it: the programs set it (IO) and put it back | USB Out Select: AF / IF / Detect |

Put an antenna on band B suited to the band you watch; for HF, a wire on the
SMA connector.

macOS puts every audio input behind the microphone permission, and a
command-line program inherits it from the terminal that starts it. Run from
Terminal and allow microphone access when asked (System Settings → Privacy &
Security → Microphone). A refused permission shows up as silence, and the
probe says so.

## The probe

```sh
make build
./bin/d75probe --list                 # audio devices and serial ports, no radio commands
./bin/d75probe -v                     # read-only survey
./bin/d75probe -v --allow-writes --ref-carrier 909000   # full IF survey, then restore
./bin/d75probe --mock --allow-writes --seconds 2        # against a fake radio
```

It writes `probe-out-<time>/` with `probe-report.json` (schema:
[schema/probe-report.schema.json](schema/probe-report.schema.json)), the WAV
recordings and `summary.txt`. With `--allow-writes` it first saves a snapshot
of band B's settings to `~/Library/Application Support/d75scope/`, changes
only frequency, mode, IF output, fine tuning, fine step and AI, and puts
them back, verifying each one. Ctrl-C once restores and exits; twice exits
at once and leaves the snapshot for `--restore-previous`.

The questions it answers are in [HARDWARE_QUESTIONS.md](HARDWARE_QUESTIONS.md).

## Development

```sh
make test            # go test with and without cgo; no radio needed
make test-hardware   # tests that need the radio
make mock            # the probe against the fake radio
```

Layout and design are in [docs/PLAN.md](docs/PLAN.md); CAT commands in
[docs/CAT.md](docs/CAT.md); design decisions in [docs/decisions/](docs/decisions/).

## Routing audio to WSJT-X

Arrives with demodulation (M3) and the rigctld server (M5): D75 Scope plays
demodulated audio to a virtual device such as BlackHole, WSJT-X takes its
input from that device, and WSJT-X tunes the radio through D75 Scope's
rigctld-compatible port on localhost.
