# Checklist A1 — the d75aprs page in demo mode

No radio is needed. The page runs on made-up traffic: every callsign starts
with Q. Nothing is transmitted, and the only network traffic is map tiles
from OpenStreetMap, through the program's cache.

## Run it

```sh
make build
./bin/d75aprs --demo --call YOURCALL-7 --lat 53.3498 --lon -6.2603
```

Use your own callsign and position; the made-up stations appear around it.
Then open http://127.0.0.1:8076/ in Chrome. To try real aprs.fi lookups,
start it with your API key in the environment:

```sh
D75APRS_APRSFI_KEY=… ./bin/d75aprs --demo --call YOURCALL-7
```

## Check

1. **Status bar**: your callsign, "Demo", "RF transmit off", and RF,
   APRS-IS and aprs.fi all marked as demo. "live" is green.
2. **Map**: within a few seconds, about a dozen stations near your position,
   with "you" marked. Pan by dragging, zoom with the wheel, the +/− buttons
   or double-click. ◎ returns to you. Dark and light modes both work (macOS
   appearance).
3. **Rings**: the legend says what each ring means. Look for:
   - a green ring (Q6VAN-7, Q7HIKE-7: RF only);
   - a blue ring (Q9FAR-9, Q0BOAT: IP only);
   - half and half (Q1HOME, Q5MOB-9: RF and IP);
   - a dashed amber ring after an aprs.fi lookup.
4. **Stations tab**: each row has RF / IP / RF+IP / aprs.fi tags, an age,
   and distance and bearing from you. The filter chips narrow the list and
   the map. Typing filters by callsign.
5. **Station detail**: click Q8FAR. It is RF+IP: its recent packet says "RF:
   third-party, gated from APRS-IS by Q3GATE · IP: sent over the internet".
   Click Q5MOB-9: Mic-E "En Route", course, speed and altitude.
6. **Packets tab**: new packets arrive at the top. Each one has:
   - its tags;
   - a second line saying how it travelled (heard direct, via Q2DIGI,
     igated by Q3GATE…);
   - the raw packet under "raw".

   Every ten minutes a malformed packet shows in red. Pause and Resume work.
7. **Messages tab**: a message from Q5MOB-9 (RF+IP) within about 20 s, one
   from Q8FAR (IP only) after about 95 s, and a bulletin from Q1HOME. The
   unread count clears when you open the tab. Sending is greyed out until A3.
8. **aprs.fi lookup**: type `Q9REMOTE` and press aprs.fi. The map moves to
   it, with a dashed ring, faded because it is two hours old, and "Data from
   aprs.fi" appears. `NOBODY` gives "aprs.fi has no position". With your
   own key, look up a real callsign.
9. **Quit** with Ctrl-C. The page shows "reconnecting…". Start the program
   again and the page carries on by itself.

## Send back

- Anything that looked wrong or confusing, especially the source tags and
  the "how it travelled" lines. Those must read right before real packets
  arrive in A2.
- Screenshots of anything odd.
- Whether the default demo centre should change (`--lat`/`--lon`), or
  whether your position should live in a config file from A2.
