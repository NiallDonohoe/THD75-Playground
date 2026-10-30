# d75sat — listening to satellites

Living spec for the fourth program in this module, agreed with the owner on
30 Oct 2026. The decisions behind it are
[0009](decisions/0009-d75sat-standalone-listen-only.md),
[0010](decisions/0010-own-sgp4-near-earth.md) and
[0011](decisions/0011-celestrak-and-satnogs.md). The radio facts it depends
on are the S questions in [../HARDWARE_QUESTIONS.md](../HARDWARE_QUESTIONS.md).

## What it is

`d75sat` helps the owner listen to amateur satellites on the TH-D75:

- upcoming passes for the station, with a countdown to the next one;
- a map with each satellite's ground track and coverage footprint and the
  station's own visibility circle, and a polar sky plot of the pass;
- during a pass, band B tuned to the downlink with Doppler correction, in
  the right mode, and put back afterwards;
- an alert in the browser a few minutes before a chosen satellite rises.

It is a separate program, like d75aprs and d75ft8, and never runs alongside
them. d75aprs cannot do this job: in KISS mode CAT stops answering, so it
cannot tune for Doppler.

## Rules

- **Listen only.** The program writes band B and nothing else: `FQ`, `MD`,
  `FT` and `FS`, all already on `radio`'s allow-list, through `radio.Guard`
  (snapshot before the first write, restore at the end of the pass, on
  exit, or on the next start after a crash). Uplinks are set by hand on the
  radio. Nothing it sends can make the radio transmit.
- **Localhost only**, with d75aprs's Host, Origin and Sec-Fetch-Site checks.
- **Outbound connections** go only to CelesTrak and the SatNOGS database
  (0011) and the map tile server, each of which can be turned off. None of
  them is sent the station's position.
- `go test ./...` passes with no radio and no network.

## Location

The station's position comes from the Mac (0009):

- **"Use this Mac's location"** on the page asks Chrome for the position
  (Wi-Fi location; `localhost` is a secure context, so the browser allows
  it). The page sends it to the program, which saves it in its config file.
- **Fallbacks**: click the map, or type a Maidenhead locator (6 characters
  is a few km, which is plenty).
- A 10 km error moves AOS and LOS by a second or two and elevation by well
  under a degree, so Wi-Fi accuracy is more than enough.
- The position is never sent anywhere except the program on localhost.
- Later, for running headless: the radio's GPS over Bluetooth serial
  (Menu 981 = Bluetooth), which keeps NMEA off the USB CAT port.

## Radio setup

| Setting | Why |
| --- | --- |
| Menu 980 = COM+AF/IF Output | CAT on USB |
| Menus 981–983 on Bluetooth (or off) | NMEA, APRS or KISS on the USB port holds every CAT write |
| Band B in VFO mode | `FQ` cannot tune a memory channel (S05) |
| Squelch on band B open, or low | FM satellites are weak; the program does not set squelch |
| An antenna for 2 m and 70 cm, ideally a handheld Yagi | |

## Satellites and frequencies

- **Orbits**: CelesTrak's amateur group, as OMM JSON (it carries catalogue
  numbers that no longer fit a TLE), fetched at most once every 2 hours and
  cached on disk.
- **Transmitters**: the SatNOGS database, refreshed once a day and cached.
  Kept: transmitters marked alive and active, with a downlink band B can
  receive (0.1–524 MHz) in a mode the radio can demodulate:
  - FM voice;
  - SSB and CW on linear transponders;
  - CW beacons;
  - FM data (AFSK, FSK, GMSK), heard as audio.
- The page credits both, and the SatNOGS data is shown under its licence
  (CC BY-SA 4.0).
- **Stale data**: elements older than 14 days are flagged on the page.
  With no network, everything runs from the cache.

## Doppler tuning (S2)

