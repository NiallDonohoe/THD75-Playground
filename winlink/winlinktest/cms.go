// Package winlinktest is a fake Winlink CMS: the Telnet login and the
// server side of B2F, for tests and d75winlink --demo.
//
// Its protocol code is written apart from package winlink's client, from
// the same description, so the two check each other. They share only
// package lzhuf (tested against a reference implementation's output) and
// the secure login hash (tested against Pat's vectors).
package winlinktest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"d75scope/lzhuf"
	"d75scope/winlink"
)

// Challenge is the fake's fixed ";PQ:" challenge.
const Challenge = "23753528"

// Mail is a message held by the fake: waiting for a station, or received
// from one.
type Mail struct {
	MID     string
	Subject string
	Raw     []byte // B2F form
	From    string // for received mail, the station that sent it
}

// CMS is the fake server. Set the fields before serving.
type CMS struct {
	Passwords map[string]string // callsign to Winlink password

	// Misbehaviour, for tests.
	Have            map[string]bool // MIDs answered '-', as if already received
	Defer           map[string]bool // MIDs answered '='
	DropAfterBlocks bool            // hang up as soon as a message's blocks arrive
	Garbage         bool            // answer the first proposals with a line that is not FS
	FailLogin       bool            // refuse every secure login answer

	// Log, if set, gets every line the fake sends and receives.
	Log func(fromServer bool, line string)
	// OnReceive, if set, is called with each message a station sends, after
	// it is stored. It may call Queue.
	OnReceive func(Mail)

	mu       sync.Mutex
	waiting  map[string][]Mail
	received []Mail
}

// Queue holds a message for a station, to be offered when it connects.
func (c *CMS) Queue(call string, m Mail) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.waiting == nil {
		c.waiting = map[string][]Mail{}
	}
	c.waiting[call] = append(c.waiting[call], m)
}

// Waiting returns the messages still held for a station.
func (c *CMS) Waiting(call string) []Mail {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Mail(nil), c.waiting[call]...)
}

// Received returns the messages stations have sent, in order.
func (c *CMS) Received() []Mail {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Mail(nil), c.received...)
}

// ListenAndServe serves every connection on ln until it is closed.
func (c *CMS) ListenAndServe(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go c.Serve(conn)
	}
}

type conn struct {
	*CMS
	rw   io.ReadWriter
	r    *bufio.Reader
	call string
}

// Serve runs one connection to the end, then closes it.
func (c *CMS) Serve(nc net.Conn) error {
	defer nc.Close()
	x := &conn{CMS: c, rw: nc, r: bufio.NewReader(nc)}
	return x.serve()
}

func (x *conn) send(line string) error {
	if x.Log != nil {
		x.Log(true, line)
	}
	_, err := io.WriteString(x.rw, line+"\r")
	return err
}

func (x *conn) line() (string, error) {
	s, err := x.r.ReadString('\r')
	if err != nil {
		return "", err
	}
	s = strings.TrimSpace(s)
	if x.Log != nil {
		x.Log(false, s)
	}
	return s, nil
}

func (x *conn) fail(text string) error {
	x.send("*** " + text)
	return errors.New(text)
}

func (x *conn) serve() error {
	x.send("Callsign :")
	call, err := x.line()
	if err != nil {
		return err
	}
	x.call = strings.ToUpper(call)
	x.send("Password :")
	if pw, err := x.line(); err != nil {
		return err
	} else if pw != "CMSTelnet" {
		return x.fail("Telnet password not accepted")
	}
	x.send("[WL2K-5.0-B2FWIHJM$]")
	x.send(";PQ: " + Challenge)
	x.send("CMS via FAKECMS >")

	// The client's handshake, up to the first line of its first turn.
	var first, pr string
	for {
		l, err := x.line()
		if err != nil {
			return err
		}
		if strings.HasPrefix(l, ";PR:") {
			pr = strings.TrimSpace(l[4:])
		}
		if strings.HasPrefix(l, "F") {
			first = l
			break
		}
	}
	pw, known := x.Passwords[x.call]
	if !known || x.FailLogin || pr != winlink.SecureLoginResponse(Challenge, pw) {
		return x.fail("[1] Secure login failed - account password does not match. - Disconnecting (127.0.0.1)")
	}

	offered := map[string]bool{}
	clientDone := false
	for turn := 0; ; turn++ {
		quit, done, err := x.clientTurn(first, turn)
		first = ""
		if err != nil || quit {
			return err
		}
		clientDone = done

		var batch []Mail
		for _, m := range x.Waiting(x.call) {
			if !offered[m.MID] && len(batch) < 5 {
				batch = append(batch, m)
				offered[m.MID] = true
			}
		}
		if len(batch) == 0 {
			if clientDone {
				return x.send("FQ")
			}
			x.send("FF")
			continue
		}
		if err := x.offer(batch); err != nil {
			return err
		}
	}
}

