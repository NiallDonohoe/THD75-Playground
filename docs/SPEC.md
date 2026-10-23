# TH-D75 IF Panadapter — Coding Agent Prompt

23 Oct 2026 · @Niall

This is the owner's original brief, kept verbatim (apart from Markdown
formatting) so that every later session works from the same text. When the
probe results contradict it, the probe wins: see HARDWARE_QUESTIONS.md and
docs/PLAN.md for what changed.

## Mission

You are building D75 Scope (working title): a Go program that turns a Kenwood TH-D75A handheld into a narrow-band SDR panadapter, with its user interface in a web browser. The radio streams a 15 kHz-wide IF, centred on 12 kHz, over USB audio. One Go binary turns that into spectrum and waterfall data, demodulates SSB, CW and AM in software, tunes the radio over its USB serial (CAT) port, and serves a TypeScript front end embedded in the binary. It replaces Kenwood's Windows-only ARFC-D75 tuning utility.

Who it is for: one licensed amateur operator. The radio plugs into an Apple Silicon Mac, and the waterfall opens in Chrome on that Mac. Later the same binary should run headless on a Raspberry Pi. Typical uses are watching a 15 kHz slice of a band, click-to-tune listening, and routing demodulated audio to WSJT-X or wsprd through a virtual audio device.

What success looks like:

- Plug in the radio, run d75scope, open the page, and within 5 seconds see a waterfall labelled in absolute RF frequency.
- Click or tap a signal and hear it, demodulated in software, without touching the radio.
- Quit, crash or unplug at any time, and the radio is left as it was found (or restored on next start).
- Everything except the final hardware check can be built and tested with no radio attached.

Constraints that shape everything:

- Receive only. Nothing in the program may cause the radio to transmit.
- The server listens on localhost only. There is no network access in v1, because the page can retune the radio.
- Several radio behaviours are unverified (see Radio facts). Treat them as hypotheses, hide them behind interfaces, and confirm them with a probe tool before relying on them.

## Radio facts

The radio does all the RF work; the app receives band B's IF as USB audio and talks CAT over a serial port on the same USB-C cable. Everything below marked unverified must be confirmed on the real radio before code depends on it.

Known, from Kenwood documentation:

- USB-C is a composite device: serial port, USB audio output and mass storage. Menu 980 picks "COM+AF/IF Output" (what we need) or "Mass Storage". The serial port is a CDC ACM device, so on macOS it appears as /dev/cu.usbmodem* with no driver. (spec sheet, manual)
- Audio only flows out of the radio. Nothing can be sent to it over USB audio.
- Menu 102 "USB Out Select" chooses AF, IF or Detect. On the predecessor TH-D74, IF sends band B's received IF to the PC and Detect sends band B's detected signal. (TH-D74 manual)
- The IF is centred on 12 kHz with a 15 kHz bandwidth. Kenwood pitches it for a PC bandscope while listening to SSB, CW or AM. (brochure)
- Band B covers 0.1–524 MHz and is the only band with SSB, CW and AM. The IF path is band B only.
- Menu 91A sets the USB audio output level, 1–7.
- Menus 981–983 route GPS NMEA, APRS output and the KISS TNC to USB or Bluetooth. Anything routed to USB arrives on the same serial port as CAT.
- Bluetooth offers serial (SPP) and headset (HSP) profiles. HSP is narrowband voice, so USB is the only usable IF path.
- Kenwood's own Windows tuner, ARFC-D75, changes Menu 102 and Menu 506 (Data Band). If it disconnects uncleanly the radio is left in that state and must be reset by hand. (ARFC-D75 notes)
- The owner's radio is the TH-D75A (Americas, tri-band). Band B fine tuning steps down to 20 Hz in SSB, CW and AM.

Unverified — the probe tool (milestone M0) must answer these:

