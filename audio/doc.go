// Package audio is the only cgo package: it lists Core Audio devices and
// captures from one chosen by ID, never the system default input. Without
// cgo (or off macOS) it builds as a stub, so the rest of the module builds
// and tests with CGO_ENABLED=0.
package audio

import "errors"

// ErrUnavailable means this build has no audio support (no cgo, or not
// macOS).
var ErrUnavailable = errors.New("audio capture needs a macOS build with cgo")
