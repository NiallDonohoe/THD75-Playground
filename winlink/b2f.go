package winlink

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
)

// B2F, as the Winlink Development Team describes it and as Pat and
// paclink-unix implement it. Lines end in CR; a CMS may add LF.

// ErrProtocol marks a peer that broke the protocol: a bad checksum, a line
// we cannot parse, or bytes where a block should be.
var ErrProtocol = errors.New("B2F protocol error")

// RemoteError is a "*** ..." line from the peer, which ends the session.
type RemoteError struct{ Text string }

func (e *RemoteError) Error() string { return "server: " + e.Text }

// LoginFailed reports whether the peer refused our secure login answer.
func (e *RemoteError) LoginFailed() bool {
	return strings.Contains(strings.ToLower(e.Text), "secure login failed")
}

const (
	maxLine       = 1024 // longest protocol line accepted
	maxProposals  = 5    // proposals per turn, as Pat and paclink-unix send
	maxBlockData  = 250  // bytes per STX block
	maxTitleBytes = 80
	soh, stx, eot = 0x01, 0x02, 0x04
)

// sidFeatures are the SID flags we send: B2F (compressed v2 with Winlink's
// extensions), F (FBB basic), H (hierarchical addresses), M (message IDs)
// and $ (BIDs, always last). The CMS sends more (W, I, J), which we neither
// need nor claim.
const sidFeatures = "B2FHM$"

// formatSID returns our SID line, without its CR.
func formatSID(name, version string) string {
	return fmt.Sprintf("[%s-%s-%s]", name, version, sidFeatures)
}

// sidFlags returns the flags in a SID line such as "[WL2K-5.0-B2FWIHJM$]":
// what follows the last '-', upper-cased.
func sidFlags(line string) (string, bool) {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return "", false
	}
	i := strings.LastIndexByte(line, '-')
	if i < 0 {
		return "", false
	}
	return strings.ToUpper(line[i+1 : len(line)-1]), true
}

// secureSalt is the Winlink secure login salt, as published in
// paclink-unix and Pat.
var secureSalt = []byte{
	77, 197, 101, 206, 190, 249, 93, 200, 51, 243, 93, 237, 71, 94, 239, 138,
	68, 108, 70, 185, 225, 137, 217, 16, 51, 122, 193, 48, 194, 195, 198, 175,
	172, 169, 70, 84, 61, 62, 104, 186, 114, 52, 61, 168, 66, 129, 192, 208,
	187, 249, 232, 193, 41, 113, 41, 45, 240, 16, 29, 228, 208, 228, 61, 20,
}

// SecureLoginResponse answers a ";PQ:" challenge: MD5 of the challenge,
// the password and the salt; the first four bytes, little-endian, with the
// top two bits cleared; the last 8 decimal digits.
func SecureLoginResponse(challenge, password string) string {
	sum := md5.Sum([]byte(challenge + password + string(secureSalt)))
	v := uint32(sum[3]&0x3f)<<24 | uint32(sum[2])<<16 | uint32(sum[1])<<8 | uint32(sum[0])
	s := fmt.Sprintf("%08d", v)
	return s[len(s)-8:]
}

// Proposal is one "FC" line: a message offered for transfer.
type Proposal struct {
	MID            string
	Size           int // uncompressed
	CompressedSize int
}

func (p Proposal) line() string {
	return fmt.Sprintf("FC EM %s %d %d 0", p.MID, p.Size, p.CompressedSize)
}

// parseProposal reads an "FC EM MID SIZE CSIZE 0" line. Other proposal
// types (FA, FB, FD) parse with ok but code other than 'C', so the caller
// can defer them.
func parseProposal(line string) (p Proposal, code byte, err error) {
	if len(line) < 3 || line[0] != 'F' || line[2] != ' ' {
		return p, 0, fmt.Errorf("%w: proposal %q", ErrProtocol, line)
	}
	code = line[1]
	f := strings.Fields(line[3:])
	if len(f) < 2 {
		return p, code, fmt.Errorf("%w: proposal %q", ErrProtocol, line)
	}
	p.MID = f[1]
	if code != 'C' {
		return p, code, nil
	}
	if len(f) != 5 || (f[0] != "EM" && f[0] != "CM") || !validMID(p.MID) {
		return p, code, fmt.Errorf("%w: proposal %q", ErrProtocol, line)
	}
	if p.Size, err = strconv.Atoi(f[2]); err != nil || p.Size < 0 {
		return p, code, fmt.Errorf("%w: proposal size in %q", ErrProtocol, line)
	}
	if p.CompressedSize, err = strconv.Atoi(f[3]); err != nil || p.CompressedSize < 6 {
		return p, code, fmt.Errorf("%w: proposal compressed size in %q", ErrProtocol, line)
	}
	return p, code, nil
}

// proposalChecksum accumulates the "F>" checksum: every byte of every
// proposal line, CR included.
func proposalChecksum(sum byte, line string) byte {
	for i := 0; i < len(line); i++ {
		sum += line[i]
	}
	return sum + '\r'
}

// Answers to a proposal, in an "FS" line.
const (
	Accept = '+'
	Reject = '-' // already have it
	Defer  = '=' // not now
)

