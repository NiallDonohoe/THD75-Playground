# 0003 — Order of the probe's write sequence

Date: 2026-10-23 · Status: accepted, deviates from the spec's wording

The spec lists the write run as: snapshot, IO 1, record IF in USB, LSB, CW
and AM, record Detect, tune 1 kHz up and record again, restore. The probe
keeps every step but changes the order and adds four, all using only the
allowed writes:

1. **Per-mode pairs.** Each mode is recorded at the dial and then 1 kHz up
   before moving to the next mode, so each inversion measurement compares
   two recordings seconds apart (less fading, less drift) and inversion is
   measured in every mode, not just one.
2. **Fine tuning first** (FT 1, FS 0, in USB) so the 1 kHz retune is
   possible, plus a **resolution test** (dial + 1010 Hz, read back) for Q15.
3. **FM and NFM** recordings for Q11.
4. **AI test last.** It needs the owner to turn the dial, which would move
   the radio off the reference carrier if it came first. Afterwards the
   probe writes the dial back to where it was, as an ordinary write, so
   restore still recognises FQ as the program's value and puts the original
   back; the owner-changed rule is not bypassed.

Detect is recorded after the IF modes, as in the spec.
