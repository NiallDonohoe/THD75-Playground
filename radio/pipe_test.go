package radio

import (
	"io"
	"net"
)

type pipeEnd struct{ net.Conn }

var _ io.ReadWriteCloser = (*pipeEnd)(nil)

func newPipePair() (*pipeEnd, *pipeEnd) {
	a, b := net.Pipe()
	return &pipeEnd{a}, &pipeEnd{b}
}
