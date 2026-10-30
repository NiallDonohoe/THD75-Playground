# Hardware questions

Every fact about the TH-D75 that the code must not guess. Each one is
answered by the probe (`cmd/d75probe`, milestone M0) or by the owner, and the
answer is written here. **When an answer contradicts docs/SPEC.md, the answer
wins**: the change is recorded in the "Changes to the spec" section at the
end, and the code reads the value from configuration or the probe report,
never from a constant.

The IDs match `answers[].id` in `probe-report.json`.

Status key: **open** (not yet run on the radio) · **answered** · **partly**.

| ID | Question | Why it matters | How it is answered | Hypothesis (from the docs) | Answer | Status |
| --- | --- | --- | --- | --- | --- | --- |
| Q01 | USB audio format: sample rate, bit depth, channel count | Every DSP constant derives from the rate | Core Audio device properties of the radio's input | 48 kHz | | open |
| Q02 | If stereo, are the channels identical, or I and Q? | An I/Q stream would replace the whole front end | Channel comparison of an IF recording | Mono, or identical channels | | open |
| Q03 | USB identity: vendor ID, product ID, product string, serial | Discovery matches on these; calibration is keyed by the serial | I/O Registry entry of the modem port | Vendor 0x2166, product string "TH-D75" (D74 was 0x600B "TH-D74") | | open |
| Q04 | Serial port path, a working baud rate, exclusive lock | Connection | Probe opens it, tries 115200 then 9600, tries a second open | `/dev/cu.usbmodem*`, any baud, TIOCEXCL honoured | | open |
| Q05 | Do ID FV FO FQ MD IO FT FS SH BY AI reply as the D74 reference says? | The codec is written to the D74 reference | Raw replies of every read, in `cat.reads` | Same as the D74 (firmware V1.10) | | open |
| Q06 | IO 0: AF (spec) or Off (D74 reference)? | What "IF off" sounds like; the banner text | Read-only run with Menu 102 = AF: is the USB audio silent? | AF | | open |
| Q07 | IF centre and bandwidth as delivered over USB | Mixer frequency, display span | Noise passband of the USB IF recording (−6/−20 dB edges) | 12 kHz centre, 15 kHz wide | | open |
| Q08 | Spectral inversion, per mode | The sign *s* in the frequency formula | Dial +1 kHz: do signals move down (s=+1) or up (s=−1)? | Unknown; LSB may differ from USB | | open |
| Q09 | Where the dial lands in the IF, per mode (Δ_mode) | Absolute frequency labels | `--ref-carrier`: known carrier vs. the peak's IF position | Unknown; differs per mode | | open |
| Q10 | Does the CW pitch (Menu 170) shift the CW IF? | Whether Δ_CW depends on a menu | Two runs with different Menu 170, compare Δ_CW | Possibly | | open |
| Q11 | Does IF output work in FM and NFM? | Whether NFM demod is possible later | Recordings in FM and NFM vs. USB level and passband | SSB, CW, AM only (10.8 kHz third IF) | | open |
| Q12 | Is the IF before or after the radio's filters (Menus 120–122) and AGC? | Whether software filters are the only selectivity; meter meaning | IF width vs. SH filter width; AGC by owner experiment | Before the filters; AGC unknown | | open |
| Q13 | Does the speaker keep playing with IF output on? | Owner experience | Owner, at the end of an interactive run | Unknown | | open |
| Q14 | FT/FS have no band parameter: do they act on band B? | "Every write targets band B" cannot be expressed in the command | Owner watches band A during the run; FT/FS read-backs | Act on band B (the only band with fine tuning) | | open |
| Q15 | FQ resolution: does the radio round, and to what? | Coarse/fine tuning split | Ask for an off-grid frequency (dial + 1010 Hz) with fine tuning on; read back | Rounds to the 20 Hz fine step | | open |
| Q16 | What do unsolicited AI lines look like? | Tracking knob turns (M2) | Interactive AI test: owner turns the dial, lines are logged | `FQ 1,<10 digits>` and similar | | open |
| Q17 | What does the Detect output (IO 2) contain? | Not needed for v1; recorded for later | Recording `detect-usb.wav` | Band B's demodulated audio | | open |
| Q18 | Does changing mode (MD) move the dial (FQ)? | Restore order, recentring | FQ read back after every MD | No | | open |
| Q19 | Is Menu 91A set so the IF peaks below −6 dBFS? | Clipping | Peak level over all IF recordings | Mid setting is about right | | open |
| Q20 | Is NMEA, APRS or KISS traffic on the USB serial port? | Writes are held while it is | Port classification during the run | Not if Menus 981–983 are on Bluetooth | | open |
| Q21 | Does capture deliver real samples (microphone permission)? | Silent capture means a refused permission | All-zero check of every recording | Yes, once Terminal has microphone access | | open |

