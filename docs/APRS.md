# d75aprs — APRS client

Living spec for the second program in this module, agreed with the owner on
29 Oct 2026. The decisions behind it are
[0004](decisions/0004-d75aprs-standalone-kiss-optional-transmit.md),
[0005](decisions/0005-d75aprs-browser-page-own-map.md) and
[0006](decisions/0006-own-aprs-parser.md). The radio facts it depends on are
the K questions in [../HARDWARE_QUESTIONS.md](../HARDWARE_QUESTIONS.md).

## What it is

`d75aprs` turns the TH-D75's built-in TNC into an APRS client on the Mac:

- a map of the stations heard on RF and on APRS-IS;
- two-way APRS messaging;
- position beacons on RF and on APRS-IS;
- aprs.fi lookups, on request, to fill in stations and messages.

Every packet, station and message on screen says where it came from: **RF**,
**IP** (APRS-IS) or **aprs.fi**.

It is a separate program from d75scope and never runs alongside it. In KISS
mode the radio's USB serial port carries KISS frames and CAT stops
answering. d75scope's rules (receive only, the CAT allow-list) are
unchanged, and still hold for d75scope and d75probe.

## Rules

Transmitting:

- **RF transmit is off by default.** It is turned on with `rf_transmit` in the
  config or `--rf-transmit`, and the page shows whether it is on at all times.
  While it is off, the program writes nothing at all to the serial port.
- With it on, the program transmits only what it originates: messages, acks
  and rejects, and its own position beacons. The source is always the
  configured callsign. There is no digipeating, no IGating (nothing is ever
  copied between RF and APRS-IS), no objects or items, and no KISS parameter
  or exit commands: only KISS data frames on port 0.
- The RF limits below are hard ceilings in code; the config can only lower
  them.
  - Path: at most two hops (default `WIDE1-1,WIDE2-1`).
  - Beacons: at least 10 minutes apart (default 30).
  - Message retries: at most 5, spaced 30, 60, 120, 240 and 480 s.
  - Frame rate: at most one frame every 5 s, and 20 in any 10 minutes.
- Sending over APRS-IS needs the callsign's passcode. Without one, the
  program logs in read-only (`pass -1`) and cannot send or beacon over IP.
- d75scope and d75probe must never import the RF transmit code. A test
  checks their dependency lists.

Network and privacy:

- The HTTP server listens on localhost only and checks the Origin header,
  as d75scope does.
- Outbound connections go to APRS-IS, aprs.fi and the map tile server, and
  each one can be turned off.
- The passcode and the aprs.fi key live in the config file and never appear
  in logs, reports or the page.
- `go test ./...` passes with no radio and no network.

## Radio setup

| Menu | Setting | Why |
| --- | --- | --- |
| 980 | COM+AF/IF Output | Serial port on USB |
| 983 | USB | KISS frames on the USB serial port |
| 506 (Data Band) | The band tuned to the local APRS frequency | The TNC sends and receives on the data band |
| TNC | KISS mode, 1200 bps, entered by hand | The program never sends `TN` (K01) |

To go back to d75scope: leave KISS mode and set Menu 983 to Bluetooth.
d75scope recognises KISS bytes on the port and says so.

## Where data comes from

| Tag | Source | Notes |
| --- | --- | --- |
| RF | KISS frames from the radio's TNC | Heard direct, or through the named digipeaters |
| IP | APRS-IS | Live stream, filtered to a range around our position |
| aprs.fi | The aprs.fi API | Only on request; always shown with its age |

The tag records how **this program** received the packet:

- **One packet heard both ways** (same source, destination and information
  field within 30 s) is kept as one packet, tagged **RF + IP**.
- **Packets that started somewhere else** carry a second line. On IP, the q
  construct says whether an IGate heard it on RF ("igated by EI7IG"). On RF,
  a third-party packet (`}`) means it came from APRS-IS through a gateway
  ("from IS via EI7IG").
- **A station** shows the source of its latest position, and separately when
  it was last heard on RF.
- **Our own packets coming back**, such as our RF beacon gated to APRS-IS, are
  shown as confirmation: "your beacon was igated by EI7IG".

## APRS-IS

- Server `rotate.aprs2.net:14580` by default, configurable.
- Login line: `user CALL pass PASSCODE vers d75aprs VERSION filter
  r/LAT/LON/KM g/CALL`.
- The passcode is computed from the callsign (the public algorithm) unless
  the config sets one.
- The server sends `#` comment lines as keepalives. With no data for
  2 minutes the program reconnects, backing off exponentially.
- Sends messages, acks and beacons with the path `TCPIP*`.

## aprs.fi

