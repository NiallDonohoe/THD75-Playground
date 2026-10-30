# 0008 — Our own FT8 decoder

Date: 2026-10-29 · Status: accepted

The `ft8` package encodes and decodes FT8 itself, in pure Go, rather than
wrapping ft8_lib (C, through cgo) or running WSJT-X's `jt9` on saved WAVs.
The owner chose this over both: 0001 keeps the module to the standard
library, `audio` stays the only cgo package, and decoding stays testable
and fuzzable here. As with 0006, everything it parses comes off the air.

It is written from the published protocol (Franke, Somerville and Taylor,
"The FT4 and FT8 Communication Protocols", QEX 2020), not translated from
WSJT-X (GPL) or ft8_lib. The exception is the two LDPC tables (the
generator and the sparse parity checks): they are protocol constants, a
decoder that differs by one bit cannot talk to anyone, and typing them in
by hand would invite exactly that. They are generated from ft8_lib's
`ft8/constants.c` (MIT licence, Kārlis Goba), whose notice is kept in
`ft8/LICENSE.ft8_lib`. A test checks each table against the other.

Testing against real signals uses recordings with WSJT-X's decodes
alongside. During development these were ft8_lib's seven test recordings
(about 360 kB each), kept outside the repository: whether to commit some
as fixtures, with their notice, is the owner's call. The owner's own
recordings from F2 will replace or join them.

Trade-off: WSJT-X decodes a few dB deeper, through ordered-statistics and
a-priori decoding. On the ft8_lib recordings this decoder finds 89% of
WSJT-X's decodes; nearly all the misses are below −13 dB. OSD can be added
later, once its false-decode rate has been measured.
