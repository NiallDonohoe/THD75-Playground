# M0 hardware checklist — run the probe on the radio

About 20 minutes. Nothing here can make the radio transmit: the probe's CAT
layer refuses every command outside a short read/tune allow-list, and the
blocked list (TX, beacons, KISS, programming and service modes, reset,
power, memories, key emulation, Bluetooth, output power) is enforced in code
with a test for each.

## 0. Before you start

- Note these settings on the radio, so you can check they come back:
  band B frequency and mode, **Menu 102** (USB Out Select), fine tuning
  on/off and its step.
- Note **Menu 170** (CW pitch), **Menu 91A** (USB audio output level) and
  **Menus 120, 121, 122** (SSB/CW/AM receive filters).
- Set **Menu 980** to **COM+AF/IF Output**.
- Set **Menus 981, 982, 983** (GPS, APRS, KISS output) to **Bluetooth**, and
  make sure KISS mode is off. (If you prefer, leave them and see whether the
  probe notices; it will refuse to write while that traffic is on the port.)
- Set **Menu 91A** to **4** for a start.
- Antenna on band B suited to what you will listen to. For medium wave or HF
  a wire on the SMA connector is enough.
- Pick a **steady carrier of exactly known frequency** for the offset
  measurement: a medium-wave broadcast station (9 kHz raster in Europe,
  10 kHz in the Americas), a time-signal station, or a signal generator.
  Tune band B to **2 kHz below it** in USB: for a carrier on 909 kHz, set
  the dial to 907.000 kHz. You should hear a 2 kHz tone. Keep the volume up
  so you can tell whether the speaker stays on during the probe.
- Build the probe if `bin/d75probe` is not there: `make build` (needs Go 1.27).

## 1. Find the radio (no radio commands)

Connect the radio by USB-C and switch it on.

```sh
./bin/d75probe --list
```

Expect an audio device named like **TH-D75** with at least one input, and a
`/dev/cu.usbmodem…` port with vendor `0x2166`. Keep the output.

## 2. Read-only run

Set **Menu 102** to **AF** first (this answers Q06), then:

```sh
./bin/d75probe -v
```

The first time, macOS asks whether Terminal may use the microphone: allow
it. (If you said no before: System Settings → Privacy & Security →
Microphone → Terminal.) If the summary says every sample was zero, that
permission is the reason.

This run only reads (ID, FV, FO 1, FQ 1, MD 1, IO, FT, FS, AI, SH 0–2,
BY 1) and records 5 seconds of whatever the radio is sending. It writes
`probe-out-<time>/` with `probe-report.json`, `summary.txt` and
`as-found.wav`.

## 3. Write run

Check the dial is still 2 kHz below your reference carrier, in USB. Then,
with your reference carrier's frequency in hertz:

```sh
./bin/d75probe -v --allow-writes --ref-carrier 909000 --note menu_91a=4
```

What it does (about 2½ minutes):

1. Saves a snapshot of band B's settings to
   `~/Library/Application Support/d75scope/radio-snapshot.json`.
2. Sets IO 1 (IF output), USB, fine tuning on with a 20 Hz step, and asks
   for dial + 1010 Hz to see how the radio rounds it.
3. In USB, LSB, CW and AM: records 10 s of IF, tunes 1 kHz up, records
   10 s more, and tunes back.
4. Records 5 s in FM and NFM (does IF output work there?).
5. Sets IO 2 and records 10 s of Detect output.
6. Turns AI on and asks you to **turn the band B dial two or three clicks
   and press Enter**. Afterwards it sets the dial back.
7. Puts every setting back as it found it, reads each one back, and says
   so in the summary.
8. Asks four questions: did the speaker keep playing, did anything on band
   A's display change, Menu 170's value, Menu 91A's value.

**Watch band A's display** during steps 2–6, for question 4 at the end.

If something goes wrong:
- **Ctrl-C once**: stops and restores the radio.
- **Ctrl-C twice**: exits at once and leaves the snapshot "dirty". The next
  write run refuses to start until you add `--restore-previous`, which puts
  the earlier settings back first.
- Exit code 2 or a summary that says "not fully restored": set the settings
  you noted in step 0 back by hand, and send me the report.

## 4. Optional runs (they answer Q10 and Q12)

- Change **Menu 170** (CW pitch) to a different value, then repeat step 3
  with `--note menu_170_cw_pitch=<new value>`. Comparing Δ_CW between the two
  runs shows whether the pitch moves the CW IF.
- Change **Menu 120** (SSB filter) to its narrowest setting and repeat step 3.
  If the IF width in Q07 does not change, the IF is taken before the radio's
  filters.

## 5. Check the radio

Confirm band B's frequency, mode, Menu 102 and fine tuning are as you noted
in step 0. Put Menus 91A, 170 and 120 back if you changed them.

## 6. What to send back

- The `--list` output from step 1.
- Every `probe-out-<time>/` folder, zipped (the WAV files matter: the M1
  DSP tests will use them as fixtures).
- Anything odd you saw or heard: the speaker, band A's display, messages
  on the radio's screen, or how the 2 kHz tone sounded in each mode.

I will fold the answers into HARDWARE_QUESTIONS.md and docs/CAT.md, record
any place where the radio contradicts the spec, and only then start M1.
