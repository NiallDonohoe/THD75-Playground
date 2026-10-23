# 0002 — Pull-based IF source

Date: 2026-10-23 · Status: accepted

The spec suggests an IFSource with "start, stop, sample rate, block
callback". `ifsource.Source` is pull-based instead: it starts on Open,
`Read` blocks until a buffer of interleaved frames is full, and `Close`
stops it. The concurrency model already has one DSP goroutine pulling
fixed-size blocks, so a pull API matches it directly, and it lets the Core
Audio input callback stay entirely in C: the callback only copies into a
single-producer, single-consumer ring (C11 atomics, no allocation, no locks,
no calls into Go), and the Go reader drains it. Synthetic and WAV sources
implement the same interface without threads. Optional interfaces add
`Flush` (discard buffered audio after a retune) and `Overruns`.
