# d75winlink — Winlink email client

Living spec for the fourth program in this module, agreed with the owner on
2 Nov 2026. The decisions behind it are
[0009](decisions/0009-d75winlink-standalone-telnet-first.md) and
[0010](decisions/0010-own-winlink-stack.md). The radio facts it depends on
are the W questions in [../HARDWARE_QUESTIONS.md](../HARDWARE_QUESTIONS.md).

## What it is

`d75winlink` sends and receives Winlink email from a page on the Mac:

- compose a message (To, Cc, Subject, body, small attachments) to Winlink
  addresses and to ordinary internet email addresses;
- an outbox of messages waiting to go, an inbox, sent items and an archive;
- a connect button that runs one session with a Winlink server, sending the
  outbox and fetching new mail, with a live log of the session.

There are two ways to reach Winlink:

| Route | How | Milestone |
| --- | --- | --- |
| **Telnet** | TCP to the Winlink CMS over the internet | W2 |
| **RF** | AX.25 connected mode through the TH-D75's KISS TNC, to a VHF/UHF packet RMS gateway | W3, W4 |

Both routes speak the same B2F protocol and share the mailbox, so a message
composed once can go either way.

It is a separate program from d75scope, d75aprs and d75ft8, and never runs
alongside them. On RF it needs the TNC in KISS mode on the USB serial port,
as d75aprs does, so CAT is silent and no other program can use the port.

## Rules

Transmitting on RF (W4):

- **RF transmit is off by default.** It is turned on with `--rf-transmit`,
  and the page shows whether it is on at all times. While it is off, the
  program writes nothing at all to the serial port.
- **Only the owner starts a connection**, by pressing Connect for a gateway
  callsign the owner has typed or chosen. There are no scheduled or
  automatic connections, and the program never answers a connection
  another station starts (peer-to-peer listening is later, and off by
  default when it comes).
- **The source is always the configured callsign.** There is no
  digipeating, no relaying and no KISS parameter or exit commands: only KISS
  data frames on port 0, as in d75aprs.
- The RF limits below are hard ceilings in code; the config can only lower
  them.
  - Path: at most two digipeaters.
  - Session: at most 20 minutes from Connect to disconnect, after which the
    program sends DISC and stops.
  - AX.25: window (k) at most 4, frame length (paclen) at most 128 bytes,
    retries (N2) at most 10. T1 is computed from the path length and
    baud rate, never below 3 s.
  - Outgoing size: at most 30 kB compressed per session over RF (about
    seven minutes of airtime at 1200 bps). Larger messages wait for
    Telnet or are split by the owner.
  - Abort: a Stop button sends DISC once, then stops transmitting whether
    or not the gateway answers.
- d75scope, d75probe, d75aprs and d75ft8 must never import the Winlink
  transmit code. A test checks their dependency lists.

Content:

- Messages go as they are. B2F compresses them (LZHUF), which is not
  encryption, and the program offers no way to encrypt or obscure content.
- The page shows a reminder that amateur radio rules apply to everything
  sent over RF: no business, no music, no obscenity, and nothing obscured.

Network and privacy:

- The HTTP server listens on localhost only and checks Host, Origin and
  Sec-Fetch-Site, as d75aprs does.
- The only outbound connection is to the Winlink CMS, and only when the
  owner presses Connect on the Telnet route. Nothing is polled.
- The Winlink password comes from the environment (`D75WINLINK_PASSWORD`)
  or the config file, and never appears in logs, reports, the session log
  or the page. It is never sent over the wire either: the secure login
  sends a hash of it.
- `go test ./...` passes with no radio and no network.

## Radio setup (W3, W4)

| Menu | Setting | Why |
| --- | --- | --- |
| 980 | COM+AF/IF Output | Serial port on USB |
| 983 | USB | KISS frames on the USB serial port |
| 506 (Data Band) | The band tuned to the gateway's frequency | The TNC sends and receives on the data band |
| TNC | KISS mode, 1200 or 9600 bps to match the gateway, entered by hand | The program never sends `TN` (K01, W01) |
| APRS | Off, or beaconing on another band | The radio's own APRS must not transmit into a session (W05) |

To go back to d75scope: leave KISS mode and set Menu 983 to Bluetooth.

## Mailbox

- One file per message, in its B2F form (the headers, body and attachments
  exactly as sent or received), named by message ID: `MID.b2f`.
- Folders `in`, `out`, `sent` and `archive` under
  `~/Library/Application Support/d75winlink/mailbox/CALL/`. Drafts are kept
  in the page until the owner queues them.
- A message moves from `out` to `sent` once the server has accepted it,
  its blocks have gone, and the server's next protocol line has come back
  (not an error), as Pat does. A session that drops before then leaves it
  in `out`, to be offered again; if the server had it after all, it answers
  "already have" and the message moves to `sent` then. A message the server
  defers stays in `out`.

## Messages

