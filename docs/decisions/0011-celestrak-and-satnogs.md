# 0011 — Orbits from CelesTrak, frequencies from SatNOGS

Date: 2026-10-30 · Status: accepted

d75sat needs fresh orbits and a list of who transmits where. The owner
chose CelesTrak and the SatNOGS database, cached on disk, over:

- a short list of frequencies maintained in the repository;
- local files only, with no network.

- **CelesTrak**: the amateur group as CCSDS OMM in JSON
  (`gp.php?GROUP=amateur&FORMAT=json`). OMM rather than TLE, because
  catalogue numbers have outgrown the TLE's five digits.
  - It is fetched at most once every 2 hours, per CelesTrak's usage
    guidance, with a descriptive User-Agent.
  - It backs off after errors, because CelesTrak blocks clients that keep
    retrying.
  - Not reachable from the development machine on 30 Oct 2026, so the JSON
    format is checked against CelesTrak's documentation and confirmed at
    S1.
- **SatNOGS database**: `db.satnogs.org/api/transmitters/` (JSON), once a
  day.
  - Its fields (downlink and uplink frequencies, mode, `alive`, `status`,
    `invert`, `baud`, `norad_cat_id`) were checked against a live response
    on 30 Oct 2026.
  - The data is CC BY-SA 4.0, so the page credits SatNOGS and links to the
    satellite's entry.
- Neither request carries the station's position. Both can be turned off,
  and everything runs from the cache. `--demo` uses a bundled snapshot and
  makes no requests.

Consequences: d75sat makes outbound connections, as d75aprs does. They
are listed in the spec, each can be switched off, and the clients are
tested against fake servers, so `go test ./...` needs no network.
