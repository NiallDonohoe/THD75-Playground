package winlink

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// The Winlink CMS over Telnet. The address, the shared password and the
// target name are as Pat uses them (P02 in docs/WINLINK.md).
const (
	CMSAddress  = "server.winlink.org:8772"
	CMSTarget   = "wl2k"
	cmsPassword = "CMSTelnet" // the same for every user; the real login is the secure login
)

// DialCMS connects to the CMS at addr and answers its login prompts. The
// session runs over the connection it returns. log, if set, sees the
// login lines.
func DialCMS(ctx context.Context, addr, call string, log func(sent bool, line string)) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	conn.SetDeadline(deadline)
	if err := Login(conn, call, log); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

// Login answers the CMS's "Callsign" and "Password" prompts on rw. It
// reads a byte at a time, so nothing after the prompts is taken from the
// session.
func Login(rw io.ReadWriter, call string, log func(sent bool, line string)) error {
	if log == nil {
		log = func(bool, string) {}
	}
	send := func(s string) error {
		log(true, s)
		_, err := io.WriteString(rw, s+"\r")
		return err
	}
	var b []byte
	one := make([]byte, 1)
	for lines := 0; lines < 50; {
		if _, err := io.ReadFull(rw, one); err != nil {
			return fmt.Errorf("Telnet login: %w", err)
		}
		if one[0] != '\r' && one[0] != '\n' {
			if len(b) == maxLine {
				return fmt.Errorf("%w: Telnet login line over %d bytes", ErrProtocol, maxLine)
			}
			b = append(b, one[0])
			continue
		}
		line := strings.TrimSpace(string(b))
		b = b[:0]
		if line == "" {
			continue
		}
		lines++
		log(false, line)
		switch l := strings.ToLower(line); {
		case strings.HasPrefix(l, "***"):
			return &RemoteError{Text: strings.TrimLeft(line, "* ")}
		case strings.HasPrefix(l, "callsign"):
			if err := send(call); err != nil {
				return err
			}
		case strings.HasPrefix(l, "password"):
			return send(cmsPassword)
		}
	}
	return fmt.Errorf("%w: no Telnet login prompts", ErrProtocol)
}
