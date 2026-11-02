// Package winlinkdemo runs d75winlink --demo: the fake CMS from
// winlinktest on a loopback port, with made-up mail waiting and a reply to
// everything sent, behind a link slowed to radio-like speed. Every
// callsign starts with Q, which no country issues, and every internet
// address is at example.com.
package winlinkdemo

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"d75scope/winlink"
	"d75scope/winlink/winlinktest"
)

// Password is the demo account's Winlink password.
const Password = "demo"

// Demo is a running fake CMS.
type Demo struct {
	call string
	cms  *winlinktest.CMS
	ln   net.Listener
	// Rate is the link's speed in bytes a second, both ways; 0 is
	// unlimited.
	Rate int

	mu      sync.Mutex
	pending []*winlink.Message // replies, held until the next connection
}

// Start starts the fake CMS for call, with three messages waiting.
func Start(call string) (*Demo, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	d := &Demo{call: call, ln: ln, Rate: 600}
	d.cms = &winlinktest.CMS{Passwords: map[string]string{call: Password}}
	d.cms.OnReceive = d.reply
	for _, m := range d.seed(time.Now().UTC()) {
		if err := d.queue(m); err != nil {
			ln.Close()
			return nil, err
		}
	}
	go d.cms.ListenAndServe(ln)
	return d, nil
}

// Close stops the fake CMS.
func (d *Demo) Close() error { return d.ln.Close() }

// CMS is the fake server, for tests.
func (d *Demo) CMS() *winlinktest.CMS { return d.cms }

// Dial connects to the fake CMS and logs in, as winlink.DialCMS does to
// the real one, through the slowed link.
func (d *Demo) Dial(ctx context.Context, log func(sent bool, line string)) (net.Conn, error) {
	d.mu.Lock()
	for _, m := range d.pending {
		d.queue(m)
	}
	d.pending = nil
	d.mu.Unlock()
	var nd net.Dialer
	conn, err := nd.DialContext(ctx, "tcp", d.ln.Addr().String())
	if err != nil {
		return nil, err
	}
	var c net.Conn = conn
	if d.Rate > 0 {
		c = &paced{Conn: conn, rate: d.Rate}
	}
	c.SetDeadline(time.Now().Add(30 * time.Second))
	if err := winlink.Login(c, d.call, log); err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

func (d *Demo) queue(m *winlink.Message) error {
	raw, err := m.Format()
	if err != nil {
		return err
	}
	d.cms.Queue(d.call, winlinktest.Mail{MID: m.MID, Subject: m.Subject, Raw: raw})
	return nil
}

// seed is the mail waiting at the start. The IDs are fixed, so on the
// next run the mailbox already has them and rejects them.
func (d *Demo) seed(now time.Time) []*winlink.Message {
	return []*winlink.Message{
		{
			MID: "Q4NETDEMO001", Date: now.Add(-3 * time.Hour), From: "Q4NET", To: []string{d.call},
			Subject: "Net tonight at 20:00 on the repeater",
			Body: "Hello all,\n\nThe weekly net is at 20:00 local on the club repeater. " +
				"Check in with your call, name and location. Winlink check-ins are welcome " +
				"by reply to this message before 19:30.\n\n73 de Q4NET (net control)",
		},
		{
			MID: "Q9FRNDDEMO02", Date: now.Add(-50 * time.Minute), From: "SMTP:friend@example.com", To: []string{d.call},
			Subject: "Re: Field day plans",
			Body: "Great to hear the radio is working. Map of the site attached; " +
				"we will set up by the north gate.\n\nSee you Saturday!",
			Files: []winlink.File{{Name: "site-map.txt", Data: []byte(siteMap)}},
		},
		{
			MID: "Q7EOCDEMO003", Date: now.Add(-10 * time.Minute), From: "Q7EOC", To: []string{d.call}, Cc: []string{"Q7OPS"},
			Subject: "Exercise traffic: shelter status",
			Body: "EXERCISE EXERCISE EXERCISE\n\nShelter: Community hall\nOccupancy: 42 of 120\n" +
				"Needs: water (200 L), cots (15)\nNext report: 18:00\n\nEXERCISE EXERCISE EXERCISE",
		},
	}
}

const siteMap = `      N
  +---------+  gate
  |  tents  |====
  |         |
  |  [ANT]  |   car park
  +---------+
`

// reply answers a message the station sent, from its first addressee. The
// answer waits for the next connection, as a real reply would take a while.
func (d *Demo) reply(in winlinktest.Mail) {
	m, err := winlink.Parse(in.Raw)
	if err != nil || len(m.To) == 0 {
		return
	}
	subject := m.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	quoted := "> " + strings.ReplaceAll(strings.TrimRight(m.Body, "\r\n"), "\r\n", "\n> ")
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = append(d.pending, &winlink.Message{
		MID:     winlink.NewMID(),
		Date:    time.Now().UTC(),
		From:    m.To[0],
		To:      []string{d.call},
		Subject: subject,
		Body: fmt.Sprintf("Thanks, your message arrived.\n\n(An automatic reply from the demo server. "+
			"Nothing was sent to %s.)\n\n%s", m.To[0], quoted),
	})
}

// paced slows reads and writes to rate bytes a second, like a radio link.
type paced struct {
	net.Conn
	rate int
}

func (p *paced) wait(n int) {
	time.Sleep(time.Duration(n) * time.Second / time.Duration(p.rate))
}

func (p *paced) Write(b []byte) (int, error) {
	n, err := p.Conn.Write(b)
	p.wait(n)
	return n, err
}

func (p *paced) Read(b []byte) (int, error) {
	// Small reads, so the pace shows block by block.
	if len(b) > 128 {
		b = b[:128]
	}
	n, err := p.Conn.Read(b)
	p.wait(n)
	return n, err
}
