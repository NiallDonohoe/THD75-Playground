package ft8

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
)

// The 77-bit payload. Its last three bits, i3, say how the rest is laid
// out; when i3 is 0, the three before them (n3) choose a subtype.
//
//	i3=1, 2  standard: c28 r1 c28 r1 R1 g15   (r1 is /R for i3=1, /P for 2)
//	i3=4     non-standard call: h12 c58 h1 r2 c1
//	i3=0.0   free text: 13 characters
//	i3=0.1   DXpedition: c28 c28 h10 r5
//	i3=0.5   telemetry: 18 hex digits
//
// Other types (contests, Field Day, RTTY Roundup, EU VHF) come back as
// ErrUnsupported, so the decode is still counted but shown as its type.

// Payload is a 77-bit message, one bit per byte, MSB first.
type Payload [payloadBits]uint8

// ErrUnsupported is returned for a valid payload of a type this package
// does not unpack.
var ErrUnsupported = errors.New("ft8: message type not supported")

var errInvalid = errors.New("ft8: invalid message")

const (
	nTokens  = 2063592 // c28 values below this are DE, QRZ and CQ forms
	max22    = 4194304 // 22-bit hashes follow the tokens
	maxGrid4 = 32400   // g15 values below this are 4-character grids
)

const (
	alnumSp  = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alnum    = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits   = "0123456789"
	lettSp   = " ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	textSet  = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./?"
	callSet  = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/" // c58 and hashes
	hashMult = 47055833459
)

func (p *Payload) get(from, n int) uint64 {
	var v uint64
	for i := from; i < from+n; i++ {
		v = v<<1 | uint64(p[i])
	}
	return v
}

func (p *Payload) put(from, n int, v uint64) {
	for i := from + n - 1; i >= from; i-- {
		p[i] = uint8(v & 1)
		v >>= 1
	}
}

func (p *Payload) getBig(from, n int) *big.Int {
	v := new(big.Int)
	for i := from; i < from+n; i++ {
		v.Lsh(v, 1)
		v.SetBit(v, 0, uint(p[i]))
	}
	return v
}

func (p *Payload) putBig(from, n int, v *big.Int) {
	for i := 0; i < n; i++ {
		p[from+n-1-i] = uint8(v.Bit(i))
	}
}

// Type returns i3 and n3.
func (p *Payload) Type() (i3, n3 int) {
	return int(p.get(74, 3)), int(p.get(71, 3))
}

// callHash is WSJT-X's callsign hash, of m bits (10, 12 or 22).
func callHash(call string, m uint) uint32 {
	call = (call + "           ")[:11]
	var n uint64
	for i := 0; i < 11; i++ {
		j := strings.IndexByte(callSet, call[i])
		if j < 0 {
			j = 0
		}
		n = 38*n + uint64(j)
	}
	return uint32((hashMult * n) >> (64 - m))
}

// Callsigns remembers the callsigns decoded in full, so that a later
// message carrying only a hash of one (shown as <...>) can name it. The
// zero value is not usable; a nil *Callsigns resolves nothing. Safe for
// concurrent use.
type Callsigns struct {
	mu   sync.Mutex
	hash map[uint]map[uint32]string
}

// NewCallsigns returns an empty table.
func NewCallsigns() *Callsigns {
	return &Callsigns{hash: map[uint]map[uint32]string{10: {}, 12: {}, 22: {}}}
}

// Add records a callsign, such as the operator's own.
func (c *Callsigns) Add(call string) {
	if c == nil || len(call) < 3 || len(call) > 11 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for m, t := range c.hash {
		t[callHash(call, m)] = call
	}
}

func (c *Callsigns) lookup(m uint, h uint32) string {
	if c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if call, ok := c.hash[m][h]; ok {
			return "<" + call + ">"
		}
	}
	return "<...>"
}

