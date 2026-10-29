# 0006 — Our own APRS parser

Date: 2026-10-29 · Status: accepted

The `aprs` package parses and formats packets itself rather than using a
library. There is no standard Go APRS library to depend on, and 0001 keeps
the module to the standard library. The source tags also need full control
over the path, the APRS-IS q construct and third-party headers, which a
library would hide or normalise. Everything it parses arrives from the air
or the internet, so it is fuzzed as well as unit-tested.

Scope:

- **Text and frames**: TNC2 text; AX.25 UI frames as KISS carries them.
- **Decoded**:
  - positions: plain, with ambiguity; compressed; Mic-E
  - objects, items and status
  - messages, acks and rejects, including the reply-ack form
  - third-party packets
- **Kept raw**: weather, telemetry, queries and anything else is classed as
  unknown, with its data type, so the page can still show it.
- **Encoded**: messages, acks, rejects and plain position reports, which is
  all the program sends.

References: the APRS Protocol Reference 1.0.1 (APRS101) and its reply-ack
addendum. The test vectors are APRS101's examples, plus Mic-E packets worked
out by hand from APRS101's tables, with the working shown in the tests.
