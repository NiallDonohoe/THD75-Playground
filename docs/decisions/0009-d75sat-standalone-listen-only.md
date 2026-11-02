# 0009 — d75sat: a standalone program, listen only, the Mac's location

Date: 2026-10-30 · Status: accepted

The owner asked for a way to listen to satellites that tunes the radio for
Doppler and shows satellite coverage on a map. It is a fourth program,
`d75sat`, with its own spec ([../SAT.md](../SAT.md)) and milestones S0–S2.

The owner chose:

- **A standalone program**, over a tab in d75aprs or a mode in d75ft8.
  d75aprs runs the radio in KISS mode, where CAT stops answering, so it
  cannot tune; the FT8 program has nothing to do with satellites. The map
  is shared by moving it from `web/aprs` to `web/map`.
- **Listen only.** Band B is written (`FQ`, `MD`, `FT`, `FS`, all already
  allowed) and nothing else. Setting the uplink on band A was the
  alternative; it would have broken the allow-list rule that every write
  targets band B. The owner sets uplinks by hand.
- **CelesTrak for orbits and SatNOGS for frequencies** (0011).
- **AOS alerts** in the browser.
- **The Mac's location**, through the browser's geolocation on the page,
  with a map click or a Maidenhead locator as the fallback.

Location was settled after the radio's GPS was looked at and set aside.
With Menu 981 on USB, NMEA arrives on the CAT port every second, and the
session holds every write while foreign traffic is on the port
(`radio/session.go`), so Doppler tuning could never write. The
alternatives were:

- GPS over Bluetooth serial;
- exempting NMEA from the hold, which changes a safety rule;
- reading one fix, then switching Menu 981 back by hand;
- a CAT command for the position, which the D74 reference does not show.

None is needed: pass prediction is insensitive to position (10 km moves
AOS by a second or two), and Chrome's Wi-Fi location is good to tens of
metres. GPS over Bluetooth stays on the later list, for running headless.

Consequences: new radio questions S01–S05, answered by the S2 checklist.
The CAT allow-list and the session's write hold are unchanged.
