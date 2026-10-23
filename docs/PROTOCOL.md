# WebSocket protocol

Not written yet: the protocol is designed and documented in M1. The spec's
outline (docs/SPEC.md, "Wire protocol") is the starting point: JSON text
frames for hello, state, meter and console traffic; binary frames for
spectrum rows with a little-endian header and one byte per bin at 0.5 dB
per step.
