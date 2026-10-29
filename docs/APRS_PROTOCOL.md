# d75aprs page protocol

Version 1. The server is `aprsweb`, the page is `web/aprs/app.js`.

## Access

- The server listens on loopback only and refuses to start on any other
  address.
- Every request must have:
  - a loopback `Host` (`localhost`, `127.0.0.1` or `::1`, any port);
  - if `Sec-Fetch-Site` is sent, `same-origin` or `none`;
  - if `Origin` is sent, `http://` plus the Host.
- Responses carry `Content-Security-Policy: default-src 'self'`. The page
  runs only its own scripts and puts packet text into the DOM as text.

## Routes

| Route | Purpose |
| --- | --- |
| `GET /` and `/{file}` | The page: `index.html`, `style.css`, `*.js`. |
| `GET /events` | Server-sent events (below). |
| `POST /api/lookup` | aprs.fi lookup, on request (below). |
| `GET /tiles/{z}/{x}/{y}` | A map tile from the cache, fetched upstream when missing or stale. 400 for coordinates outside zoom 0–19, 502 when no copy can be had. Absent with `--no-tiles`. |

## Events

`GET /events` is a `text/event-stream`. Each event is one line of compact
JSON. The browser reconnects by itself, and every connection starts with a
fresh `hello`, so a page that falls behind is cut off and starts again from
a snapshot, never waited for. A `: keepalive` comment is sent every 15 s.

| Event | Data |
| --- | --- |
| `hello` | `{info, snapshot}`, first on every connection. |
| `packet` | A packet record, new or updated (a second source or a duplicate). |
| `station` | A station record, whole. |
| `message` | A message record, new or updated. |
| `status` | `{source: {state, detail}}` for one source. |

`info`: `protocol`, `version`, `myCall`, `lat`, `lon`, `demo`, `rfTransmit`,
`tiles`, `aprsfi`.

`snapshot`: `stations` (all), `packets` (the latest 500, oldest first),
`messages`, `status` (keyed `rf`, `is`, `aprsfi`).

Sources are `"rf"`, `"is"` (APRS-IS) and `"aprsfi"`. A list of sources is in
arrival order. A packet heard on both RF and APRS-IS within 30 s is one
record with both sources.

Packet record:

```json
{
  "id": 7, "at": "2026-10-29T15:00:02Z", "sources": ["rf", "is"],
  "raw": "Q1HOME>APK005,WIDE1-1:=5321.00N/00616.00W-Home",
  "src": "Q1HOME", "station": "Q1HOME", "kind": "position",
  "summary": "Position 53.3500, -6.2667",
  "rf": {"direct": true, "digis": ["Q2DIGI"], "gateway": "Q3GATE"},
  "is": {"q": "qAR", "via": "Q3GATE", "fromRF": true},
  "dupes": 1
}
```

Fields:

- **`kind`**: `position`, `object`, `item`, `message`, `status`,
  `third-party`, `unknown` or `malformed`.
- **`raw`** is the packet as first received. For a third-party packet, `src`
  and `station` belong to the packet inside it, and `rf.gateway` names the
  station that relayed it.
- **`station`** is a station key (below).
- **`rf`** and **`is`** are present only for the sources that delivered the
  packet. `digis` and `gateway` are left out when empty.

Station record:

```json
{
  "key": "Q5MOB-9", "name": "Q5MOB-9", "kind": "station", "owner": "",
  "symbol": "/>", "comment": "", "status": "", "micE": "En Route", "killed": false,
  "pos": {"lat": 53.37, "lon": -6.25, "ambiguity": 0, "hasCourse": true, "course": 90,
          "speed": 30, "hasAlt": true, "alt": 45, "at": "…", "sources": ["rf", "is"]},
  "track": [[53.37, -6.26], [53.37, -6.25]],
  "lastHeard": "…", "lastRF": "…", "lastIS": "…", "lastAPRSFI": "…", "packets": 4
}
```

Fields:

- **`key`**: a station's key is its callsign in upper case. An object's or
  item's key is `;` plus its name, and `owner` is the station that sent it.
- **`pos`**: `speed` is in knots and `alt` in metres. `pos.sources` says
  where the latest position came from.
- **`lastRF`, `lastIS`, `lastAPRSFI`**: omitted when never.
- **`track`**: holds up to 100 points.

Message record (to our callsign, or a bulletin):

```json
{"id": 12, "at": "…", "sources": ["rf", "is"], "from": "Q5MOB-9", "to": "EI2ABC-7",
 "kind": "message", "text": "Hello", "msgId": "3", "bulletin": false}
```

Fields:

- **`kind`**: `message`, `ack` or `rej`.
- **`id`**: the id of the packet that carried the message.

## Lookup

`POST /api/lookup` with `Content-Type: application/json` and
`{"call": "Q9REMOTE"}` (1–9 letters, digits or hyphens). The results go into
the store, so they also arrive as `station` events.

| Status | Body |
| --- | --- |
| 200 | `{"keys": ["Q9REMOTE"]}`: the station keys found; empty if aprs.fi knows none |
| 400 | `{"error": …}`: a bad callsign or body |
| 415 | not JSON |
| 429 | waiting after recent aprs.fi failures |
| 502 | aprs.fi failed (the message never contains the API key) |
| 503 | lookups are off |
