# 0007 — d75ft8: a standalone program, the radio's AF output, receive only

Date: 2026-10-29 · Status: accepted

The owner asked for an FT8 mode that decodes FT8 signals. The brief
(SPEC.md) makes in-program digital-mode decoding a v1 non-goal for
d75scope, whose milestones are also still waiting on the M0 probe run. So
FT8 is a third program, `d75ft8`, with its own spec
([../FT8.md](../FT8.md)) and milestones F0–F2. d75scope's constraints and
the CAT allow-list are unchanged.

The owner chose:

- **A standalone program**, over a tab in d75aprs (which needs the TNC in
  KISS mode, so there is no CAT to tune with) or a mode inside d75scope
  (which would wait for M1–M3). It owns the USB audio and the CAT port while
  it runs, so it never runs alongside d75scope or d75aprs.
- **The radio's AF output with band B in USB** (Menu 102 = AF, `IO 0`),
  over the IF and our own SSB demodulator. The radio does the SSB, and the
  0–3 kHz audio is what FT8 needs. It does not depend on the open IF
  questions (inversion, centre, Q07–Q09). It raises its own: what the USB
  AF carries and how wide it is (F01–F05).
- **A browser page** in the d75aprs style (plain JavaScript, server-sent
  events, localhost only), with an audio waterfall, a decode list and a band
  picker, and a `--demo` mode first (F1), as the owner asked for d75aprs.

Receive only follows from the hardware: nothing can be sent into the radio
over USB audio, so there is no transmit path. The CAT writes (`IO`, `MD`,
`FQ` on band B) are all already allowed, and go through `radio.Guard` for
snapshot and restore.

Consequences: new radio questions F01–F05 in HARDWARE_QUESTIONS.md,
answered by the F2 checklist. Q01 (the audio format) and Q06 (whether `IO 0`
is AF) from the probe apply here too.
