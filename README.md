# D75 Scope

A panadapter for the Kenwood TH-D75A: the radio streams band B's 15 kHz IF
over USB audio, and D75 Scope turns it into a waterfall in Chrome, with
software SSB, CW and AM demodulation and tuning over the USB serial (CAT)
port. Receive only: nothing in it can make the radio transmit.

This repository holds four programs that share one radio and some packages:

| Program | What it does | Status | Spec |
| --- | --- | --- | --- |
| `d75probe` | Hardware survey of the radio's USB serial and IF audio | M0 built, awaiting a real-radio run | [PLAN](docs/PLAN.md) |
| `d75scope` | Panadapter and demodulator | Not started (M1 is gated on the probe) | [SPEC](docs/SPEC.md) |
| `d75aprs` | APRS client: map, messages, beacons | A1 demo page built, awaiting check | [APRS](docs/APRS.md) |
| `d75ft8` | FT8 decoder and encoder | F0 (WAV files) merged; live page is F1 | [FT8](docs/FT8.md) |
| `d75sat` | Satellite passes, map, Doppler | S1 page built, awaiting check | [SAT](docs/SAT.md) |

**Status of d75scope: milestone M0.** The hardware probe (`d75probe`) is built and
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

The fourth, `d75sat`, is for listening to amateur satellites: passes, a map
with ground tracks and coverage, and (from milestone S2) Doppler tuning of
band B. Listen only. See [docs/SAT.md](docs/SAT.md). The page runs now,
on real data from CelesTrak and SatNOGS or on made-up satellites:

```sh
./bin/d75sat --demo          # made-up satellites, clock ×10
./bin/d75sat                 # real data; set your location on the page
# then open http://127.0.0.1:8078/ in Chrome
./bin/d75sat passes --lat 53.35 --lon -6.26 --freq 145.8 amateur.tle
```

What to check is in [docs/CHECKLIST_S1.md](docs/CHECKLIST_S1.md).

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
make build               # all four binaries into bin/
make test                # vet, go test with and without cgo, and the Node tests
make test-web            # Node tests of the pages' map and sky maths (skipped without Node)
make test-ft8-reference  # decode recordings that have WSJT-X's decodes alongside
make test-hardware       # tests that need the radio
make mock                # the probe against the fake radio
make aprs-demo           # d75aprs on made-up traffic
make sat-demo            # d75sat on made-up satellites
```

`d75sat` flags: `--demo`, `--speed`, `--offline` (cached data only),
`--lat`/`--lon`, `--addr`, `--no-tiles`, `--tile-url`, `-v`. The pages listen on
loopback only and refuse requests from other sites.

### Layout

| Directory | Contents |
| --- | --- |
| `cmd/` | The four programs' `main` packages |
| `radio/`, `audio/`, `ifsource/`, `probe/` | CAT over USB serial, Core Audio capture, IF sources (real and synthetic), the probe and its report |
| `dsp/` | FFT, DFT, FIR, spectra, WAV |
| `aprs/`, `aprsapp/`, `aprsfi/`, `aprsweb/` | APRS parser and encoder, engine, aprs.fi lookups, server |
| `ft8/` | FT8 packing, LDPC, synthesis and decoder |
| `orbit/`, `satdata/`, `satapp/`, `satweb/`, `satdemo/` | SGP4 and passes, CelesTrak and SatNOGS, engine, server, demo catalogue |
| `demo/` | Made-up APRS traffic |
| `localweb/`, `tiles/` | Shared loopback listener and request guard; map tile proxy |
| `web/` | Embedded pages: `aprs/`, `sat/` and the shared `map/` |
| `docs/`, `schema/` | Specs, protocols, checklists, decisions; the probe report schema |

Per-change history is in [CHANGELOG.md](CHANGELOG.md).

Layout and design are in [docs/PLAN.md](docs/PLAN.md); CAT commands in
[docs/CAT.md](docs/CAT.md); design decisions in [docs/decisions/](docs/decisions/).

## Routing audio to WSJT-X

Arrives with demodulation (M3) and the rigctld server (M5): D75 Scope plays
demodulated audio to a virtual device such as BlackHole, WSJT-X takes its
input from that device, and WSJT-X tunes the radio through D75 Scope's
rigctld-compatible port on localhost.
