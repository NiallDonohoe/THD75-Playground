// Package winlinkapp is d75winlink's state: the mailbox on disk, the
// session runner and the event stream the page follows. See
// docs/WINLINK.md.
package winlinkapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"d75scope/lzhuf"
	"d75scope/winlink"
)

// Folders, as directory names under the mailbox.
const (
	In      = "in"
	Out     = "out"
	Sent    = "sent"
	Archive = "archive"
)

var folders = []string{In, Out, Sent, Archive}

// ErrNotFound marks a message ID that is not in the mailbox, or not in the
// folder asked for.
var ErrNotFound = errors.New("no such message")

// ErrNotAllowed marks a move or delete the mailbox does not offer.
var ErrNotAllowed = errors.New("not allowed")

// Summary is one message as the folder lists show it.
type Summary struct {
	MID        string    `json:"mid"`
	Folder     string    `json:"folder"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Cc         []string  `json:"cc,omitempty"`
	Subject    string    `json:"subject"`
	Date       time.Time `json:"date"`
	Size       int       `json:"size"`                 // bytes, as sent
	Compressed int       `json:"compressed,omitempty"` // bytes on the air; outbox only
	Files      int       `json:"files"`
	Read       bool      `json:"read"`
	Broken     bool      `json:"broken,omitempty"` // received, but would not parse
}

// Mailbox is the message store: one MID.b2f file per message, in a folder
// per state, under one directory. It is safe for concurrent use, and
// implements winlink.Mailbox for sessions.
type Mailbox struct {
	dir string

	mu       sync.Mutex
	index    map[string]*Summary
	read     map[string]bool
	onChange func()
}

// OpenMailbox opens the mailbox in dir, creating its folders if needed,
// and reads every message's headers.
func OpenMailbox(dir string) (*Mailbox, error) {
	mb := &Mailbox{dir: dir, index: map[string]*Summary{}, read: map[string]bool{}}
	for _, f := range folders {
		if err := os.MkdirAll(filepath.Join(dir, f), 0o700); err != nil {
			return nil, err
		}
		names, err := filepath.Glob(filepath.Join(dir, f, "*.b2f"))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			mid := strings.TrimSuffix(filepath.Base(name), ".b2f")
			if !winlink.ValidMID(mid) {
				continue
			}
			raw, err := os.ReadFile(name)
			if err != nil {
				return nil, err
			}
			mb.index[mid] = summarise(mid, f, raw)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "read.json")); err == nil {
		var mids []string
		if json.Unmarshal(b, &mids) == nil {
			for _, m := range mids {
				mb.read[m] = true
			}
		}
	}
	for mid, s := range mb.index {
		s.Read = s.Folder != In || mb.read[mid]
	}
	return mb, nil
}

// Dir is the mailbox's directory.
func (mb *Mailbox) Dir() string { return mb.dir }

func summarise(mid, folder string, raw []byte) *Summary {
	s := &Summary{MID: mid, Folder: folder, Size: len(raw)}
	m, err := winlink.Parse(raw)
	if err != nil {
		s.Broken = true
		s.Subject = "(could not be read)"
		return s
	}
	s.From, s.To, s.Cc, s.Subject, s.Date, s.Files = m.From, m.To, m.Cc, m.Subject, m.Date, len(m.Files)
	if folder == Out {
		s.Compressed = len(lzhuf.CompressB2(raw))
	}
	return s
}

// setOnChange sets a function called, without the lock, after every change.
func (mb *Mailbox) setOnChange(f func()) {
	mb.mu.Lock()
	mb.onChange = f
	mb.mu.Unlock()
}

func (mb *Mailbox) changed() {
	mb.mu.Lock()
	f := mb.onChange
	mb.mu.Unlock()
	if f != nil {
		f()
	}
}

func (mb *Mailbox) path(folder, mid string) string {
	return filepath.Join(mb.dir, folder, mid+".b2f")
}

// List returns every message, newest first.
func (mb *Mailbox) List() []Summary {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	out := make([]Summary, 0, len(mb.index))
	for _, s := range mb.index {
		c := *s
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].MID < out[j].MID
	})
	return out
}

// Get returns a message's summary, its parsed form (nil if it does not
// parse) and its raw form.
func (mb *Mailbox) Get(mid string) (Summary, *winlink.Message, []byte, error) {
	mb.mu.Lock()
	s, ok := mb.index[mid]
	var sum Summary
	if ok {
		sum = *s
	}
	mb.mu.Unlock()
	if !ok {
		return Summary{}, nil, nil, ErrNotFound
	}
	raw, err := os.ReadFile(mb.path(sum.Folder, mid))
	if err != nil {
		return Summary{}, nil, nil, err
	}
	m, _ := winlink.Parse(raw)
	return sum, m, raw, nil
}

// Queue formats a message and puts it in the outbox.
func (mb *Mailbox) Queue(m *winlink.Message) (Summary, error) {
	raw, err := m.Format()
	if err != nil {
		return Summary{}, err
	}
	mb.mu.Lock()
	if _, dup := mb.index[m.MID]; dup {
		mb.mu.Unlock()
		return Summary{}, fmt.Errorf("message ID %s is already in the mailbox", m.MID)
	}
	if err := writeFile(mb.path(Out, m.MID), raw); err != nil {
		mb.mu.Unlock()
		return Summary{}, err
	}
	s := summarise(m.MID, Out, raw)
	s.Read = true
	mb.index[m.MID] = s
	sum := *s
	mb.mu.Unlock()
	mb.changed()
	return sum, nil
}

// writeFile writes through a temporary file and a rename, so a message is
// never half on disk.
func writeFile(name string, b []byte) error {
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// moves are the moves the page offers.
var moves = map[[2]string]bool{
	{In, Archive}:   true,
	{Sent, Archive}: true,
	{Archive, In}:   true,
}

// Move moves a message to another folder, if the page offers that move.
func (mb *Mailbox) Move(mid, to string) error {
	mb.mu.Lock()
	s, ok := mb.index[mid]
	if !ok {
		mb.mu.Unlock()
		return ErrNotFound
	}
	if !moves[[2]string{s.Folder, to}] {
		mb.mu.Unlock()
		return fmt.Errorf("%w: moving from %s to %s", ErrNotAllowed, s.Folder, to)
	}
	err := mb.moveLocked(s, to)
	mb.mu.Unlock()
	if err == nil {
		mb.changed()
	}
	return err
}

func (mb *Mailbox) moveLocked(s *Summary, to string) error {
	if err := os.Rename(mb.path(s.Folder, s.MID), mb.path(to, s.MID)); err != nil {
		return err
	}
	s.Folder = to
	s.Compressed = 0
	return nil
}

// Delete removes a message from the outbox (before it is sent) or the
// archive. Received and sent mail is archived, not deleted, first.
func (mb *Mailbox) Delete(mid string) error {
	mb.mu.Lock()
	s, ok := mb.index[mid]
	if !ok {
		mb.mu.Unlock()
		return ErrNotFound
	}
	if s.Folder != Out && s.Folder != Archive {
		mb.mu.Unlock()
		return fmt.Errorf("%w: deleting from %s", ErrNotAllowed, s.Folder)
	}
	err := os.Remove(mb.path(s.Folder, mid))
	if err == nil {
		delete(mb.index, mid)
		delete(mb.read, mid)
	}
	mb.mu.Unlock()
	if err == nil {
		mb.changed()
	}
	return err
}

// MarkRead marks a received message as read.
func (mb *Mailbox) MarkRead(mid string) error {
	mb.mu.Lock()
	s, ok := mb.index[mid]
	if !ok {
		mb.mu.Unlock()
		return ErrNotFound
	}
	if s.Read {
		mb.mu.Unlock()
		return nil
	}
	s.Read = true
	mb.read[mid] = true
	err := mb.saveReadLocked()
	mb.mu.Unlock()
	mb.changed()
	return err
}

func (mb *Mailbox) saveReadLocked() error {
	mids := make([]string, 0, len(mb.read))
	for m := range mb.read {
		if _, ok := mb.index[m]; ok {
			mids = append(mids, m)
		}
	}
	sort.Strings(mids)
	b, _ := json.Marshal(mids)
	return writeFile(filepath.Join(mb.dir, "read.json"), b)
}

// Counts returns the number of messages in each folder, and the unread
// ones in the inbox under "unread".
func (mb *Mailbox) Counts() map[string]int {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	c := map[string]int{In: 0, Out: 0, Sent: 0, Archive: 0, "unread": 0}
	for _, s := range mb.index {
		c[s.Folder]++
		if !s.Read {
			c["unread"]++
		}
	}
	return c
}

// Outbox returns the outbox, oldest first (winlink.Mailbox).
func (mb *Mailbox) Outbox() []winlink.Outgoing {
	mb.mu.Lock()
	var list []*Summary
	for _, s := range mb.index {
		if s.Folder == Out {
			list = append(list, s)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].Date.Equal(list[j].Date) {
			return list[i].Date.Before(list[j].Date)
		}
		return list[i].MID < list[j].MID
	})
	mb.mu.Unlock()
	var out []winlink.Outgoing
	for _, s := range list {
		raw, err := os.ReadFile(mb.path(Out, s.MID))
		if err != nil {
			continue
		}
		out = append(out, winlink.Outgoing{MID: s.MID, Subject: s.Subject, Raw: raw})
	}
	return out
}

// Have reports whether a message ID is anywhere in the mailbox
// (winlink.Mailbox).
func (mb *Mailbox) Have(mid string) bool {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	_, ok := mb.index[mid]
	return ok
}

// Received puts an arriving message in the inbox, unread (winlink.Mailbox).
func (mb *Mailbox) Received(mid string, raw []byte, _ *winlink.Message) error {
	if !winlink.ValidMID(mid) {
		return fmt.Errorf("message ID %q", mid)
	}
	mb.mu.Lock()
	if _, dup := mb.index[mid]; dup {
		mb.mu.Unlock()
		return nil
	}
	if err := writeFile(mb.path(In, mid), raw); err != nil {
		mb.mu.Unlock()
		return err
	}
	mb.index[mid] = summarise(mid, In, raw)
	mb.mu.Unlock()
	mb.changed()
	return nil
}

// Sent moves a message from the outbox to sent (winlink.Mailbox). A
// message the server already had is sent all the same.
func (mb *Mailbox) Sent(mid string, _ bool) {
	mb.mu.Lock()
	s, ok := mb.index[mid]
	if !ok || s.Folder != Out {
		mb.mu.Unlock()
		return
	}
	mb.moveLocked(s, Sent)
	mb.mu.Unlock()
	mb.changed()
}