- **Addresses**: a callsign (`EI2ABC`), a Winlink address
  (`EI2ABC@winlink.org`, stored as the callsign) or an internet address
  (stored as `SMTP:name@example.com`).
- **Headers**: `Mid`, `Date` (UTC), `Type: Private`, `From`, `To`, `Cc`,
  `Subject`, `Mbo` (our callsign), `Body` and one `File` per attachment.
- **Message ID**: 12 characters of upper-case letters and digits, random.
- **Body**: plain text in ISO-8859-1 (what Winlink delivers to internet
  mail), lines ending CRLF and split at 1000 bytes. Characters outside
  ISO-8859-1 are refused on the page. Received bodies are read in the
  charset their `Content-Type` names, ISO-8859-1 when it names none.
- **Subject and attachment names**: RFC 2047 encoded words when not ASCII.
- **Attachments**: added on the page, at most 120 kB in all per message
  (Winlink's own limit may be lower; P06). The page shows each message's
  compressed size and its estimated airtime at 1200 and 9600 bps.
- **Internet replies**: Winlink drops internet mail from senders the
  account has not whitelisted. The page says so when a message goes to an
  `SMTP:` address.

## Sessions

A session is one connection, run by the `winlink` package over any
byte stream (TCP or an AX.25 link):

1. **Login.** On Telnet: the CMS prompts for the callsign and a password,
   and the client answers with the callsign and `CMSTelnet`. Over RF there
   is no prompt; the gateway's greeting follows the connect.
2. **Handshake.** The server sends its SID (`[WL2K-…-B2FWIHJM$]`), a
   secure login challenge (`;PQ: …`) and its prompt. We send `;FW:` with our
   callsign, our SID (`[d75winlink-VERSION-B2FHM$]`) and the challenge
   answer (`;PR: …`).
3. **Exchange.** The side whose turn it is proposes its messages (`FC`
   lines and an `F>` checksum), or sends `FF` if it has none. The other side
   answers each proposal with accept, reject or defer (`FS`), and the
   accepted messages follow as compressed blocks. Turns alternate until
   both sides have nothing to send.
4. **Quit.** `FQ`, then the connection closes.

The client takes the first turn. A turn offers at most 5 messages; the rest
wait for our next turn. Proposals we cannot take (another proposal type, a
message ID repeated in the batch, or over 1 MB) are deferred, so they stay
on the server. A proposal for a message ID already in the mailbox is
rejected. A `***` line from the server ends the session with its text.

Every line sent and received goes to the session log on the page (with the
challenge answer shown as `;PR: ********`), and to a session log on disk.

### Protocol questions

These were stated from memory when the spec was written, then checked in W0
against Pat's source (wl2k-go, MIT, read as a reference; no code copied).
Each is confirmed on the real CMS in W2.

| ID | Question | Answer (W0) | Status |
| --- | --- | --- | --- |
| P01 | The secure login | MD5 of challenge, password and a fixed 64-byte salt (from paclink-unix); bytes 0–3 little-endian with the top two bits cleared, as 8 decimal digits. Pat's two vectors pass. | answered; W2 confirms |
| P02 | The CMS Telnet login | `server.winlink.org:8772`; prompts start "Callsign" and "Password", each ended by CR; the password is `CMSTelnet` for everyone; the target is `wl2k`. TLS not checked. | answered; W2 confirms, and checks for TLS |
| P03 | The B2 compressed payload | CRC-16/XMODEM (polynomial 0x1021, initial 0) of the length and the coded bits, little-endian; then the length, 4 bytes little-endian; then LZHUF with a **2048**-byte window and 60-byte lookahead (not 4096). Our encoder matches a reference output byte for byte. | answered |
| P04 | Proposal checksum and blocks | `F> XX`: the negated sum of every proposal byte, CR included, in hex. SOH, header length, title (RFC 2047 if not ASCII, at most 80 bytes), NUL, offset, NUL; STX, length (0 means 256), data; EOT and the negated sum of the data. Pat sends 125-byte blocks, paclink-unix 250; we send 250. | answered |
| P05 | SID flags | We send `B2FHM$`, as Pat does. The client needs `B2F` in the server's SID. Answers `+-=` and the letter forms `YNRLHA`, and `!` or `A` with an offset. | answered |
| P06 | Winlink's per-message size limit | Not in the references. | open, W2 |
| P07 | Setting up an account | Pat's tests show a welcome `;PM:` from SERVICE on a new account's first connection, which suggests connecting creates it. Unconfirmed. | open, W2 |

## AX.25 connected mode (W3)

The `ax25` package adds what APRS never needed:

- every frame type: SABM, UA, DISC, DM, FRMR, I, RR, RNR, REJ; modulo-8
  sequence numbers (no SABME);
- the data-link state machine (disconnected, awaiting connection,
  connected, timer recovery, awaiting release) with T1, T3 and N2;
- digipeater paths in the address field;
- a `Conn` that looks like a `net.Conn` to the `winlink` session, so the
  session code is the same for Telnet and RF.

The state machine runs on an injected clock and a frame channel, with no I/O
of its own, so it is tested on a fake clock against a second instance of
itself and against recorded frame sequences.

## Page

- **Compose**: To, Cc, Subject, body, attachments; size and airtime;
  Queue (to `out`) and Save draft.
- **Folders**: Inbox, Outbox, Sent, Archive; reading a message shows its
  headers and body, and lets the owner save its attachments.
- **Connect**: the route (Telnet or RF), and for RF the gateway callsign
  and optional digipeaters, with recently used gateways remembered. The
  page shows RF transmit on or off.
- **Session**: a live log, a progress bar per message, and Stop.
- The wire format between the page and the server goes in
  `WINLINK_PROTOCOL.md`, in the style of
  [APRS_PROTOCOL.md](APRS_PROTOCOL.md). The address is
  `http://127.0.0.1:8078/`.

## Packages

| Package | Role |
| --- | --- |
| `lzhuf` | LZHUF compression and decompression (LZSS over a 2048-byte window plus adaptive Huffman), as FBB and B2F use it, with B2F's CRC-16 header. Pure Go, no I/O. |
| `winlink` | Messages (parse and format, IDs, addresses), B2F (SIDs, secure login, proposals, blocks) and the session over an `io.ReadWriter`; the Telnet dialler. |
| `winlink/winlinktest` | A fake CMS that speaks the server side of B2F, for tests and `--demo`. Its protocol code is written separately from the client's, so a misreading shared by both would have to be made twice. |
| `winlinkapp` | The mailbox on disk, the session runner and the event stream. |
| `winlinkweb` | HTTP server: localhost guard, embedded page, server-sent events, compose and connect endpoints. |
| `web/winlink` | The page, in plain JavaScript like `web/aprs`. |
| `kiss` | (W3) KISS framing: a receive-only `Reader` and a `Transmitter` that exists only when RF transmit is on. Shared with d75aprs, whose A2 also needs it; whichever track reaches it first builds it, to this spec and APRS.md's. |
| `ax25` | (W3) AX.25 frames and connected mode. The `aprs` package keeps its own UI-frame code for now; moving it onto `ax25` is a later tidy-up. |
| `cmd/d75winlink` | The program. |

The serial port and USB discovery are reused from `radio`; CAT is not used.

## Testing without the radio or the network

- **`lzhuf`**: round trips of text and random data, fuzzing of the
  decompressor (it reads what comes off the air), and known compressed
  outputs from a reference implementation as fixtures.
- **`winlink`**: message round trips, the secure login against P01's test
  vector, proposal checksums, and whole sessions against the fake CMS over
  `net.Pipe`: sending, receiving, rejecting, deferring, a dropped
  connection, and a server that sends garbage.
- **`ax25`**: two state machines connected through a lossy fake channel on
  a fake clock: lost frames, lost acks, REJ recovery, retries running out,
  and disconnects from either side.
- **Fake TNC** (W3): a KISS peer on a pseudo-terminal with a fake gateway
  behind it. It records every byte written; with transmit off it must
  receive zero bytes.
- **`--demo`**: the page against the fake CMS in-process, with made-up mail
  from callsigns starting with Q, which no country issues.

## Milestones

Each milestone ends with green tests, a changelog entry and an owner check.
They do not wait for the other programs' milestones.

1. **W0 — Core.** `lzhuf`, and in `winlink` the message format, B2F and
   the session, against the fake CMS. P01–P05 answered from the references
   first. No owner check needed beyond reading the tests.
2. **W1 — Page, in demo mode.** Mailbox, compose, folders, session log,
   `--demo` against the fake CMS. Done when the owner has worked through
   checklist W1 in Chrome.
3. **W2 — Telnet.** Real sessions with the CMS using the owner's account.
   Done when the owner has sent a message to their own internet email,
   replied to it, and received the reply (checklist W2, confirming
   P01–P07).
4. **W3 — KISS and AX.25, receive only.** `kiss`, `ax25`, the fake TNC, and
   a monitor view of frames heard on the gateway's frequency. Nothing is
   transmitted. Done when the owner has watched another station's session
   with a gateway in the monitor (checklist W3, answering W01–W04).
5. **W4 — RF.** A session with a packet RMS gateway through the radio,
   under the RF limits. Done when the owner has sent and received a
   message over RF (checklist W4, answering W05–W07).

Later: peer-to-peer sessions with another station; a KISS multiplexer so
d75aprs and d75winlink can share the TNC; gateway lists from the Winlink
API; Winlink forms (ICS-213 and the like); position reports.

## Non-goals

- HF modes (VARA, ARDOP, PACTOR): the TH-D75 cannot transmit on HF.
- Acting as a gateway, or relaying other stations' mail.
- Encrypting or obscuring message content.
- Running alongside d75scope, d75aprs or d75ft8.