## Questions the probe cannot answer

| ID | Question | Plan |
| --- | --- | --- |
| X01 | Is there a CAT command for Menu 506 (Data Band)? | Not needed so far. If it is, capture ARFC-D75's serial traffic on Windows with a port monitor. Never guess one. |
| X02 | Does AE (serial number and model, in the D74 list) work on the D75, and should it be used? | Not in the spec's command table, so not sent. The USB serial number is used instead. Needs the owner's approval to add. |
| X03 | What does APRS output on the USB port look like (Menu 982)? | The session detects lines shaped like `CALL>PATH:…`. Unverified; confirm only if the owner ever routes APRS to USB. |
| X04 | Is the IF before or after the radio's AGC? | Owner experiment in M2: watch the IF level while a strong signal fades in and out, with the radio's AGC fast vs. slow. |

## d75aprs: the KISS TNC

Questions for the APRS client ([docs/APRS.md](docs/APRS.md)). They are
answered by owner checklists, not the probe: A2 (receive) and A4 (transmit).

| ID | Question | Why it matters | How it is answered | Hypothesis | Answer | Status |
| --- | --- | --- | --- | --- | --- | --- |
| K01 | Does firmware 1.03 offer the KISS TNC on USB (Menu 983 = USB), and how is KISS mode entered and left by hand? | The whole receive path; the setup steps | Owner, A2 checklist | Yes, from the TNC menu or key, as on the D74 | | open |
| K02 | In KISS mode, is the radio's own APRS off (display, auto-ack, beacons)? | Same SSID as the radio is safe only if the radio stops acking | Owner sends the radio a message from another station in KISS mode; watch for a second ack | Off | | open |
| K03 | Does the KISS port echo frames the host sends? | Our own packets must not show up as heard on RF | A4 checklist: transmit one frame and log what comes back | No echo | | open |
| K04 | Which band does the TNC use in KISS mode, and is 1200 or 9600 bps set on the radio? | Setup steps; the program cannot change either | Owner, A2 checklist | The data band (Menu 506), 1200 bps set in the TNC menu | | open |
| K05 | With Menu 981 (GPS) also on USB, do NMEA sentences interleave with KISS frames? | A GPS position for beacons later | Later, when GPS is wanted | Yes, as text between KISS frames | | open |
| K06 | Does the TNC send only data frames on KISS port 0? | The reader's filtering | A2: log every KISS command byte seen | Only `0x00` | | open |
| K07 | Does CAT stay silent in KISS mode, and does leaving KISS mode by hand restore it cleanly? | Switching between d75aprs and d75scope | Owner, A2 checklist: run `d75probe --list` and a read-only probe after leaving KISS | Silent in KISS; clean afterwards | | open |
| K08 | Does a KISS data frame transmit straight away on the data band, at the radio's power setting, after waiting for a clear channel? | Transmit behaviour and limits | Owner, A4 checklist, with a second receiver | Yes, with the TNC's own channel check | | open |

## d75ft8: the AF output

Questions for the FT8 decoder ([docs/FT8.md](docs/FT8.md)), answered by the
owner's F2 checklist. Q01 (audio format) and Q06 (whether `IO 0` is AF)
above apply as well.

| ID | Question | Why it matters | How it is answered | Hypothesis | Answer | Status |
| --- | --- | --- | --- | --- | --- | --- |
| F01 | With Menu 102 = AF (`IO 0`), what does the USB audio carry: band B's demodulated audio, band A's, or both mixed as on the speaker? | Band A could add audio, or carry the wrong band | F2: band B on a quiet HF frequency, band A on a busy FM channel; record the USB audio | Both bands, as the speaker hears them; band A has to be quiet | | open |
| F02 | Is the USB AF level fixed by Menu 91A, or does it follow the volume knob? | The level meter's advice; whether turning the speaker down kills the decodes | F2: change the volume while watching the level meter | Fixed by Menu 91A | | open |
| F03 | What is the AF passband in USB mode, and does Menu 120 (SSB filter) change it? | FT8 uses 200–3000 Hz of audio; a 2.4 kHz filter would cut the top | F2: noise spectrum of an AF recording at each SSB filter setting | About 100–2900 Hz at the widest setting | | open |
| F04 | Does the radio's SSB AGC pump on a strong FT8 signal, and can it be set to slow or off? | Strong signals could pull weak ones in the same slot under the noise | F2: owner watches the waterfall's noise floor under a strong signal, AGC fast vs slow | AGC on, adjustable in the menu | | open |
| F05 | On an HF dial frequency, is audio 0 Hz exactly at the dial (the USB carrier), or offset? | RF frequency labels and spot reports | F2: a decode's audio frequency vs. WSJT-X on another receiver, or a known carrier | Within ±10 Hz on HF | | open |

## Changes to the spec

None yet: the probe has not run on the radio.