// unpack turns a payload into text as WSJT-X prints it.
func unpack(p *Payload, calls *Callsigns) (string, error) {
	i3, n3 := p.Type()
	switch {
	case i3 == 1 || i3 == 2:
		return unpackStandard(p, i3, calls)
	case i3 == 4:
		return unpackNonstandard(p, calls)
	case i3 == 0 && n3 == 0:
		return unpackText(p), nil
	case i3 == 0 && n3 == 1:
		return unpackDXpedition(p, calls)
	case i3 == 0 && n3 == 5:
		return unpackTelemetry(p), nil
	case i3 == 0 && n3 > 6, i3 > 5:
		return "", errInvalid
	}
	return "", ErrUnsupported
}

func unpackStandard(p *Payload, i3 int, calls *Callsigns) (string, error) {
	n1, r1 := uint32(p.get(0, 28)), p.get(28, 1)
	n2, r2 := uint32(p.get(29, 28)), p.get(57, 1)
	ack, g := p.get(58, 1), int(p.get(59, 15))
	c1, ok1 := unpack28(n1, calls)
	c2, ok2 := unpack28(n2, calls)
	// The second call is never a CQ, QRZ or DE token, and a token takes
	// neither a suffix nor a roger.
	if !ok1 || !ok2 || n2 < nTokens || (n1 < nTokens && (r1 == 1 || ack == 1)) {
		return "", errInvalid
	}
	// Remember calls seen in full (not tokens or hashes).
	if n1 >= nTokens+max22 {
		calls.Add(c1)
	}
	if n2 >= nTokens+max22 {
		calls.Add(c2)
	}
	suffix := "/R"
	if i3 == 2 {
		suffix = "/P"
	}
	if r1 == 1 {
		c1 += suffix
	}
	if r2 == 1 {
		c2 += suffix
	}
	msg := c1 + " " + c2
	if g < maxGrid4 {
		grid := string([]byte{byte('A' + g/1800), byte('A' + g/100%18), byte('0' + g/10%10), byte('0' + g%10)})
		if ack == 1 {
			return msg + " R " + grid, nil
		}
		return msg + " " + grid, nil
	}
	switch rpt := g - maxGrid4; rpt {
	case 0:
		return "", errInvalid
	case 1:
		return msg, nil
	case 2:
		return msg + " RRR", nil
	case 3:
		return msg + " RR73", nil
	case 4:
		return msg + " 73", nil
	default:
		r := fmt.Sprintf("%+03d", rpt-35)
		if ack == 1 {
			return msg + " R" + r, nil
		}
		return msg + " " + r, nil
	}
}

// unpack28 decodes a c28 field: a token, a hashed call or a standard call.
func unpack28(n uint32, calls *Callsigns) (string, bool) {
	switch {
	case n == 0:
		return "DE", true
	case n == 1:
		return "QRZ", true
	case n == 2:
		return "CQ", true
	case n <= 1002:
		return fmt.Sprintf("CQ %03d", n-3), true
	case n < nTokens:
		m := n - 1003
		if m >= 27*27*27*27 {
			return "", false
		}
		var b [4]byte
		for i := 3; i >= 0; i-- {
			b[i] = lettSp[m%27]
			m /= 27
		}
		s := strings.TrimLeft(string(b[:]), " ")
		if s == "" || strings.Contains(s, " ") {
			return "", false
		}
		return "CQ " + s, true
	case n < nTokens+max22:
		return calls.lookup(22, n-nTokens), true
	}
	n -= nTokens + max22
	var b [6]byte
	for i, set := range []string{lettSp, lettSp, lettSp} {
		b[5-i] = set[n%27]
		n /= 27
	}
	b[2] = digits[n%10]
	n /= 10
	b[1] = alnum[n%36]
	n /= 36
	if n >= uint32(len(alnumSp)) {
		return "", false
	}
	b[0] = alnumSp[n]
	call := strings.TrimSpace(string(b[:]))
	if strings.Contains(call, " ") || len(call) < 3 {
		return "", false
	}
	switch {
	case strings.HasPrefix(call, "3D0") && len(call) > 3:
		call = "3DA0" + call[3:]
	case call[0] == 'Q' && call[1] >= 'A' && call[1] <= 'Z':
		call = "3X" + call[1:]
	}
	return call, true
}