Only on request, as the [API terms](https://aprs.fi/page/api) require:

- clicking a station or searching for a callsign runs a `what=loc` lookup;
- opening the inbox runs a `what=msg` lookup for our callsign.

Further rules:

- Results are cached for 5 minutes. Nothing is polled or preloaded in the
  background.
- Wherever aprs.fi data appears, the page shows a visible link crediting
  aprs.fi.
- The owner's own API key goes in the config.
- Requests send the User-Agent `d75aprs/VERSION (+HOME PAGE)`, time out after
  5 s, and back off exponentially after failures.

## Messaging

- **Route**: each outgoing message goes over RF, IP or both. RF is offered
  only while RF transmit is on.
- **Format**: text of at most 67 printable characters, without `|`, `~` or
  `{`. Message numbers come from a counter saved on disk.
- **Retries**: on the schedule in the RF limits, until the message is acked
  or rejected, or the owner cancels it.
- **Incoming** messages to our callsign are acked on the route they came in
  on. When an RF message arrives with RF transmit off, the page says the
  sender will not get an RF ack.
- **Duplicates** (same sender and number within 30 minutes) are acked again
  but shown only once.
- **Reply-acks** are parsed but not sent in v1.
- **Threads**: one per correspondent, and each message carries its tag.
- **SSID**: the same as the radio's. In KISS mode the radio's own APRS should
  be off, so it will not ack messages as well (K02).

## Beacons

- The position comes from the config: latitude, longitude, symbol, comment
  and optional altitude. Fixed station only; GPS is later (K05).
- On RF (only with transmit on), every 30 minutes by default, 10 at least.
- On IP (only with a passcode), every 30 minutes by default, 5 at least.
- Both routes send the same information field, so APRS-IS drops the copy an
  IGate forwards.
- Destination (tocall) `APZD75`, from the experimental `APZ` range, until
  the owner registers one in the aprs-deviceid registry.
- A "beacon now" button, still subject to the RF limits.

## Map

- Our own canvas map ([0005](decisions/0005-d75aprs-browser-page-own-map.md)):
  Web Mercator, OpenStreetMap raster tiles, pan and zoom, station markers and
  labels.
- **Tiles** go through the Go server. It caches them on disk, honours their
  HTTP cache headers, sends a User-Agent and never prefetches beyond the
  view. The map shows "© OpenStreetMap contributors".
- **Without internet**, markers draw over whatever tiles are cached, on a
  latitude–longitude grid.
- **Alongside the map**: a station list that can be filtered by source.
- **Open: APRS symbols.** Use simple markers with the symbol code at first.
  An icon set needs its licence checked before it is embedded.

## Packages

| Package | Role |
| --- | --- |
| `aprs` | Packet core: TNC2 text, AX.25 frames; decoding positions (plain, compressed, Mic-E), objects, items, messages, status and third-party packets; encoding messages, acks and positions. Pure Go, no I/O. |
| `kiss` | KISS framing. A receive-only `Reader`, and a `Transmitter` that exists only when RF transmit is on and enforces the RF limits. |
| `aprsis` | APRS-IS client. |
| `aprsfi` | aprs.fi client with its cache. |
| `aprsapp` | Station store (deduplication, source tags, persistence), message engine, beacon scheduler. |
| `aprsweb` | HTTP server: embedded page, server-sent events, JSON POSTs, tile proxy. |
| `web/aprs` | The page, in plain JavaScript. |
| `cmd/d75aprs` | The program. |

The serial port and USB discovery are reused from `radio`; CAT is not used.

## Testing without the radio or the network

- **Fake TNC**: a KISS peer on a pseudo-terminal. It injects frames and
  records every byte written. With transmit off it must receive zero bytes.
- **Fake servers**: APRS-IS on a local TCP port, aprs.fi and the tile server
  through `httptest`.
- **Message and beacon timing** is tested on a fake clock: retries, acks and
  the RF limits.
- **Packet corpus**: packets in `testdata/aprs/`, plus fuzzing of everything
  that parses radio or network input.

## Milestones

Each milestone ends with green tests, a changelog entry and, where the radio
is involved, a hardware checklist. They do not wait for d75scope's
milestones.

1. **A0 — Packet core.** The `aprs` package. Done when its decode, encode and
   round-trip tests pass and fuzzing finds no panics.
2. **A1 — Receive.** KISS reader, APRS-IS client (read-only login), station
   store with tags and deduplication, and `d75aprs --listen`, which prints
   every packet with its tag. Done when the owner has run it in KISS mode
   (checklist A1, answering K01–K04, K06 and K07) and sees RF and IP
   packets tagged correctly.
3. **A2 — Page and map.** Server, events, map, station list and aprs.fi
   lookups, plus a `--demo` mode on the fakes. Done when the page works in
   Chrome in demo mode and the owner checks it against the live feeds.
4. **A3 — Messaging and beacons over IP.** Passcode, messages, acks, retries
   and IP beacons. Done when the owner exchanges messages with another
   station over IP and the beacon shows on aprs.fi.
5. **A4 — RF transmit.** The transmitter and its limits, RF messages and
   beacons, and message routes. Done when every limit has a test and the
   owner has completed an RF message round trip, seen a beacon igated, and
   answered K08.

## Non-goals

- IGate or digipeater functions.
- Sending objects or items, and Mic-E encoding.
- Decoding telemetry and weather (shown raw).
- GPS position and smart beaconing (later).
- D-STAR, and running alongside d75scope.
