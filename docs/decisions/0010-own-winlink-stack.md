# 0010 — Our own Winlink stack: LZHUF, B2F and AX.25 connected mode

Date: 2026-11-02 · Status: accepted

The owner chose to build the Winlink client rather than drive Pat, the
open-source Go Winlink client, as a sidecar. Pat would have brought B2F and
LZHUF ready-made, but it was not clear that its RF transports support the
radio's KISS TNC over USB on macOS, and running it would put the transmit decisions
in another program's code and config, outside the limits and tests that
d75aprs set the pattern for. 0001 also keeps the module to the standard
library.

Scope:

- **`lzhuf`**: LZHUF as FBB uses it (a 4096-byte LZSS window with adaptive
  Huffman coding), both ways.
- **`winlink`**: the Winlink message format; B2F (SIDs, secure login,
  proposals, accept/reject/defer, block framing, checksums, resume); the
  session over any byte stream; the Telnet dialler.
- **`ax25`**: AX.25 2.0 connected mode, modulo 8, as a state machine with
  no I/O of its own, on an injected clock.

Not in scope: B1 (uncompressed FBB) and the older B protocol, AX.25 2.2's
SABME and selective reject, and serving as a gateway.

References: the Winlink B2F description published by the Winlink
Development Team, the FBB forwarding protocol notes, and AX.25 Link Access
Protocol 2.0 and 2.2. Pat's source is read as a second reference where the
written ones are thin (P01–P05 in WINLINK.md list where); no code is
copied from it. Everything the stack parses arrives from the air or the
internet, so the LZHUF decoder, the B2F line and block parser and the AX.25
frame decoder are fuzzed as well as unit-tested.

Test vectors: LZHUF outputs from a reference implementation, kept as
fixtures with their source noted; a secure-login vector (P01); B2F
exchanges recorded from the fake CMS and, in W2, the real one (with the
login answer removed).
