package lzhuf

import "fmt"

// encoder holds LZHUF.C's state: the ring buffer, the binary search trees
// that find the longest match, and the bit writer.
type encoder struct {
	h *huff

	text          [n + f - 1]byte // the first f-1 bytes are mirrored past n
	lson, dad     [n + 1]int
	rson          [n + 257]int // rson[n+1+c] is the root of the tree for first byte c
	matchLength   int
	matchPosition int

	out    []byte
	putbuf uint16
	putlen int
}

func newEncoder() *encoder {
	e := &encoder{h: newHuff()}
	for i := n + 1; i <= n+256; i++ {
		e.rson[i] = nilNode
	}
	for i := 0; i < n; i++ {
		e.dad[i] = nilNode
	}
	return e
}

// insertNode adds the string at r to the trees and leaves the longest
// match for it in matchLength and matchPosition (the nearest, on a tie).
func (e *encoder) insertNode(r int) {
	key := e.text[r:]
	p := n + 1 + int(key[0])
	e.rson[r], e.lson[r] = nilNode, nilNode
	e.matchLength = 0
	cmp := 1
	for {
		if cmp >= 0 {
			if e.rson[p] == nilNode {
				e.rson[p] = r
				e.dad[r] = p
				return
			}
			p = e.rson[p]
		} else {
			if e.lson[p] == nilNode {
				e.lson[p] = r
				e.dad[r] = p
				return
			}
			p = e.lson[p]
		}
		i := 1
		for ; i < f; i++ {
			if cmp = int(key[i]) - int(e.text[p+i]); cmp != 0 {
				break
			}
		}
		if i > threshold {
			pos := ((r - p) & (n - 1)) - 1
			if i > e.matchLength {
				e.matchPosition = pos
				if e.matchLength = i; e.matchLength >= f {
					break
				}
			}
			if i == e.matchLength && pos < e.matchPosition {
				e.matchPosition = pos
			}
		}
	}
	// A full-length match: r replaces p in the tree.
	e.dad[r] = e.dad[p]
	e.lson[r] = e.lson[p]
	e.rson[r] = e.rson[p]
	e.dad[e.lson[p]] = r
	e.dad[e.rson[p]] = r
	if e.rson[e.dad[p]] == p {
		e.rson[e.dad[p]] = r
	} else {
		e.lson[e.dad[p]] = r
	}
	e.dad[p] = nilNode
}

func (e *encoder) deleteNode(p int) {
	if e.dad[p] == nilNode {
		return
	}
	var q int
	switch {
	case e.rson[p] == nilNode:
		q = e.lson[p]
	case e.lson[p] == nilNode:
		q = e.rson[p]
	default:
		q = e.lson[p]
		if e.rson[q] != nilNode {
			for e.rson[q] != nilNode {
				q = e.rson[q]
			}
			e.rson[e.dad[q]] = e.lson[q]
			e.dad[e.lson[q]] = e.dad[q]
			e.lson[q] = e.lson[p]
			e.dad[e.lson[p]] = q
		}
		e.rson[q] = e.rson[p]
		e.dad[e.rson[p]] = q
	}
	e.dad[q] = e.dad[p]
	if e.rson[e.dad[p]] == p {
		e.rson[e.dad[p]] = q
	} else {
		e.lson[e.dad[p]] = q
	}
	e.dad[p] = nilNode
}

// putCode writes the top l bits of c.
func (e *encoder) putCode(l int, c uint16) {
	e.putbuf |= c >> e.putlen
	if e.putlen += l; e.putlen < 8 {
		return
	}
	e.out = append(e.out, byte(e.putbuf>>8))
	if e.putlen -= 8; e.putlen >= 8 {
		e.out = append(e.out, byte(e.putbuf))
		e.putlen -= 8
		e.putbuf = c << (l - e.putlen)
	} else {
		e.putbuf <<= 8
	}
}

