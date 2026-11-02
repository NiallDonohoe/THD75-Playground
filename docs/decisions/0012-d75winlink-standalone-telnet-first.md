# 0012 — d75winlink: a standalone program, Telnet first, RF transmit as an option

Date: 2026-11-02 · Status: accepted

The owner asked to compose and send Winlink email from our own page. The
brief (SPEC.md) keeps d75scope receive-only, and Winlink over RF transmits,
so it is a fourth program, `d75winlink`, with its own spec
([../WINLINK.md](../WINLINK.md)) and milestones W0–W4. d75scope's
constraints and the CAT allow-list are unchanged: `TN` and `AS` stay
blocked, and d75winlink sends no CAT at all.

The owner chose, from the options offered:

- **A standalone program**, over a tab in d75aprs. Both need the TNC in
  KISS mode on the USB port, so they could not run at once anyway without a
  KISS multiplexer, and keeping them apart keeps each one's transmit rules
  simple. A multiplexer is listed as later work.
- **Telnet before RF.** The first live milestone (W2) reaches the Winlink
  CMS over the internet. The mailbox, compose page and B2F protocol are
  proven end to end with no radio and no transmitting, and RF follows once
  `kiss` and `ax25` exist.
- **RF transmit as an option, off by default**, on the d75aprs pattern:
  nothing is written to the serial port while it is off, and with it on,
  hard limits on session length, size, path and AX.25 parameters. Only the
  owner starts a connection; the program never answers one.

Winlink differs from APRS in what it transmits. An APRS frame is one short
burst; a Winlink session holds the channel for minutes and transmits as
fast as the gateway acks. The limits are therefore per session (20 minutes,
30 kB) rather than per frame, and Stop always ends transmitting.

Consequences: new radio questions W01–W07 in HARDWARE_QUESTIONS.md,
answered by the W3 and W4 checklists. `kiss` is shared with d75aprs's A2.
HF modes are out of reach, because the radio cannot transmit on HF, so the
RF route is VHF/UHF packet only, and its use depends on a packet RMS gateway
being in range.
