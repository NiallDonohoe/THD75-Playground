# d75ft8 — FT8 decoder

Living spec for the third program in this module, agreed with the owner on
29 Oct 2026. The decisions behind it are
[0007](decisions/0007-d75ft8-standalone-af-receive-only.md) and
[0008](decisions/0008-own-ft8-decoder.md). The radio facts it depends on are
the F questions in [../HARDWARE_QUESTIONS.md](../HARDWARE_QUESTIONS.md).

## What it is

`d75ft8` decodes FT8 from the TH-D75 on the Mac, with no WSJT-X needed:

- band B in USB on an FT8 dial frequency, its demodulated audio over USB;
- every 15 s slot decoded, as WSJT-X would print it (UTC, SNR, DT, audio
  frequency, message);
- a page on localhost with an audio waterfall, the decodes marked on it, a
  scrolling decode list and a band picker that tunes band B.

The original brief (SPEC.md) lists "decoding digital modes inside the
program" as a v1 non-goal for d75scope, so this is a separate program, as
d75aprs is. d75scope's rules are unchanged. The three programs never use
the radio at the same time.

## Rules

- **Receive only.** USB audio only flows out of the radio, so there is no
  path to transmit FT8 at all, and the program sends nothing that could key
  the radio.
- **CAT goes through `radio`'s allow-list and `Guard`**, unchanged: the
  writes are `IO 0` (AF output), `MD 1,4` (USB) and `FQ 1,…` (the dial),
  all on band B, after a snapshot, and restored on exit or on the next
  start after a crash, as d75scope does.
- **Localhost only**, with the same Host, Origin and Sec-Fetch-Site checks
  as d75aprs, and no outbound connections. PSK Reporter uploads are later
  and would be off by default.
- `go test ./...` passes with no radio, no audio device and no network.

## Radio setup

| Menu | Setting | Why |
| --- | --- | --- |
| 980 | COM+AF/IF Output | Serial port and USB audio |
| 981–983 | Bluetooth (or off) | Keep NMEA, APRS and KISS off the CAT port |
| 102 | Set by the program (`IO 0`, AF) | Band B's demodulated audio over USB (F01) |
| 91A | Middle of 1–7 to start | The page's level meter shows when it is right |
| 120 (SSB filter) | Widest | FT8 occupies 200–3000 Hz of audio (F03) |

An antenna for the band: for HF, a wire on the SMA connector. Band A
should be quiet (single-band display, or its squelch closed) until F01 is
answered.

## Slot timing

FT8 slots start at 0, 15, 30 and 45 s past each UTC minute. The program
takes slot boundaries from the Mac's clock (macOS keeps it on NTP) and
decodes each slot as soon as it ends. The decoder accepts DT from −1.3 to
+2 s. The page shows the median DT of the last few minutes as a clock
check: well away from zero means the Mac's clock (or the audio path's
latency) is off.

## Bands

The band picker tunes band B to the usual FT8 dial frequencies, in USB:

| Band | Dial (MHz) | | Band | Dial (MHz) |
| --- | --- | --- | --- | --- |
| 160 m | 1.840 | | 15 m | 21.074 |
| 80 m | 3.573 | | 12 m | 24.915 |
| 60 m | 5.357 | | 10 m | 28.074 |
| 40 m | 7.074 | | 6 m | 50.313 |
| 30 m | 10.136 | | 2 m | 144.174 |
| 20 m | 14.074 | | 70 cm | 432.174 |
| 17 m | 18.100 | | | |

Any other frequency in band B's range can be typed in.

## Decoder

Package `ft8`, pure Go, no I/O ([0008](decisions/0008-own-ft8-decoder.md)):

- **Messages**: standard calls (with /R and /P), CQ with a modifier, grids,
  reports, RRR, RR73 and 73, hashed calls (<...>, filled in once the call
  has been heard in full), non-standard calls, free text, telemetry and
  DXpedition mode. Contest types (Field Day, RTTY Roundup, EU VHF) are
  shown as their type. Pack covers what a station sends in a normal QSO,
  for `encode`, the demo and the tests.
- **Coding**: CRC-14, the (174,91) LDPC code, Gray mapping, Costas arrays,
  GFSK (BT 2.0) synthesis.