// clientTurn reads the client's proposals, FF or FQ. first is a line
// already read, or "".
func (x *conn) clientTurn(first string, turn int) (quit, done bool, err error) {
	type prop struct {
		mid         string
		size, csize int
	}
	var props []prop
	var sum int
	for {
		l := first
		first = ""
		if l == "" {
			if l, err = x.line(); err != nil {
				return false, false, err
			}
		}
		switch {
		case l == "" || l[0] == ';':
		case l == "FF":
			return false, true, nil
		case l == "FQ":
			return true, false, nil
		case strings.HasPrefix(l, "FC "):
			f := strings.Fields(l)
			if len(f) != 6 {
				return false, false, x.fail("bad proposal " + l)
			}
			size, _ := strconv.Atoi(f[3])
			csize, _ := strconv.Atoi(f[4])
			props = append(props, prop{f[2], size, csize})
			for i := 0; i < len(l); i++ {
				sum += int(l[i])
			}
			sum += '\r'
		case strings.HasPrefix(l, "F>"):
			theirs, err := strconv.ParseUint(strings.TrimSpace(l[2:]), 16, 8)
			if err != nil || int(theirs) != (-sum)&0xff {
				return false, false, x.fail("proposal checksum error")
			}
			if x.Garbage && turn == 0 {
				return false, false, x.send("FX what is this")
			}
			answers := ""
			for _, p := range props {
				switch {
				case x.Have[p.mid]:
					answers += "-"
				case x.Defer[p.mid]:
					answers += "="
				default:
					answers += "+"
				}
			}
			x.send("FS " + answers)
			for i, p := range props {
				if answers[i] != '+' {
					continue
				}
				data, err := x.blocks(p.csize)
				if err != nil {
					return false, false, err
				}
				if x.DropAfterBlocks {
					return true, false, errors.New("dropped the connection, as asked")
				}
				raw, err := lzhuf.DecompressB2(data, p.size)
				if err != nil {
					return false, false, x.fail(p.mid + ": " + err.Error())
				}
				m, err := winlink.Parse(raw)
				subject := ""
				if err == nil {
					subject = m.Subject
				}
				mail := Mail{MID: p.mid, Subject: subject, Raw: raw, From: x.call}
				x.mu.Lock()
				x.received = append(x.received, mail)
				x.mu.Unlock()
				if x.OnReceive != nil {
					x.OnReceive(mail)
				}
			}
			return false, false, nil
		default:
			return false, false, x.fail("unexpected " + l)
		}
	}
}

// blocks reads SOH, STX blocks and EOT, for csize bytes of data.
func (x *conn) blocks(csize int) ([]byte, error) {
	b, err := x.r.ReadByte()
	if err != nil || b != 1 {
		return nil, fmt.Errorf("expected SOH, got %d (%v)", b, err)
	}
	n, _ := x.r.ReadByte()
	if _, err := io.ReadFull(x.r, make([]byte, n)); err != nil {
		return nil, err
	}
	var data []byte
	sum := 0
	for {
		b, err := x.r.ReadByte()
		if err != nil {
			return nil, err
		}
		switch b {
		case 2:
			n, _ := x.r.ReadByte()
			k := int(n)
			if k == 0 {
				k = 256
			}
			chunk := make([]byte, k)
			if _, err := io.ReadFull(x.r, chunk); err != nil {
				return nil, err
			}
			for _, c := range chunk {
				sum += int(c)
			}
			data = append(data, chunk...)
		case 4:
			c, _ := x.r.ReadByte()
			if (sum+int(c))&0xff != 0 || len(data) != csize {
				return nil, x.fail("block checksum or length error")
			}
			return data, nil
		default:
			return nil, x.fail(fmt.Sprintf("unexpected byte %d in blocks", b))
		}
	}
}

// offer proposes a batch, reads the answers and sends what was accepted.
// A message counts as delivered when the client's next line arrives.
func (x *conn) offer(batch []Mail) error {
	type out struct {
		Mail
		data []byte
	}
	var outs []out
	sum := 0
	for _, m := range batch {
		data := lzhuf.CompressB2(m.Raw)
		outs = append(outs, out{m, data})
		l := fmt.Sprintf("FC EM %s %d %d 0", m.MID, len(m.Raw), len(data))
		x.send(l)
		for i := 0; i < len(l); i++ {
			sum += int(l[i])
		}
		sum += '\r'
	}
	x.send(fmt.Sprintf("F> %02X", (-sum)&0xff))
	l, err := x.line()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(l, "FS ") || len(l) != 3+len(batch) {
		return x.fail("bad answer " + l)
	}
	var delivered []string
	for i, o := range outs {
		switch l[3+i] {
		case '+':
			title := o.Subject
			if title == "" {
				title = "No subject"
			}
			hdr := title + "\x000\x00"
			w := bufio.NewWriter(x.rw)
			w.Write([]byte{1, byte(len(hdr))})
			w.WriteString(hdr)
			s := 0
			for p := 0; p < len(o.data); p += 250 {
				chunk := o.data[p:min(p+250, len(o.data))]
				w.Write([]byte{2, byte(len(chunk))})
				w.Write(chunk)
				for _, c := range chunk {
					s += int(c)
				}
			}
			w.Write([]byte{4, byte(-s & 0xff)})
			if err := w.Flush(); err != nil {
				return err
			}
			delivered = append(delivered, o.MID)
		case '-':
			delivered = append(delivered, o.MID)
		}
	}
	if len(delivered) == 0 {
		return nil
	}
	if _, err := x.r.Peek(1); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	keep := x.waiting[x.call][:0]
	for _, m := range x.waiting[x.call] {
		if !contains(delivered, m.MID) {
			keep = append(keep, m)
		}
	}
	x.waiting[x.call] = keep
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