- USB audio format: sample rate, bit depth and channel count. Expect 48 kHz, but read it from the device.
- USB identity. The TH-D74 enumerated as vendor 0x2166 (JVC KENWOOD), product 0x600B, product string "TH-D74" (CHIRP issue #4129). Expect the same vendor and a "TH-D75" product string, and match on those rather than a hard-coded product ID.
- Whether the IF is spectrally inverted, and where the displayed frequency lands within the IF in each mode (USB, LSB, CW, AM). The CW pitch (Menu 170) may shift it.
- Whether IF output works in every band B mode or only SSB, CW and AM. Kenwood specifies a 10.8 kHz third IF for those three modes only, which hints that the USB IF comes from that chain. (spec sheet)
- Whether the IF is taken before or after the radio's AGC and its own receive filters (Menus 120–122).
- Whether the radio's speaker keeps playing while IF output is active.
- Whether the TH-D74 CAT commands in the next section behave the same on the TH-D75.
- What the Detect output contains. Not needed for v1, but record a sample of it.

## CAT protocol

CAT is plain ASCII: a two-letter command, a space, comma-separated parameters, and a carriage return. The radio replies by echoing the command with its values. Kenwood does not publish it; the community reference is LA3QMA's TH-D74 command list (D74 firmware V1.10). Use it as the starting point and verify every command on the TH-D75.

Transport:

- Open /dev/cu.usbmodem* at 8N1. Baud is nominal on a CDC port; both 9600 and 115200 have worked on the D74, and commands work in normal operating mode with no special PC mode. No flow control.
- Terminate every command with \r; read replies up to \r.
- Expect ? for an unknown or malformed command and N when a command is not available in the current state. Treat any reply that does not echo the command as an error, and log it.
- One command in flight at a time, owned by a single goroutine. Time out after 500 ms and retry once.
- With AI 1 the radio pushes unsolicited updates when the knob turns. The reader must route lines by their prefix, not assume request/response pairing.
- If Menus 981–983 route NMEA, APRS or KISS to USB, that traffic lands on this port. Detect it (lines starting $GP, bytes 0xC0) and tell the user to route those menus to Bluetooth.

Commands the app uses (band B is parameter 1):

| Command | Purpose | Syntax from the D74 reference | Notes |
| --- | --- | --- | --- |
| ID | Identify the radio | ID → ID &lt;model&gt; | First call; expect TH-D75 |
| FV | Firmware version | FV | Log it with every probe report |
| FQ | Band B frequency | Read FQ 1 → FQ 1,&lt;Hz, 10 digits&gt;; set FQ 1,&lt;Hz, 10 digits&gt; | Main tuning command; read back after every set |
| FO | Full VFO record | Read FO 1 → 21 fields: frequency, offset, steps, mode, fine mode, fine step, tones, D-STAR fields | Snapshot and restore |
| MD | Band B mode | Read MD 1 → MD 1,&lt;m&gt;; set MD 1,&lt;m&gt;. 0 FM, 1 DV, 2 AM, 3 LSB, 4 USB, 5 CW, 6 NFM, 7 DR, 8 WFM, 9 R-CW | |
| IO | USB output select (Menu 102) | Read IO → IO &lt;p&gt;; set IO &lt;p&gt;. 0 AF, 1 IF, 2 Detect | The D74 reference labels 0 as "Off" |
| FT | Fine tuning on/off | Read FT; set FT 0 or FT 1 | Needed for 20 Hz steps |
| FS | Fine step | Read FS; set FS &lt;p&gt;. 0 = 20 Hz, 1 = 100 Hz, 2 = 500 Hz, 3 = 1 kHz | |
| SH | Radio's own receive filter | Read SH &lt;mode&gt; → SH &lt;mode&gt;,&lt;index&gt;; mode 0 SSB, 1 CW, 2 AM | Read only; record in the snapshot |
| BY | Squelch state | Read BY 1 → BY 1,&lt;0 closed, 1 open&gt; | Status indicator only |
| AI | Auto-information | Read AI; set AI 0 or AI 1 | On while connected, restored on exit |

The D74 reference describes SM as an S-meter squelch flag, not a signal level, so measure signal strength from the IF itself. Optional extras such as RA (attenuator) and BS (bar antenna) may be added later, after reading their pages in the reference. Menu 506 (Data Band) has no documented command; if the app turns out to need it, capture ARFC-D75's serial traffic on Windows with a port monitor to find it.

Safety allow-list — enforced in the CAT layer, not the UI:

- Only the commands in the table above may be sent, and the only writes allowed are FQ, MD, IO, FT, FS and AI. Every write targets band B.
- Hard-block, with a unit test per command: TX (transmit), BE and PT (APRS beacons, which transmit), TN and AS (KISS TNC mode, which takes over the port), 0M PROGRAM (programming mode), 0G KENWOOD (service mode), SR (reset), PS (power), ME and MR (memory channels), DW and UP (key emulation), BT and PC.
- The debug CAT console in the UI goes through the same allow-list. There is no bypass flag.

## Stack and architecture

Build one Go binary, d75scope, that owns the radio and serves the front end, compiled from TypeScript and embedded with go:embed. Split the Go code into packages so the DSP and radio code can be tested without the UI or the radio. Prefer the standard library; any third-party dependency needs a one-paragraph justification in docs/decisions/.

Stack:

- Go, current stable release. Keep cgo inside the audio package; everything else must build and test with CGO_ENABLED=0.
- Audio in and out: miniaudio through a Go binding such as malgo, or Core Audio directly through cgo. Select the radio's USB audio device explicitly by ID; never use the system default input.
- Serial: go.bug.st/serial, or a small termios wrapper over golang.org/x/sys/unix. Open the port exclusively.
- DSP: pure Go. Hand-written oscillators and FIR filters, and an FFT from gonum or a hand-written radix-2. At 48 kHz the load is tiny, so no SIMD is needed.
- HTTP and WebSocket: net/http plus a maintained WebSocket package such as github.com/coder/websocket.
- Front end: TypeScript in strict mode, no framework, built with esbuild. WebGL2 for the waterfall, Canvas 2D for the spectrum trace and overlays.
- Hot-plug: poll the serial-port and audio-device lists every second or two. It is simpler than OS notifications and portable to Linux.
- Settings, calibration and the radio-state snapshot: JSON files under os.UserConfigDir() (Application Support on macOS).

Packages:

1. dsp — oscillator and mixer, FIR design, decimator, FFT with windowing and averaging, demodulators, AGC, resampler, synthetic signal generator, WAV reader and writer. Pure Go, deterministic, fully unit-tested.
2. radio — device discovery, the CAT goroutine, command codec and allow-list, snapshot and restore, and a mock radio.
3. audio — the only cgo package: capture and playback on real devices. The WAV-file and synthetic IF sources live outside it, so tests never need cgo.
4. server — HTTP, embedded assets, the WebSocket protocol, and later the rigctld-compatible server.
5. web/ — the TypeScript front end.
6. cmd/d75scope — the program, including a --demo mode that runs on synthetic signals and the mock radio. cmd/d75probe — the hardware probe (see Milestones).

Concurrency model:

- The audio callback arrives on a C thread. It only copies samples into a preallocated lock-free ring buffer: no allocation, no logging, no blocking channel sends.
- One DSP goroutine pulls fixed-size blocks, runs the pipeline, publishes spectrum rows to a broadcast hub, and pushes demodulated audio into the playback ring buffer.
- One CAT goroutine owns the serial port. Everything else sends it requests over a channel, and it publishes radio state to the hub.
- Each browser connection gets its own writer goroutine with a small queue. A slow client drops spectrum rows; it never blocks the DSP.
- Preallocate DSP buffers so garbage collection stays out of the audio path; the soak test checks for underruns.

Interfaces (names are suggestions):

- IFSource — start, stop, sample rate, block callback. Implementations: USB audio device, WAV file, synthetic.
- Radio — identify, get/set frequency, mode and IF output, snapshot, restore, and an event stream for AI updates. Implementations: serial CAT and mock. Keep it transport-agnostic so this binary can grow into a general radio daemon.
- AudioSink — playback device, WAV recorder, and a null sink for tests.

Wire protocol (versioned, documented in docs/PROTOCOL.md):

- GET / serves the page and GET /ws upgrades to one WebSocket per client. Check the Origin header on upgrade.
- Text frames carry JSON. From the server: a hello with the protocol version, state (dial frequency, radio mode, IF output, calibration, devices, banners), meter at about 10 Hz, and CAT console lines. From the client: tune, recentre, mode, filter, AGC, volume, record, calibrate and console commands.
- Binary frames carry spectrum rows: a small little-endian header (sequence number, centre RF frequency in Hz, bin width in Hz, bin count), then one byte per bin at 0.5 dB per step, where 255 is 0 dBFS and 0 is −127.5 dBFS or below. 4096 bins at 30 rows a second is about 120 KB/s.
- The client owns floor, range and colour map, so contrast changes need no round trip to the server.

## DSP pipeline

Treat the input as a real IF at the device rate, shift the 12 kHz IF to complex baseband, decimate by 2, and drive both the display and the demodulator from that one complex stream. The numbers below assume a 48 kHz device; derive every constant from the measured rate.

The dial frequency comes only from CAT read-back; everything else runs off the one 24 kHz complex stream. The numbered steps below are the specification.

1. Capture. Float32 at the device rate. If the device is stereo, check whether the two channels are identical; if they are not, they may be I and Q, so record that in the probe report and stop to ask. Show input level, and warn above −1 dBFS so the owner can lower Menu 91A.
2. Downconvert. Multiply by a complex oscillator at the IF centre. At exactly 48 kHz this is fs/4, a four-step pattern of 1, −j, −1, +j with no trig at all. Keep this stage exact and apply calibration trims later, at baseband.
3. Filter and decimate by 2 to 24 kHz complex. The real IF's mirror image lands at ±16.5 kHz and above after mixing, and decimating to 24 kHz folds it straight onto the wanted band. The anti-alias FIR must pass ±7.5 kHz and reject at least 80 dB from ±16.5 kHz. That wide transition keeps the filter short. For another device rate, recompute where the image lands, then pick the integer factor that keeps the output at or above 20 kHz.
4. Spectrum. FFT the 24 kHz stream with a 4-term Blackman–Harris window. Offer sizes 2048, 4096 (default, about 5.9 Hz per bin) and 8192. Set the hop for about 30 frames per second, average power with a selectable time constant, and output dBFS, quantised to one byte per bin for the wire. Show ±7.5 kHz around the IF centre and dim anything beyond it.
5. Waterfall rows. Send one quantised row per frame to every connected browser; the browser draws the waterfall (see UI). Each row header carries the RF centre frequency, so a retune shows up in the client as a marker line, and history shifts sideways when the change stays within the span. Colour map, floor, range and noise-floor tracking all live in the client.
6. Channel select. A second oscillator shifts the chosen signal to 0 Hz; its offset is the software VFO. Then apply the mode's channel filter:
   - USB: complex band-pass +300 to +2700 Hz by default, width adjustable 1.8–3.0 kHz. Take the real part.
   - LSB: the mirror, −2700 to −300 Hz.
   - CW: 500 Hz wide by default, adjustable 100–1000 Hz, then a beat oscillator at the CW pitch (default 700 Hz).
   - AM: ±4.5 kHz by default, adjustable 3–6 kHz, envelope detector, DC removed.
   - NFM is a stretch goal, only if the probe shows IF output works in FM mode.
7. AGC. Fast attack (about 5 ms), selectable decay (fast about 100 ms, slow about 1 s), hang time for SSB, a maximum gain cap, and a −12 dBFS target.
8. Audio out. Resample to the playback device's rate with a polyphase resampler and push it through a ring buffer to the playback device, on the Mac, from the Go process. On underrun, play silence and count the event. Streaming audio to the browser is a later feature.
9. Signal meter. Channel power in dBFS from the channel filter output. Label it relative; the radio offers no calibrated S-meter.
10. Recording. Three formats: the raw IF as captured, the 24 kHz complex stream as a 2-channel float WAV, and the demodulated audio. Every recording gets a JSON sidecar with RF centre frequency, start time, sample rate, mode and calibration.

Target under 150 ms from a click to hearing the signal.

## Frequency mapping and calibration

Every frequency the app shows comes from one formula, and calibration only sets its two unknowns per mode: an inversion sign and an offset.

$$f_{RF} = f_{dial} + \Delta_{mode} + s \cdot f_{bb}$$

Here f_dial is the band B frequency read back with FQ 1, f_bb is a position in the complex baseband (−7.5 to +7.5 kHz from the IF centre), s is +1 or −1 for spectral inversion, and Δ_mode is where the dial frequency sits relative to the IF centre in that mode. Δ_mode will probably differ between USB, LSB, CW and AM.

Calibration wizard:

1. Tune near a carrier of known frequency. European medium-wave broadcast carriers sit on an exact 9 kHz raster and make good references; a signal generator also works.
2. Find the carrier's peak bin, with parabolic interpolation for sub-bin accuracy.
3. Retune 1 kHz up, with fine tuning on, using FQ. If the carrier moves 1 kHz down in baseband, s = +1; if it moves up, s = −1.
4. Solve for Δ_mode from the formula, then repeat in USB, LSB, CW and AM.
5. Check the residual error at −5, 0 and +5 kHz across the span by retuning; it must stay under 10 Hz.
6. Store the result keyed by the radio's identity and firmware version (from ID, FV and the USB serial number). Offer to re-run it after a firmware update.

Coarse and fine tuning:

- The radio tunes only in its own steps (20 Hz at best, in fine mode). The app tunes coarsely with FQ, reads the result back, and covers the remainder with the software VFO, so clicks land with 1 Hz resolution.
- A click inside the span moves only the software VFO; no CAT traffic. Recentring is explicit (double-click), or automatic when the VFO comes within 1 kHz of the span edge.
- To recentre, set f_dial so the VFO lands at baseband 0, snapped to the radio's step, and keep the residual in software.
- Never assume a write took effect. The read-back and AI updates are the only source of truth for f_dial, including knob turns on the radio itself.
- After any retune, mark about 200 ms of waterfall as settling and exclude it from averaging.

## UI and interaction spec

A single-page web app: a status bar across the top, the spectrum above the waterfall, and a control sidebar on the right. Dark appearance first; light must also work. Target current desktop Chrome on macOS; other browsers are not tested in v1.

Layout:

- Status bar: connection state (server, radio model, firmware, audio device, serial port), dial frequency as an editable field, radio mode, IF output state, input level meter with a clip light, calibration status for the current mode, and a record button.
- Spectrum (about a third of the height): dBFS axis, averaged trace with optional peak hold, the channel filter drawn as a shaded passband at the software VFO, and dimmed edges beyond ±7.5 kHz.
- Waterfall (the rest): same frequency axis, newest row at the top.
- Frequency axis: absolute RF in kHz (for example 7074.0), major ticks every 1 kHz, minor ticks every 250 Hz, adapting to zoom.
- Sidebar: demod mode, filter width, AGC (off, fast, slow), volume and mute, audio output device (listed by the server), CW pitch, FFT size, averaging, waterfall floor, range and auto, and zoom from the full 15 kHz down to 3 kHz.

Rendering:

- Waterfall: a WebGL2 texture used as a ring buffer. Upload one row per frame with texSubImage2D, offset the read position in the shader, and map bytes to colour through a 256-entry lookup texture.
- Downsample rows to the canvas width with max-hold, so narrow CW signals survive.
- Spectrum trace and overlays on a Canvas 2D layer above it. Honour devicePixelRatio, and draw only when a row arrives.
- Auto floor tracks the noise (for example the 20th percentile of each row), computed in the client.

Mouse and keyboard:

- Click sets the software VFO. In USB and LSB the click marks the suppressed carrier and the passband is drawn to its right or left. In CW and AM it marks the carrier.
- Drag the passband to fine-tune; drag its edges to change filter width. Scroll tunes 10 Hz per step, Shift for 100 Hz, Option for 1 Hz.
- Double-click recentres the radio on that signal. Hovering shows frequency and level under the pointer.
- Keys must not clash with browser shortcuts: arrows step the VFO, Shift-arrows move 1 kHz, R recentres, Space mutes, [ and ] change filter width, M cycles the mode, and the backtick opens the CAT console.
- A typed frequency tunes the radio, validated against band B's 0.1–524 MHz range.

Status and errors. Show these as banners with a fix where one exists:

- Lost connection to the server, with automatic reconnect and a state resync.
- Radio not found, or the serial port held by another program.
- IF output is off (Menu 102), with a one-click fix that snapshots state and then sends IO 1.
- NMEA, APRS or KISS traffic on the USB port, telling the owner to set Menus 981–983 to Bluetooth.
- No calibration for the current mode, or no microphone permission (capture returning only silence).
- The radio left in a modified state by a previous session (see Radio-state safety).

Never block the UI on a server round trip; show a pending state instead. Several clients can be open at once, and all show the same radio state.

CAT console (hidden by default): timestamped traffic in both directions, an input line limited by the server's allow-list, and a copy-log button.

Accessibility: labelled controls, full keyboard operation, and the tuned frequency announced through a live region, at most once a second.

## Radio-state safety

The program changes at most six settings (FQ, MD, IO, FT, FS, AI). It records each one before touching it, and puts them back on quit, or on the next connection after an unplug or a crash.

Snapshot:

- On connect, before any write, read ID, FV, FO 1, MD 1, IO, FT, FS, AI and SH for all three modes.
- Write the snapshot to Application Support atomically (temporary file, then rename), with a timestamp and a dirty flag, before the first write command goes out.
- Restore with the same allowed write commands, never with FO. Restore only what the app changed, in reverse order, and read each value back.
- If the owner changes a setting on the radio by hand after the app changed it, leave the owner's value alone. Restore a field only if it still holds the value the app last wrote.
- Clear the dirty flag only after every restored value reads back correctly.

Triggers:

- Normal quit: restore before exit, capped at 2 seconds, triggered by SIGINT, SIGTERM or the page's quit button. A second Ctrl-C exits at once and keeps the dirty snapshot.
- Unplug or radio off: restore is impossible, so keep the dirty snapshot. On reconnect, show a banner offering to restore the previous session's settings, defaulting to yes.
- Crash: the next launch finds the dirty snapshot and shows the same banner on connect.
- Sleep and wake: re-check the connection and re-read radio state before trusting the cached state.

Serial-port hygiene:

- If the port carries NMEA, APRS or KISS traffic, send no writes until it is clean (see CAT protocol).
- If the radio is in KISS mode, CAT will not answer. Detect it (no reply to ID, or 0xC0 framing) and ask the owner to leave KISS mode on the radio. Never send KISS exit sequences automatically.
- Open the port exclusively (TIOCEXCL) so no other app can talk to the radio mid-session.

Sharing the radio (milestone M5): because this binary owns the port, expose a rigctld-compatible TCP server on localhost so WSJT-X or a logger can read and set frequency and mode through it. Support the subset WSJT-X uses: f, F, m, M, v, V, t, \dump_state and q. t always reports receive, and T (set PTT) returns an error. The server goes through the same allow-list.

## Testing without hardware

You will not have a radio, so every layer must run against a fake: a synthetic IF generator, recorded IF files, and a mock radio that speaks CAT. go test ./... must pass with CGO_ENABLED=0, no hardware and no network; the cgo audio package is left out by build tag.

Fakes:

- Synthetic IF source: generates the real 12 kHz IF at any sample rate from a list of signals at chosen offsets: a CW carrier, keyed Morse text, two-tone SSB, an AM carrier with tone modulation, and band-limited noise. It can also inject spectral inversion, a frequency offset, a deliberate mirror image and clipping.
- WAV-file IF source: plays raw-IF recordings in real time or as fast as possible, with loop and seek. The owner will supply real recordings from the probe; until then, generate fixtures with the synthetic source.
- Mock radio: implements Radio in process, plus a pseudo-terminal variant that puts a fake D75 behind a real device path so the serial CAT code is tested end to end. It supports the allowed commands, ? and N errors, adjustable latency, unsolicited AI lines, injected NMEA or KISS bytes, and an unplug mid-session. It logs every command it receives, so tests can assert nothing outside the allow-list was ever sent.

DSP tests (dsp):

- Oscillator phase stays continuous across block boundaries; the fs/4 shortcut matches the general oscillator to within 1e-6.
- Anti-alias filter: at least 80 dB rejection from ±16.5 kHz, at most 0.1 dB ripple across ±7.5 kHz.
- Image rejection end to end: a tone at the mirror position comes out at least 75 dB below the wanted tone.
- Frequency mapping: a synthetic tone at a known RF frequency displays within 2 Hz after peak interpolation, for both s = +1 and s = −1.
- Demodulators: a USB tone 1 kHz above the carrier gives 1 kHz audio with the opposite sideband 40 dB down; keyed CW gives a tone at the set pitch; AM with a 1 kHz tone gives 1 kHz audio under 1% distortion.
- AGC step response stays within its time constants and never exceeds 0 dBFS. The resampler stays clean and drift-free over 10 simulated minutes.

Radio tests (radio):

- The codec parses and formats every command in the table, including malformed and error replies.
- One test per blocked command, through both the normal path and the console path.
- Snapshot and restore: change each field, simulate quit, unplug and crash, and verify the exact restore, including the rule about settings the owner changed by hand.
- NMEA or KISS bytes on the port hold back all writes and raise the banner.

Server tests (server):

- Golden tests for the binary row encoding and every JSON message type.
- End to end with the synthetic source and mock radio: connect a WebSocket client, receive hello, state and rows, send a tune command, and assert the mock radio received the snapped FQ value.
- Access control: the server refuses to bind anywhere but localhost, and WebSocket upgrades with a foreign Origin are refused.

Front-end tests:

- Unit tests for row decoding, frequency-to-pixel mapping and the colour lookup.
- Playwright tests against d75scope --demo in Chromium: click-to-tune, recentring, mode changes and the calibration wizard. Feed the wizard a carrier with a known injected offset and inversion; it must recover both within 10 Hz.

Performance budgets (on an M1 MacBook Air):

- The Go process under 10% of one core at 4096-point FFT, 30 rows per second, with the demodulator running.
- Chrome holds 30 frames per second on the waterfall.
- No audio dropouts over a 60-minute soak, with garbage-collection pauses logged.
- Under 150 ms from a tune command arriving to the first demodulated output sample.

Put tests that need the radio behind a hardware build tag.

## Milestones and acceptance criteria

Build in this order and do not start a milestone until the previous one meets its criteria. Hardware checks are run by the owner from a checklist you write; everything else you verify yourself.

1. M0 — Probe CLI (cmd/d75probe). Build it first: it answers the unverified radio facts.
   - Read-only by default: list audio devices and their formats, find the radio's audio device and serial port, and read ID, FV, FO 1, MD 1, IO, FT, FS, AI, SH and BY 1.
   - With --allow-writes: snapshot, set IO 1, record 10 s of IF in USB, LSB, CW and AM, then 10 s of Detect output. Tune 1 kHz up and record again (for the inversion test), then restore and verify.
   - Output a probe-report.json, the WAV files, and a plain-language summary.
   - Done when it runs green against the mock radio and synthetic source, and the owner has run it on the radio and you have folded the results into HARDWARE_QUESTIONS.md.
2. M1 — Engine and page in demo mode. Synthetic and WAV sources, the DSP pipeline, the WebSocket protocol and the waterfall page.
   - Done when synthetic tones land at the correct positions in the browser, Chrome holds 30 frames per second, and the performance budgets pass.
3. M2 — Live radio. Discovery, the CAT goroutine, snapshot and restore, live IF from the device, dial readout, AI updates, frequency axis in RF.
   - Done when the end-to-end mock tests pass, and the owner sees live band B with frequency labels within a few hundred hertz (before calibration).
4. M3 — Demodulation and audio. USB, LSB, CW and AM, AGC, playback from the Go process, output device selection, click-to-tune, software VFO and recentring.
   - Done when the demodulator tests pass, click-to-audio is under 150 ms, and the owner can tune and copy an HF SSB signal.
5. M4 — Calibration and recording. The calibration wizard, per-mode persistence, and the three recording formats with sidecars.
   - Done when the wizard recovers injected parameters within 10 Hz, and on the radio the residual error stays under 10 Hz across the span.
6. M5 — Polish and sharing. The rigctld-compatible server, CAT console, accessibility pass, settings, error banners, README and the .app wrapper script.
   - Done when WSJT-X tunes the radio through rigctld and decodes FT8 from the program's audio routed through a virtual audio device (owner check).

## Working rules, deliverables and non-goals

Plan before you code, keep every unknown visible, and stop to ask the owner whenever a decision depends on the real radio.

Working rules:

- Start by writing docs/PLAN.md: module breakdown, build order and the risks you see. Keep it current.
- Keep HARDWARE_QUESTIONS.md: each unverified fact, how the probe answers it, and the answer once known. Code reads these values from configuration or the probe report, never from a hard-coded guess.
- Never invent CAT commands, parameters or reply formats. Anything not in the CAT table goes into HARDWARE_QUESTIONS.md as a question.
- When the owner's probe results contradict this prompt, the probe wins. Update the docs and say what changed.
- Finish each milestone with green tests, a changelog entry, and a hardware checklist: exact steps for the owner and what to send back.
- Commit in small, single-purpose steps with descriptive messages.
- Ask before adding a dependency, sending any CAT write outside the allowed list, or changing the minimum Go or macOS version.

Permissions:

- macOS puts every audio input, USB devices included, behind the microphone permission. A command-line binary inherits it from the terminal that starts it, so during development run from Terminal and grant Terminal access.
- To run as a background service, provide a script that wraps the binary in a minimal signed .app with NSMicrophoneUsageDescription, plus a launchd agent.
- A refused permission often shows up as all-zero samples rather than an error. Detect that and say so in a banner.

Owner setup (goes in the README):

- Menu 980 set to COM+AF/IF Output. The program manages Menu 102 itself through IO.
- Menus 981–983 set to Bluetooth, and KISS mode off.
- Menu 91A: start in the middle of 1–7, then adjust until the input meter peaks below −6 dBFS.
- An antenna on band B suited to the band being watched; for HF, a wire on the SMA connector.
- Run d75scope and open http://localhost:8075 in Chrome.

Deliverables:

- A Go module with cmd/d75scope, cmd/d75probe, the packages above and web/, plus a Makefile with build, test, web and app targets. make build produces one binary with the front end embedded.
- README covering setup, the radio menus, running the probe, and routing audio to WSJT-X through BlackHole.
- docs/PLAN.md, docs/PROTOCOL.md, HARDWARE_QUESTIONS.md, one HARDWARE_CHECKLIST per milestone, docs/CAT.md (the commands verified on the TH-D75, with firmware version) and docs/decisions/.
- Synthetic WAV fixtures and the JSON schema of the probe report.

Non-goals for v1:

- Transmitting, or any transmit-related control.
- iPad and phone use, browsers other than Chrome, Bluetooth audio, and Windows builds. Linux should compile but is not tested in v1.
- D-STAR, APRS, KISS, memory programming (CHIRP covers that) or firmware changes.
- Decoding digital modes inside the program; route audio to WSJT-X or fldigi instead.

Later, not now: audio streamed to the browser, network access behind a token for other devices, headless running on a Raspberry Pi, MQTT and Prometheus endpoints, a band sweep that stitches several tunings into a wider view, DX cluster and PSKReporter spots on the waterfall, bookmarks, NFM, and experiments with the Detect output.

## References

Read these before writing the CAT and DSP layers. Where they disagree with this prompt, flag it rather than guess.

- LA3QMA TH-D74 command reference, with per-command pages for FQ, FO, MD, IO, FT, FS, SH, BY, SM and AI.
- TH-D75A/E user manual: the full menu table, including Menus 102, 120–122, 170, 506, 91A and 980–985.
- Kenwood TH-D75E specifications: USB functions, IF and Detect outputs, Bluetooth profiles, band B receiver chain.
- Kenwood TH-D75E brochure: IF centre and bandwidth, bandscope use, 20 Hz fine steps.
- ARFC-D75 page: which menus Kenwood's tuner changes and its unclean-disconnect warning.
- TH-D74 user manual: what the IF and Detect outputs carry.
- CHIRP issue #4129: how the D74 enumerates on USB, serial settings that worked, and error replies seen in practice.
- trx-control application notes: routing CAT, GPS and APRS between USB and Bluetooth on the TH-D75.
- Prior art to read, not copy: codeplugtool, a Java CAT library for the D72/D74, and a Swift macOS CAT controller for the D74/D75.
