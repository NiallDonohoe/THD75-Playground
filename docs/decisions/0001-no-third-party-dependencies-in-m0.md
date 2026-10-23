# 0001 — No third-party dependencies in M0

Date: 2026-10-23 · Status: accepted

The spec allows `go.bug.st/serial` or a termios wrapper over
`golang.org/x/sys/unix`, and malgo or Core Audio directly through cgo. M0
uses neither library. The standard `syscall` package on darwin and linux
already exposes `Termios`, the get/set ioctls, `TIOCEXCL` and `Select`, so
the serial port is about 150 lines (radio/serial_unix.go). Core Audio's C API
is called directly from the audio package (audio/coreaudio.c), which keeps
cgo in one package as the spec requires and avoids miniaudio's extra layer
between us and device selection by UID. The module therefore has no
`go.sum`, and nothing needed the owner's approval.

Consequences: Linux serial support compiles but is untested (as the spec
allows for v1). macOS pseudo-terminals accept `TIOCEXCL` without enforcing
it, so the exclusive lock is only proven on the real port; the probe checks
it there (`serial.exclusive_lock` in the report).

Expected later, each with its own record and the owner's approval first:
`github.com/coder/websocket` (M1), esbuild and Playwright (M1, dev only).
