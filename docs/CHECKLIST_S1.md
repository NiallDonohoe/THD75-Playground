# Checklist S1 — the d75sat page

No radio is needed. Part 1 runs on made-up satellites (DEMOSAT-1 to 5,
catalogue numbers 99901 on) with a clock running ten times fast, so a pass
plays out in about a minute. Part 2 uses the real data from CelesTrak and
SatNOGS. Nothing is transmitted, and the radio is not touched.

## Part 1: demo

```sh
make build
./bin/d75sat --demo
```

Open http://127.0.0.1:8078/ in Chrome (type the address; the page refuses
requests that come from other sites). The demo starts at your position if
you give `--lat` and `--lon`, otherwise at Dublin. It remembers nothing
between runs.

1. **Status bar**: "Demo · clock ×10", "Listen only", the location chip
   (a locator and "demo default"), CelesTrak and SatNOGS as demo data, a
   UTC clock running fast, and a green "live".
2. **Next pass**: DEMOSAT-1 rises within about two minutes of starting
   (20 s of real time) and peaks above 45°. The countdown runs, then the
   card turns green, "in view", with time to set.
3. **Map**: five satellites; any in view are green, with a light footprint.
   Click one, or a row of the pass table:
   - it turns blue;
   - its footprint fills;
   - its ground track is drawn, dashed behind it and solid ahead;
   - a dashed circle round "you" shows where it has to be for you to hear
     it.

   ⊕ follows it. Pan and zoom out to the whole world: tracks and
   footprints cross ±180° and the poles without jumps, and grid labels stay
   within ±180°. Dark and light mode both work.
4. **Selected satellite**: its facts, a sky plot of its current or next
   pass with AOS and LOS marked, and the dot moving along the track while
   it is up. The transmitters each show the frequency heard now, with the
   Doppler shift. At AOS it is above the listed frequency and at LOS below
   it: about ±3.5 kHz on 2 m and ±10 kHz on 70 cm. DEMOSAT-3 and 4 have
   linear transponders, shown as passbands.
5. **Passes**: the next day, by AOS (UTC), with length, peak and rise and
   set directions. "Peak at least" filters it. A pass in view says so in
   green. Clicking a row shows that pass in the sky plot.
6. **Location**: click the location chip.
   - **"Use this Mac's location"**: Chrome asks for permission once
     (allow it). The chip then shows your locator and "Mac location", and
     the passes change. If macOS Location Services is off for Chrome, the
     panel says so instead of failing silently.
   - **"Pick on the map"**: the cursor becomes a crosshair; click
     somewhere and the chip says "picked on the map". Esc cancels.
   - **A locator**: `IO63` or `IO63wi` works; `ZZ99` is refused with a
     message.
7. **Alerts**: select DEMOSAT-2 and click 🔔 Alert. Chrome asks to allow
   notifications (allow it). The button turns blue, and the satellite's
   pass rows and list entry get a bell. About five minutes before its next
   pass peaking at 10° or more, a banner appears at the top of the page and
   a macOS notification comes up. At ×10 that is 30 s of real time, which
   needs the page open. Clicking the notification selects the satellite.
   Change "min before AOS" and "peaks ≥" and Save.

## Part 2: real data

```sh
./bin/d75sat
```

1. Within a few seconds, CelesTrak and SatNOGS show "now" or "0 min old".
   Set your location as in step 6. The satellite list holds the amateur
   satellites with a live transmitter the radio can hear; expect several
   dozen, including the ISS. Any marked "not predicted" are in deep-space
   orbits.
2. Pick the ISS. Its next pass should agree with Heavens-Above or Gpredict
   for your location to within a few seconds, and its peak to within a
   degree. Its transmitters should include 145.800 MHz (FM voice) and
   145.825 MHz (APRS).
3. Quit and start again: your location and alerts are remembered
   (`~/Library/Application Support/d75sat/config.json`). The data comes
   from the cache in `~/Library/Caches/d75sat`, with no new download for
   2 hours (orbits) and a day (transmitters).
4. `./bin/d75sat --offline` runs from the cache, with both sources
   marked "cached".

## Send back

- Anything that looked wrong, with a screenshot.
- The satellite count from Part 2, and how the ISS pass compared.
- Whether "Use this Mac's location" worked first time, or what it said.
- Whether the macOS notification appeared.