- **Decoding**, per slot:
  1. coarse search for Costas patterns in a spectrogram (half-symbol hops,
     3.125 Hz bins);
  2. for each candidate, a baseband copy cut from one FFT of the slot, then
     fine sync to 0.25 Hz and 1/30 of a symbol;
  3. soft bit metrics over one, two and three symbols, and sum-product
     belief propagation;
  4. CRC check and unpacking;
  5. each decode subtracted from the audio (start refined to the sample),
     and the search repeated, three passes in all.
- **Input**: 12 kHz mono; `To12k` filters and decimates from any multiple
  of 12 kHz (the radio's 48 kHz, Q01).

What it does not do yet: ordered-statistics decoding (OSD) and a-priori
decoding, which is how WSJT-X reaches its last few dB. Both raise the
false-decode rate and need measuring before they go in.

## Packages

| Package | Role |
| --- | --- |
| `ft8` | Message packing, coding, synthesis and the decoder. Pure Go, no I/O. |
| `dsp` | Shared: FFT, the any-size DFT (Bluestein), FIR design, WAV. |
| `ft8app` | (F1) Slot clock, decode history, waterfall rows, the event stream. |
| `ft8web` | (F1) HTTP server: localhost guard, embedded page, server-sent events. Wire format in `FT8_PROTOCOL.md`. |
| `web/ft8` | (F1) The page, in plain JavaScript like `web/aprs`. |
| `cmd/d75ft8` | The program: `decode` and `encode` now; the page from F1. |

Audio capture is `audio` (Core Audio, the only cgo), and CAT, discovery,
the allow-list and snapshot/restore are `radio`, all reused unchanged.

## Testing without the radio

- **Synthetic slots**: messages encoded by `ft8`, GFSK-modulated, placed at
  chosen frequencies, times and SNRs in white noise, including two signals
  overlapping in frequency. The decoder must find all of them, to 1 Hz,
  40 ms and 3 dB, and decode nothing from noise alone.
- **Table cross-check**: every codeword the LDPC generator makes must pass
  every parity check, which catches a mistake in either table.
- **Fuzzing**: `unpack` on arbitrary payloads (they come off the air), and
  `Pack` on arbitrary text, which must unpack to the same words.
- **Reference recordings**: real 15 s slots with WSJT-X's decodes alongside.
  `make test-ft8-reference` decodes every `.wav` in `FT8_REFERENCE_DIR`
  (default `testdata/ft8`) and compares. The test is skipped when there are
  none, and fails below 70% of WSJT-X's decodes. With the seven ft8_lib test
  recordings it finds 71 of WSJT-X's 80 decodes (89%); the misses are all
  but one below −13 dB.
- **`d75ft8 encode`** writes a WAV that WSJT-X can decode, as an
  independent check on the encoder.
- **`--demo`** (F1): made-up band activity, with every callsign Q plus a
  digit (Q1ABC), which no country issues and which survives FT8's callsign
  packing.

## Milestones

Each milestone ends with green tests, a changelog entry and an owner check.
They do not wait for d75scope's or d75aprs's milestones.

1. **F0 — Decoder core.** `ft8`, the any-size DFT in `dsp`, and `d75ft8
   decode` and `encode`. Built and green. Done when the owner has decoded a
   few WSJT-X recordings with `d75ft8 decode` and decoded a `d75ft8 encode`
   WAV in WSJT-X (both optional, no radio needed).
2. **F1 — Page, in demo mode.** Slot clock, waterfall, decode list, band
   picker (inert in demo), clock check, `--demo` with made-up slots decoded
   by the real decoder. Done when the owner has worked through checklist F1
   in Chrome.
3. **F2 — Live receive.** The radio's USB audio through `To12k`, slots on
   the Mac's clock, CAT tuning with snapshot and restore, the level meter, a
   decode log on disk in ALL.TXT form, and recordings of slots on request.
   Done when the owner decodes FT8 off the air on at least one HF band and
   2 m (checklist F2, answering F01–F05), and the recordings become
   reference fixtures.

Later: PSK Reporter uploads (off by default), OSD and a-priori decoding,
FT4, and showing the decodes on d75scope's waterfall.

## Non-goals

- Transmitting, which the radio cannot do over USB audio anyway.
- Logging QSOs, or calling and answering stations.
- Running alongside d75scope or d75aprs.
