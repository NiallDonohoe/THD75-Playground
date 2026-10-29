# 0004 — d75aprs: a standalone program, KISS over USB, optional RF transmit

Date: 2026-10-29 · Status: accepted

The owner asked for an APRS client: messaging, a map of stations and aprs.fi
lookups, with every packet tagged RF or IP. The brief (SPEC.md) makes
d75scope receive-only and lists APRS and KISS as non-goals, so the client is
a separate program, `d75aprs`, with its own spec ([../APRS.md](../APRS.md)).
d75scope's constraints and the CAT allow-list are unchanged: `TN` and `AS`
stay blocked, and d75aprs sends no CAT at all.

The owner chose:

- **KISS over USB** (Menu 983 = USB), entered by hand on the radio. CAT stops
  answering in KISS mode, so the two programs cannot run at the same time.
  KISS over Bluetooth SPP would have avoided that; the owner preferred the
  wired port.
- **RF transmit as an option**, off by default. While it is off, nothing is
  written to the serial port. With it on, only messages, acks, rejects and
  beacons the program originates may go out, under hard limits on path,
  rate, retries and beacon interval. The transmit code lives where d75scope
  and d75probe never import it, and a test checks their dependency lists.
- **The same SSID as the radio.** This relies on KISS mode switching off the
  radio's own APRS, so the radio does not ack messages too (question K02).
- **Beacons on both RF and IP**, with an identical information field, so
  APRS-IS drops the copy an IGate forwards.

Consequences: the radio setup differs between the two programs, and the
README says how to switch. New radio questions K01–K08 go into
HARDWARE_QUESTIONS.md and are answered by the A1 and A4 checklists. Nothing
in d75aprs depends on an answer before its checklist has confirmed it.
