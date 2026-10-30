# d75sat page protocol

Version 1. The server is `satweb`, the page is `web/sat/app.js`.

## Access

As d75aprs ([APRS_PROTOCOL.md](APRS_PROTOCOL.md)); both use `localweb`:

- The server listens on loopback only.
- Every request needs a loopback `Host`, and a same-origin
  `Sec-Fetch-Site` and `Origin` when they are sent.
- Responses carry `Content-Security-Policy: default-src 'self'`.
- Satellite names and transmitter descriptions come from the network, and
  the page only ever puts them into the DOM as text.

## Routes

| Route | Purpose |
| --- | --- |
| `GET /` and `/{file}` | The page: `index.html`, `style.css`, `*.js`, and the shared map under `map/`. |
| `GET /events` | Server-sent events (below). |
| `GET /api/track?id=N` | Ground track of satellite N: `{now, past, future}`, `[lat, lon]` every 30 s for one orbit each way (at most 4 h). 404 for an unknown or unpredicted satellite. |
| `GET /api/pass?id=N&at=T` | The pass of N under way at T (RFC 3339, default now) or the next after it: `{pass, points}`, points `{t, az, el, rr}` every 10 s from AOS to LOS. 409 with no location or no pass in the next day. |
| `POST /api/location` | `{lat, lon, altM, source}`, source `browser`, `map`, `locator`, `flags` or `demo`. Saved in the config. 400 for a position off the Earth or unknown fields. Answers with the config. |
| `POST /api/alert` | `{id, on}`: the AOS alert for one satellite. |
| `POST /api/alert-timing` | `{minutes, minEl}`: 1–60 minutes before AOS, for passes peaking at 0–90°. |
| `GET /tiles/{z}/{x}/{y}` | Map tiles, as d75aprs. Absent with `--no-tiles`. |

POST bodies are JSON (`Content-Type: application/json`), at most 1 kB.

## Events

`GET /events` is a `text/event-stream` of compact JSON, as d75aprs's: a
fresh `hello` on every connection, a page that falls behind is cut off and
reconnects, and a `: keepalive` comment every 15 s.

| Event | Data |
| --- | --- |
| `hello` | `{info, snapshot}`, first on every connection. |
| `tick` | `{now, live}` every second of real time. `now` is the program's clock, which runs fast in demo mode; the page counts down from it, never from the browser's clock. |
| `passes` | `{computed, passes}` when they are recomputed: after a location or catalogue change, and every 10 minutes of clock time. |
| `catalog` | `{sats, status}` when the data is refreshed. |
| `config` | The whole config, after any change. |
| `status` | `{celestrak, satnogs}`, each `{fetched, error, off}`. |

`info`: `protocol`, `version`, `demo`, `speed` (demo clock rate), `tiles`,
`network`.

`snapshot`: `now`, `config`, `sats`, `passes`, `status`, `live`.

`config`: `location` (`{lat, lon, altM, source, set}`, absent until set),
`alerts` (`{id: true}`), `alertMinutes`, `alertMinEl`.

A satellite:

```json
{
  "id": "99903", "name": "DEMOSAT-3", "epoch": "2026-10-30T12:19:00Z",
  "predicted": true, "periodMin": 109.2,
  "satnogs": "https://db.satnogs.org/satellite/…",
  "transmitters": [
    {"id": "…", "description": "Linear transponder, inverting",
     "downlink": 435610000, "downlinkTop": 435670000,
     "uplink": 145935000, "uplinkTop": 145995000,
     "mode": "SSB", "radioMode": "USB", "invert": true, "kind": "Transponder"}
  ]
}
```

`predicted` is false, with a `problem`, when SGP4 cannot use the elements
(a deep-space orbit, say). `radioMode` is what band B will be set to:
`FM`, `USB`, `LSB` or `CW`.

A pass: `{id, name, aos, tca, los, aosAz, losAz, maxEl, inProgress}`, all
the passes from now for 24 hours, in AOS order.

A live entry: `{id, lat, lon, alt, az, el, rr, range}`. Height and range
are in km and range rate in km/s (positive receding). `az`, `el`, `rr` and
`range` are present once the station has a location.

The page works out Doppler itself from `rr`: f × (1 − rr / c). It draws
footprints from `alt`, with the same formula as `orbit.FootprintRadius`.