func unpackNonstandard(p *Payload, calls *Callsigns) (string, error) {
	h := uint32(p.get(0, 12))
	n58 := p.get(12, 58)
	flip, rpt, cq := p.get(70, 1), p.get(71, 2), p.get(73, 1)
	var b [11]byte
	for i := 10; i >= 0; i-- {
		b[i] = callSet[n58%38]
		n58 /= 38
	}
	full := strings.TrimSpace(string(b[:]))
	if full == "" || strings.Contains(full, " ") {
		return "", errInvalid
	}
	// Look the hash up before remembering the full call, which could
	// share its 12-bit hash.
	c1, c2 := calls.lookup(12, h), full
	calls.Add(full)
	if cq == 1 {
		return "CQ " + full, nil
	}
	if flip == 1 {
		c1, c2 = c2, c1
	}
	msg := c1 + " " + c2
	return msg + [...]string{"", " RRR", " RR73", " 73"}[rpt], nil
}

func unpackText(p *Payload) string {
	n := p.getBig(0, 71)
	var b [13]byte
	base, r := big.NewInt(int64(len(textSet))), new(big.Int)
	for i := 12; i >= 0; i-- {
		n.QuoRem(n, base, r)
		b[i] = textSet[r.Int64()]
	}
	return strings.TrimSpace(string(b[:]))
}

func unpackTelemetry(p *Payload) string {
	s := strings.TrimLeft(fmt.Sprintf("%018X", p.getBig(0, 71)), "0")
	if s == "" {
		return "0"
	}
	return s
}

func unpackDXpedition(p *Payload, calls *Callsigns) (string, error) {
	c1, ok1 := unpack28(uint32(p.get(0, 28)), calls)
	c2, ok2 := unpack28(uint32(p.get(28, 28)), calls)
	n1, n2 := p.get(0, 28), p.get(28, 28)
	if !ok1 || !ok2 || n1 < nTokens || n2 < nTokens {
		return "", errInvalid
	}
	h := uint32(p.get(56, 10))
	rpt := 2*int(p.get(66, 5)) - 30
	return fmt.Sprintf("%s RR73; %s %s %+03d", c1, c2, calls.lookup(10, h), rpt), nil
}

// Pack turns message text into a payload. It packs the forms a station
// sends in a normal QSO (standard calls, CQ with a modifier, grids,
// reports, RRR, RR73, 73, /R and /P, a hashed call in angle brackets,
// non-standard calls) and falls back to free text of up to 13 characters.
func Pack(text string) (Payload, error) {
	words := strings.Fields(strings.ToUpper(text))
	if len(words) == 0 {
		return Payload{}, errors.New("ft8: empty message")
	}
	if p, ok := packStandard(words); ok {
		return p, nil
	}
	if p, ok := packNonstandard(words); ok {
		return p, nil
	}
	return packText(strings.Join(words, " "))
}

var reportRE = regexp.MustCompile(`^(R?)([+-][0-9]{2})$`)