// encodeChar writes symbol c's path from the root. LZHUF.C builds the code
// in 16 bits and loses the end of a longer one; frequencies up to maxFreq
// allow paths of about 23 bits, so it is built in 32 and written in two
// parts. Up to 16 bits the output is the same.
func (e *encoder) encodeChar(c int) {
	var code uint32
	bits := 0
	for k := e.h.prnt[c+t]; k != root; k = e.h.prnt[k] {
		code >>= 1
		if k&1 != 0 {
			code |= 1 << 31
		}
		bits++
	}
	if bits <= 16 {
		e.putCode(bits, uint16(code>>16))
	} else {
		e.putCode(16, uint16(code>>16))
		e.putCode(bits-16, uint16(code))
	}
	e.h.update(c)
}

func (e *encoder) encodePosition(c int) {
	e.putCode(int(pLen[c>>6]), uint16(pCode[c>>6])<<8)
	e.putCode(6, uint16(c&0x3f)<<10)
}

// encode is LZHUF.C's Encode, reading from data instead of a file.
func (e *encoder) encode(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	s, r := 0, n-f
	for i := s; i < r; i++ {
		e.text[i] = ' '
	}
	length := 0
	for ; length < f && length < len(data); length++ {
		e.text[r+length] = data[length]
	}
	next := length
	for i := 1; i <= f; i++ {
		e.insertNode(r - i)
	}
	e.insertNode(r)
	for length > 0 {
		if e.matchLength > length {
			e.matchLength = length
		}
		if e.matchLength <= threshold {
			e.matchLength = 1
			e.encodeChar(int(e.text[r]))
		} else {
			e.encodeChar(255 - threshold + e.matchLength)
			e.encodePosition(e.matchPosition)
		}
		last := e.matchLength
		i := 0
		for ; i < last && next < len(data); i++ {
			c := data[next]
			next++
			e.deleteNode(s)
			e.text[s] = c
			if s < f-1 {
				e.text[s+n] = c
			}
			s = (s + 1) & (n - 1)
			r = (r + 1) & (n - 1)
			e.insertNode(r)
		}
		for ; i < last; i++ {
			e.deleteNode(s)
			s = (s + 1) & (n - 1)
			r = (r + 1) & (n - 1)
			if length--; length > 0 {
				e.insertNode(r)
			}
		}
	}
	if e.putlen > 0 {
		e.out = append(e.out, byte(e.putbuf>>8))
	}
	return e.out
}

// bitReader reads the coded bits most significant first, and fails rather
// than inventing bits past the end.
type bitReader struct {
	b    []byte
	pos  int // in bits
	fail bool
}

func (br *bitReader) bit() int {
	if br.pos >= len(br.b)*8 {
		br.fail = true
		return 0
	}
	v := int(br.b[br.pos>>3]>>(7-br.pos&7)) & 1
	br.pos++
	return v
}

func (br *bitReader) byte8() int {
	v := 0
	for i := 0; i < 8; i++ {
		v = v<<1 | br.bit()
	}
	return v
}

// decode is LZHUF.C's Decode, for size bytes of output.
func decode(b []byte, size int) ([]byte, error) {
	h := newHuff()
	br := &bitReader{b: b}
	var text [n]byte
	for i := 0; i < n-f; i++ {
		text[i] = ' '
	}
	r := n - f
	out := make([]byte, 0, size)
	for len(out) < size {
		c := h.son[root]
		for c < t {
			c = h.son[c+br.bit()]
		}
		c -= t
		h.update(c)
		if br.fail {
			return nil, fmt.Errorf("%w: data ends after %d of %d bytes", ErrCorrupt, len(out), size)
		}
		if c < 256 {
			out = append(out, byte(c))
			text[r] = byte(c)
			r = (r + 1) & (n - 1)
			continue
		}
		i := br.byte8()
		hi := int(dCode[i]) << 6
		for j := int(dLen[i]) - 2; j > 0; j-- {
			i = i<<1 | br.bit()
		}
		if br.fail {
			return nil, fmt.Errorf("%w: data ends after %d of %d bytes", ErrCorrupt, len(out), size)
		}
		start := (r - (hi | i&0x3f) - 1) & (n - 1)
		count := c - 255 + threshold
		if len(out)+count > size {
			return nil, fmt.Errorf("%w: a match runs past the stated length of %d bytes", ErrCorrupt, size)
		}
		for k := 0; k < count; k++ {
			c := text[(start+k)&(n-1)]
			out = append(out, c)
			text[r] = c
			r = (r + 1) & (n - 1)
		}
	}
	return out, nil
}
