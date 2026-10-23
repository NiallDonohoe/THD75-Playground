//go:build !(darwin && cgo)

package audio

import "d75scope/ifsource"

// CoreAudio is unavailable in this build.
type CoreAudio struct{}

// New returns the stub provider.
func New() *CoreAudio { return &CoreAudio{} }

func (*CoreAudio) Devices() ([]ifsource.Device, error)  { return nil, ErrUnavailable }
func (*CoreAudio) Open(string) (ifsource.Source, error) { return nil, ErrUnavailable }