func packStandard(words []string) (Payload, bool) {
	var p Payload
	// "CQ DX K1ABC FN42": the modifier joins the CQ.
	if len(words) >= 3 && words[0] == "CQ" {
		if _, ok := pack28("CQ " + words[1]); ok {
			if _, isCall := standardCall(words[1]); !isCall {
				words = append([]string{"CQ " + words[1]}, words[2:]...)
			}
		}
	}
	if len(words) < 2 || len(words) > 4 {
		return p, false
	}
	// /R and /P are one flag per call; i3 says which suffix it means, so a
	// message cannot mix them.
	i3, r := uint64(1), [2]uint64{}
	var calls [2]string
	sawR := false
	for k, w := range words[:2] {
		calls[k] = w
		switch {
		case strings.HasSuffix(w, "/R"):
			r[k], calls[k], sawR = 1, strings.TrimSuffix(w, "/R"), true
		case strings.HasSuffix(w, "/P"):
			r[k], calls[k], i3 = 1, strings.TrimSuffix(w, "/P"), 2
		}
	}
	if sawR && i3 == 2 {
		return p, false
	}
	n1, ok1 := pack28(calls[0])
	n2, ok2 := pack28(calls[1])
	if !ok1 || !ok2 || n2 < nTokens || (n1 < nTokens && r[0] == 1) {
		return p, false
	}
	var ack, g uint64
	rest := words[2:]
	if len(rest) == 2 {
		if rest[0] != "R" || n1 < nTokens {
			return p, false
		}
		ack, rest = 1, rest[1:]
	}
	if len(rest) == 0 {
		g = maxGrid4 + 1
	} else {
		w := rest[0]
		switch {
		case w == "RRR":
			g = maxGrid4 + 2
		case w == "RR73":
			g = maxGrid4 + 3
		case w == "73":
			g = maxGrid4 + 4
		case isGrid4(w):
			g = uint64(((int(w[0]-'A')*18)+int(w[1]-'A'))*100 + int(w[2]-'0')*10 + int(w[3]-'0'))
		case reportRE.MatchString(w) && ack == 0:
			m := reportRE.FindStringSubmatch(w)
			var v int
			fmt.Sscanf(m[2], "%d", &v)
			// −00 would come back as +00, the only form WSJT-X prints.
			if v < -30 || v > 49 || m[2] == "-00" || (m[1] == "R" && n1 < nTokens) {
				return p, false
			}
			if m[1] == "R" {
				ack = 1
			}
			g = uint64(maxGrid4 + 35 + v)
		default:
			return p, false
		}
		if ack == 1 && g > maxGrid4 && g < maxGrid4+5 {
			return p, false
		}
	}
	p.put(0, 28, uint64(n1))
	p.put(28, 1, r[0])
	p.put(29, 28, uint64(n2))
	p.put(57, 1, r[1])
	p.put(58, 1, ack)
	p.put(59, 15, g)
	p.put(74, 3, i3)
	return p, true
}

func isGrid4(w string) bool {
	return len(w) == 4 && w[0] >= 'A' && w[0] <= 'R' && w[1] >= 'A' && w[1] <= 'R' &&
		w[2] >= '0' && w[2] <= '9' && w[3] >= '0' && w[3] <= '9'
}

// pack28 encodes a c28 field.
func pack28(tok string) (uint32, bool) {
	switch tok {
	case "DE":
		return 0, true
	case "QRZ":
		return 1, true
	case "CQ":
		return 2, true
	}
	if mod, ok := strings.CutPrefix(tok, "CQ "); ok {
		if len(mod) == 3 && strings.Trim(mod, digits) == "" {
			return 3 + uint32((mod[0]-'0'))*100 + uint32(mod[1]-'0')*10 + uint32(mod[2]-'0'), true
		}
		if len(mod) >= 1 && len(mod) <= 4 && strings.Trim(mod, lettSp[1:]) == "" {
			var m uint32
			for _, c := range []byte(strings.Repeat(" ", 4-len(mod)) + mod) {
				m = 27*m + uint32(strings.IndexByte(lettSp, c))
			}
			return 1003 + m, true
		}
		return 0, false
	}
	if len(tok) > 2 && tok[0] == '<' && tok[len(tok)-1] == '>' {
		inner := tok[1 : len(tok)-1]
		if !validCall(inner) {
			return 0, false
		}
		return nTokens + callHash(inner, 22), true
	}
	c6, ok := standardCall(tok)
	if !ok {
		return 0, false
	}
	n := uint32(strings.IndexByte(alnumSp, c6[0]))
	n = n*36 + uint32(strings.IndexByte(alnum, c6[1]))
	n = n*10 + uint32(c6[2]-'0')
	for _, c := range []byte(c6[3:]) {
		n = n*27 + uint32(strings.IndexByte(lettSp, c))
	}
	return nTokens + max22 + n, true
}

