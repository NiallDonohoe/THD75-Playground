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

## Changes to the spec

None yet: the probe has not run on the radio.
