# Changelog

## W1 — d75winlink page, demo mode (2026-11-02, waiting for the owner's check)

- `d75winlink --demo`: a page on localhost with Inbox, Outbox, Sent and
  Archive; reading with attachments as downloads; compose and reply with
  size and airtime estimates, a check for characters Winlink cannot carry,
  and notes on internet replies and RF rules; drafts kept in the browser;
  Connect and Stop with a live session log and a progress bar per message.
  Runs only in demo mode until W2.
- `winlinkapp`: the mailbox on disk (one `.b2f` file per message, moved
  between folders by rename, read state in `read.json`), the session runner
  with a 2-minute idle limit and Stop, the session log on disk, compose
  limits (120 kB of attachments, 150 kB a message) and the event stream.
- `winlinkweb`: the page's server, wire format in docs/WINLINK_PROTOCOL.md.
- `winlinkdemo`: the fake CMS with three messages waiting, a reply to
  everything sent (on the next connection) and a link slowed to 600 bytes
  a second.
- `winlink`: progress reports per block, `ValidMID` exported, and
  `Mailbox.Received` takes the message ID from the proposal, so a message
  that will not parse is still stored under its ID. The fake CMS has an
  `OnReceive` hook.
- `localweb`: taken unchanged from the d75sat branch, so both branches
  share one guard.
- `web/winlink`: plain JavaScript; the helpers are tested with `node --test`.
- Decisions renumbered 0012–0013, as d75sat's branch already uses
  0009–0011.
- Makefile: builds d75winlink; `make winlink-demo`; `make test-web` runs the
  Winlink page's tests too.

## W0 — d75winlink core (2026-11-02)

- `lzhuf`: LZHUF compression both ways, with B2F's CRC-16/XMODEM and
  length header. Matches a reference implementation's output byte for byte
  (the Gettysburg fixture and four short vectors from wl2k-go). Writes
  Huffman codes over 16 bits whole, where LZHUF.C cut them short. Refuses
  truncated data, bad CRCs and stated lengths over the caller's limit.
- `winlink`: messages (addresses, IDs, headers, ISO-8859-1 bodies split at
  1000 bytes, encoded-word subjects, attachments) both ways; B2F (SIDs, the
  secure login, proposals and their checksum, answers including offsets,
  SOH/STX/EOT blocks); a client session that offers at most 5 messages a
  turn, defers what it cannot take, rejects what it already has, holds
  back what would pass a per-session size cap, confirms sent messages only
  on the server's next line, and never logs the password or its hash;
  the CMS Telnet login and `DialCMS`.
- `winlink/winlinktest`: a fake CMS with the server side written
  separately from the client: secure login checking, queued mail, and
  misbehaviour on request (already have, defer, drop mid-transfer, garbage,
  login failure).
- Tests: Pat's secure login, proposal and answer vectors; whole sessions
  against the fake over TCP; fuzzing of the decompressor, message parser
  and block reader.
- Docs: protocol questions P01–P05 answered from Pat's source (the LZHUF
  window is 2048 bytes, not 4096 as the spec first said); a sent message
  moves to `sent` on the server's next line, as Pat does.

## S1 — d75sat page (2026-10-30, waiting for the owner's check)

- `d75sat` serves a page on localhost:8078:
  - a map with satellites, the selected one's ground track and footprint,
    footprints of satellites in view, and the station's visibility
    circle;
  - a next-pass card with a countdown;
  - a pass table for the next day, and the satellite list;
  - a selected-satellite panel with a sky plot of the pass and live
    Doppler-shifted frequencies;
  - AOS alerts (banner and Chrome notification);
  - the location from the Mac (Chrome geolocation), a map click or a
    locator, saved in the config.
- `--demo` runs five made-up satellites on a ×10 clock, with a high pass
  in the first two minutes. `--offline` runs from the cache.
- `satdata`: CelesTrak (OMM JSON, at most every 2 h) and SatNOGS (daily)
  with a disk cache, backoff after failures, refusal of non-JSON answers,
  and filtering to what band B can receive and demodulate.
- `satapp`, `satweb`: the engine (positions every second, passes, tracks,
  config) and its server. Wire format in docs/SAT_PROTOCOL.md.
- Shared with d75aprs, without changing how it behaves or looks:
  - its map is now a base map in `web/map` (`geo.js` moved there, and
    grid labels past ±180° fixed);
  - the localhost guard is now package `localweb`.
- Tests: satdata against fake servers, satapp on a fixed clock, satweb end
  to end, the demo catalogue, and Node tests for the footprint, antimeridian,
  locator and sky-plot maths. The page was checked in headless Chrome.

## S0 — d75sat orbit core (2026-10-30)

- `orbit`: element sets from TLEs (with checksums) and CelesTrak OMM JSON;
  SGP4, near-Earth branch, after Vallado et al. 2006 (deep-space sets are
  refused); GMST and TEME to Earth-fixed; geodetic conversions; azimuth,
  elevation, range and range rate; pass prediction (AOS, TCA, LOS, passes
  already in progress); Doppler; coverage footprints and ground tracks.
- Tests: Vallado's nine near-Earth verification cases (all 158 rows agree
  to 1 mm during development; 27 embedded), GMST, geodetic round trips,
  passes against a one-second scan, footprint edges, and fuzz tests of the
  TLE and OMM parsers.
- `d75sat passes ELEMENTS-FILE --lat --lon` lists passes, with
  Doppler-shifted frequencies when given `--freq`.