// standardCall returns a callsign in the six-character form c28 encodes
// (call-area digit third), or false if it does not fit.
func standardCall(call string) (string, bool) {
	switch {
	case strings.HasPrefix(call, "3D0") && len(call) > 3:
		// 3D0 is how 3DA0 calls travel, so a literal 3D0 cannot.
		return "", false
	case strings.HasPrefix(call, "3DA0") && len(call) > 4:
		call = "3D0" + call[4:]
	case strings.HasPrefix(call, "3X") && len(call) > 2 && call[2] >= 'A' && call[2] <= 'Z':
		call = "Q" + call[2:]
	case len(call) > 1 && call[0] == 'Q' && call[1] >= 'A' && call[1] <= 'Z':
		// Q and a letter is how 3X calls travel; no country issues Q.
		return "", false
	}
	var c6 string
	switch {
	case len(call) >= 3 && isDigit(call[2]):
		c6 = call
	case len(call) >= 2 && isDigit(call[1]):
		c6 = " " + call
	default:
		return "", false
	}
	if len(c6) > 6 || len(c6) < 4 {
		return "", false
	}
	c6 += strings.Repeat(" ", 6-len(c6))
	if strings.IndexByte(alnumSp, c6[0]) < 0 || strings.IndexByte(alnum, c6[1]) < 0 || c6[0] == ' ' && c6[1] == ' ' {
		return "", false
	}
	suffix := strings.TrimRight(c6[3:], " ")
	if suffix == "" || strings.Trim(suffix, lettSp[1:]) != "" {
		return "", false
	}
	return c6, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// validCall accepts anything c58 can carry that looks like a callsign.
func validCall(call string) bool {
	if len(call) < 3 || len(call) > 11 || strings.Trim(call, callSet[1:]) != "" {
		return false
	}
	return strings.ContainsAny(call, digits)
}

func packNonstandard(words []string) (Payload, bool) {
	var p Payload
	var full string
	var h uint32
	var flip, rpt, cq uint64
	switch {
	case len(words) == 2 && words[0] == "CQ":
		full, cq = words[1], 1
	case len(words) == 2 || len(words) == 3:
		a, b := words[0], words[1]
		ha := len(a) > 2 && a[0] == '<' && a[len(a)-1] == '>'
		hb := len(b) > 2 && b[0] == '<' && b[len(b)-1] == '>'
		var hashed string
		switch {
		case ha && !hb:
			hashed, full = a[1:len(a)-1], b
		case hb && !ha:
			hashed, full, flip = b[1:len(b)-1], a, 1
		default:
			return p, false
		}
		if !validCall(hashed) {
			return p, false
		}
		h = callHash(hashed, 12)
		if len(words) == 3 {
			switch words[2] {
			case "RRR":
				rpt = 1
			case "RR73":
				rpt = 2
			case "73":
				rpt = 3
			default:
				return p, false
			}
		}
	default:
		return p, false
	}
	if !validCall(full) {
		return p, false
	}
	var n58 uint64
	for _, c := range []byte((full + "           ")[:11]) {
		n58 = 38*n58 + uint64(strings.IndexByte(callSet, c))
	}
	p.put(0, 12, uint64(h))
	p.put(12, 58, n58)
	p.put(70, 1, flip)
	p.put(71, 2, rpt)
	p.put(73, 1, cq)
	p.put(74, 3, 4)
	return p, true
}

func packText(s string) (Payload, error) {
	var p Payload
	if len(s) > 13 {
		return p, fmt.Errorf("ft8: %q does not fit: free text is at most 13 characters", s)
	}
	if strings.Trim(s, textSet) != "" {
		return p, fmt.Errorf("ft8: %q has characters free text cannot carry", s)
	}
	s = strings.Repeat(" ", 13-len(s)) + s
	n, base := new(big.Int), big.NewInt(int64(len(textSet)))
	for i := 0; i < 13; i++ {
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(strings.IndexByte(textSet, s[i]))))
	}
	p.putBig(0, 71, n)
	return p, nil // n3 = 0, i3 = 0
}