// parseAnswers reads an "FS" line for n proposals. Each answer is one of
// +-= or their letter forms (Y; N, R; L, H), or an accept from an offset:
// '!' or 'A' and digits. Offsets come back in offsets, 0 when none.
func parseAnswers(line string, n int) (answers []byte, offsets []int, err error) {
	s, ok := strings.CutPrefix(line, "FS ")
	if !ok {
		return nil, nil, fmt.Errorf("%w: expected FS, got %q", ErrProtocol, line)
	}
	for len(s) > 0 {
		c := s[0]
		s = s[1:]
		off := 0
		switch c {
		case '+', 'Y', 'y':
			c = Accept
		case '-', 'N', 'n', 'R', 'r':
			c = Reject
		case '=', 'L', 'l', 'H', 'h':
			c = Defer
		case '!', 'A', 'a':
			i := 0
			for i < len(s) && i < 7 && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i == 0 {
				return nil, nil, fmt.Errorf("%w: offset answer without an offset in %q", ErrProtocol, line)
			}
			off, _ = strconv.Atoi(s[:i])
			s = s[i:]
			c = Accept
		default:
			return nil, nil, fmt.Errorf("%w: answer %q in %q", ErrProtocol, c, line)
		}
		answers = append(answers, c)
		offsets = append(offsets, off)
	}
	if len(answers) != n {
		return nil, nil, fmt.Errorf("%w: %d answers for %d proposals in %q", ErrProtocol, len(answers), n, line)
	}
	return answers, offsets, nil
}

// writeBlocks sends one message's compressed data, from offset: an SOH
// header (title, offset), STX blocks of at most maxBlockData bytes, and EOT
// with a checksum that makes the data bytes sum to zero.
func writeBlocks(w io.Writer, title string, offset int, data []byte) error {
	title = mime.QEncoding.Encode("utf-8", title)
	if title == "" {
		title = "No subject"
	}
	if len(title) > maxTitleBytes {
		title = title[:maxTitleBytes]
	}
	off := strconv.Itoa(offset)
	bw := bufio.NewWriter(w)
	bw.Write([]byte{soh, byte(len(title) + len(off) + 2)})
	bw.WriteString(title)
	bw.WriteByte(0)
	bw.WriteString(off)
	bw.WriteByte(0)
	var sum byte
	rest := data[offset:]
	for len(rest) > 0 {
		k := min(len(rest), maxBlockData)
		bw.Write([]byte{stx, byte(k)})
		bw.Write(rest[:k])
		for _, c := range rest[:k] {
			sum += c
		}
		rest = rest[k:]
	}
	bw.Write([]byte{eot, -sum})
	return bw.Flush()
}

// readBlocks reads one message's compressed data, which must be exactly
// size bytes from offset 0. A '*' where SOH should be is a remote error
// line.
func readBlocks(r *bufio.Reader, size int) (title string, data []byte, err error) {
	c, err := r.ReadByte()
	if err != nil {
		return "", nil, err
	}
	if c == '*' {
		line, _ := readLine(r)
		return "", nil, &RemoteError{Text: strings.TrimLeft("*"+line, "* ")}
	}
	if c != soh {
		return "", nil, fmt.Errorf("%w: expected SOH, got 0x%02x", ErrProtocol, c)
	}
	hlen, err := r.ReadByte()
	if err != nil {
		return "", nil, err
	}
	header := make([]byte, hlen)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", nil, err
	}
	parts := strings.Split(string(header), "\x00")
	if len(parts) != 3 || parts[2] != "" {
		return "", nil, fmt.Errorf("%w: block header %q", ErrProtocol, header)
	}
	if parts[1] != "0" {
		return "", nil, fmt.Errorf("%w: offset %q, but we asked for the whole message", ErrProtocol, parts[1])
	}
	if t, err := new(mime.WordDecoder).DecodeHeader(parts[0]); err == nil {
		title = t
	} else {
		title = parts[0]
	}

	data = make([]byte, 0, size)
	var sum byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", nil, err
		}
		switch c {
		case stx:
			n, err := r.ReadByte()
			if err != nil {
				return "", nil, err
			}
			k := int(n)
			if k == 0 {
				k = 256
			}
			if len(data)+k > size {
				return "", nil, fmt.Errorf("%w: more data than the %d bytes proposed", ErrProtocol, size)
			}
			start := len(data)
			data = data[:start+k]
			if _, err := io.ReadFull(r, data[start:]); err != nil {
				return "", nil, err
			}
			for _, c := range data[start:] {
				sum += c
			}
		case eot:
			c, err := r.ReadByte()
			if err != nil {
				return "", nil, err
			}
			if sum+c != 0 {
				return "", nil, fmt.Errorf("%w: block checksum", ErrProtocol)
			}
			if len(data) != size {
				return "", nil, fmt.Errorf("%w: %d bytes received, %d proposed", ErrProtocol, len(data), size)
			}
			return title, data, nil
		default:
			return "", nil, fmt.Errorf("%w: expected STX or EOT, got 0x%02x", ErrProtocol, c)
		}
	}
}

// readLine reads one CR-terminated line, dropping LFs and the spaces
// around it, and refuses lines over maxLine bytes.
func readLine(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(b) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return "", err
		}
		switch c {
		case '\r':
			return strings.TrimSpace(string(b)), nil
		case '\n':
			continue
		}
		if len(b) == maxLine {
			return "", fmt.Errorf("%w: line over %d bytes", ErrProtocol, maxLine)
		}
		b = append(b, c)
	}
}