- On selecting a transmitter for a pass, the program sets band B's mode:
  - FM voice and data → FM;
  - USB or LSB → USB or LSB (as the transponder's downlink is);
  - CW → CW.

  In SSB and CW it turns fine tuning on at 20 Hz (`FT 1`, `FS 0`).
- **Frequency**: through the pass it tunes to the downlink as heard on the
  ground, f × (1 − range rate / c). It retunes when the error reaches half
  the radio's step: 20 Hz in SSB and CW, the channel step in FM (S01). It
  sends at most one `FQ` a second (S02).
- **Linear transponders**: the page picks the frequency within the
  passband. If the owner turns the dial (an unsolicited `FQ` with AI on),
  the program takes the new frequency, less the current correction, as the
  frequency at the satellite and keeps correcting from there.
- **At LOS**, or when tracking is stopped, the radio is restored to its
  snapshot.
- Doppler on the 2 m downlinks is ±3.5 kHz, and on 70 cm ±10 kHz.

## Page (S1)

One page on `http://127.0.0.1:8078/`, in plain JavaScript like d75aprs,
using the same map moved to a shared `web/map`:

- **Map**:
  - ground tracks for the selected satellites, one orbit back and one
    ahead;
  - each satellite's footprint (its radio horizon) and current position;
  - the station and its own visibility circle at the minimum elevation.
- **Satellite list**: name, the next pass, and its transmitters.
- **Pass table**: the next 24 hours, with AOS, length, maximum elevation,
  and AOS and LOS azimuths.
- **Sky plot**: the selected pass on a polar plot, with the satellite's
  live position.
- **Tuning panel** (S2): transmitter, mode, the frequency now, the Doppler
  correction, and start and stop.
- **Alerts**: a per-satellite toggle that shows a browser notification
  (and an in-page banner) a few minutes before AOS. Only passes above a
  minimum peak elevation trigger it (default 5 min before, 10°). The page
  has to be open.

## Packages

| Package | Role |
| --- | --- |
| `orbit` | Elements (TLE, OMM JSON), SGP4 (near-Earth), frames, look angles, passes, Doppler, footprints and ground tracks. Pure Go, no I/O. |
| `satdata` | (S1) CelesTrak and SatNOGS clients with disk cache, rate limits and filtering. |
| `satapp` | (S1) Satellites, passes and alerts for the page; (S2) the Doppler loop. |
| `satweb` | (S1) HTTP server, embedded page, server-sent events. |
| `web/map` | (S1) The canvas map and its maths, moved from `web/aprs` and shared. |
| `web/sat` | (S1) The page. |
| `cmd/d75sat` | `passes` now; the page from S1, tuning from S2. |

CAT, discovery, the allow-list and snapshot/restore are `radio`, reused
unchanged. No USB audio is used.

## Testing without the radio or the network

- **SGP4**: Vallado's verification cases for every near-Earth satellite,
  at three times each, to 1 cm and 0.1 mm/s; deep-space sets refused.
- **Frames**: GMST against Vallado's worked example, and geodetic round
  trips.
- **Passes**: against a one-second brute-force scan (AOS and LOS to 1.5 s,
  peak elevation to 0.05°), and passes already in progress.
- **Footprints**: an observer on the 10° edge sees the satellite at 10°.
- **Fuzzing**: the TLE and OMM parsers, whose input comes from the
  network.
- **Clients** (S1): fake CelesTrak and SatNOGS servers through `httptest`.
- **Doppler loop** (S2): a fake clock and the fake radio, checking the
  step size, the rate limit, knob-follow and restore at LOS.
- **`--demo`** (S1): bundled elements and transmitters, and a clock that
  can run fast, so a whole pass plays out in a minute.

## Milestones

Each ends with green tests, a changelog entry and an owner check. They do
not wait for the other programs' milestones.

1. **S0 — Orbit core.** `orbit` and `d75sat passes`. Built and green. Owner
   check, optional and with no radio: compare a day of ISS passes with
   Heavens-Above or Gpredict.
2. **S1 — Page, in demo mode.** Data clients and cache, the shared map,
   satellite list, pass table, sky plot, tracks and footprints, location,
   alerts and `--demo`. Done when the owner has worked through checklist S1
   in Chrome.
3. **S2 — Doppler tuning.** The tuning loop on the radio through `Guard`,
   the mode per transmitter, knob-follow and restore at LOS. Done when the
   owner has heard an FM satellite and followed a linear transponder
   through a pass (checklist S2, answering S01–S05).

Later: the radio's GPS over Bluetooth; recording pass audio; rotator
tracking through antenna-rotator-server; SDP4 (deep space), if a
high-orbit satellite worth hearing appears.

## Non-goals

- Uplinks, transmitting, or setting band A.
- Decoding telemetry, APRS or SSTV (route the audio to other software).
- Running alongside d75scope, d75aprs or d75ft8.