- Docs: d75sat spec (docs/SAT.md), decisions 0009–0011, questions S01–S05.

## F0 — d75ft8 decoder core (2026-10-30)

- `ft8`: FT8 in pure Go. Packs and unpacks 77-bit messages (standard calls
  with /R and /P, CQ with modifiers, grids, reports, RRR/RR73/73, hashed
  calls resolved from calls heard in full, non-standard calls, free text,
  telemetry, DXpedition; contest types shown by type). CRC-14, the
  (174,91) LDPC code with sum-product BP, Gray mapping, Costas sync, GFSK
  synthesis. Decoder: spectrogram Costas search, baseband fine sync, one-,
  two- and three-symbol soft metrics, three passes with subtraction of what
  was decoded. `To12k` converts 48 kHz (or any multiple of 12 kHz) audio.
- Tests: LDPC table cross-check, CRC, BP, pack/unpack round trips,
  synthetic slots (including overlapping signals and noise alone), and fuzz
  tests of both parsers; the fuzzer found and fixed four packing edge cases
  (3X and 3DA0 call forms, −00, a 12-bit hash collision). With ft8_lib's
  seven recordings: 71 of WSJT-X's 80 decodes, no false decodes in 60 slots
  of noise, about 70 ms a slot.
- `dsp`: a DFT of any size (Bluestein over the radix-2 FFT).
- `d75ft8 decode FILE.wav…` prints decodes as WSJT-X does; `d75ft8 encode`
  writes a WAV of a message. `make test-ft8-reference` runs the recording
  comparison.
- Docs: d75ft8 spec (docs/FT8.md), decisions 0007–0008, questions F01–F05.

## A1 — d75aprs page and map, demo mode (2026-10-29, waiting for the owner's check)

- `d75aprs --demo`: a page on localhost with a map of stations, a station
  list and detail, a packet log and messages. Every packet, station and
  message is tagged RF, IP, RF+IP or aprs.fi, with a second line saying how
  it travelled (direct, digipeaters, IGate, third-party gateway). Sending is
  not built yet (A3, A4).
- `aprsapp`: store of packets, stations and messages; merges a packet heard
  on RF and APRS-IS within 30 s into one record with both sources; event
  stream for the page that drops a slow page rather than waiting for it.
- `aprsfi`: on-request lookups with a 5-minute cache, exponential backoff, a
  descriptive User-Agent, and errors that never contain the API key.
- `tiles`: OpenStreetMap tile proxy with a disk cache that honours the
  server's cache headers, revalidates with ETags, serves stale tiles when the
  server is down, and runs at most two downloads at once.
- `aprsweb`: loopback-only server with Host, Sec-Fetch-Site and Origin checks
  and a strict Content-Security-Policy; server-sent events; lookups. Wire
  format in docs/APRS_PROTOCOL.md.
- `demo`: made-up RF (through AX.25) and APRS-IS traffic covering every tag
  case, and a fake aprs.fi used through the real client.
- `web/aprs`: plain JavaScript page with our own canvas map (Web Mercator,
  dark-mode tiles, tracks, ambiguity boxes, label collision), emoji APRS
  symbols. Map maths tested with `node --test`.
- `aprs`: Mic-E comments drop the leading radio-type byte ('>' or ']').
- Milestones A1 (receive) and A2 (page) swapped at the owner's request.
- Makefile: builds d75aprs; `make aprs-demo`; `make test` runs the Node
  tests when Node is installed.

## A0 — APRS packet core (2026-10-29)

- `aprs`: TNC2 text and AX.25 UI frames (both ways), the q construct and
  digipeaters used. Decodes plain positions (with ambiguity), compressed
  positions, Mic-E, objects, items, status, third-party packets, and
  messages, acks and rejects (including reply-acks). Weather, telemetry and
  other types come back as unknown, with the raw field kept. Encodes
  messages, acks, rejects and plain position reports; resolves APRS
  timestamps. Fuzz tests for the TNC2 and AX.25 parsers.
- Docs: d75aprs spec (docs/APRS.md), decisions 0004–0006, KISS questions
  K01–K08 in HARDWARE_QUESTIONS.md.

## M0 — probe CLI (2026-10-23, waiting for the hardware run)

- `d75probe`: read-only survey of the radio (audio devices and formats, USB
  identity, serial port, ID FV FO FQ MD IO FT FS AI SH BY) and an
  as-found recording; `--allow-writes` for the full IF survey with snapshot
  and verified restore. Writes `probe-report.json`, WAV files and
  `summary.txt`. `--mock` runs it against a fake TH-D75 on a
  pseudo-terminal and a synthetic IF. `--list` lists devices only.
- `radio`: CAT codec, allow-list with hard blocks, session goroutine with
  prefix routing, 500 ms timeout and one retry, NMEA/APRS/KISS detection
  that holds writes, termios serial with TIOCEXCL, USB discovery from the
  I/O Registry, atomic snapshot and restore with the owner-changed rule,
  and the fake radio.
- `dsp`: WAV I/O, FFT, Blackman–Harris window, spectrum averaging, peak
  interpolation, level statistics, Kaiser FIR design, synthetic IF.
- `audio`: Core Audio listing and capture by device UID (cgo).
- Docs: PLAN, SPEC, HARDWARE_QUESTIONS, CAT, M0 checklist, decisions
  0001–0003, probe report schema.
