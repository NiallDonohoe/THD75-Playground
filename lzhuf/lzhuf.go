// Package lzhuf is LZHUF compression as FBB and Winlink's B2F use it: LZSS
// over a 2048-byte window with adaptive Huffman coding (Okumura and
// Yoshizaki's LZHUF.C), behind a 4-byte little-endian length. B2F puts a
// CRC-16 of the length and the coded bits in front of that.
//
// Messages are small (WINLINK.md caps them at 120 kB), so the package works
// on whole byte slices. Pure Go, no I/O.
package lzhuf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrCorrupt marks compressed data that cannot be decoded: cut short,
// failing its checksum, or decoding to more than its stated length.
var ErrCorrupt = errors.New("lzhuf: corrupt data")

// ErrTooLarge marks compressed data whose stated length is over the
// caller's limit.
var ErrTooLarge = errors.New("lzhuf: stated length over the limit")

const (
	n         = 2048                // ring buffer size
	f         = 60                  // lookahead size, the longest match
	threshold = 2                   // matches this short or shorter are coded as literals
	nChar     = 256 - threshold + f // symbols: 256 literals, then match lengths 3..f
	t         = nChar*2 - 1         // Huffman table size
	root      = t - 1
	maxFreq   = 0x8000
	nilNode   = n // the tree's "no node"
)

// huff is the adaptive Huffman tree, shared by the coder and the decoder so
// both adapt identically.
type huff struct {
	freq [t + 1]uint16 // freq[t] is a sentinel
	prnt [t + nChar]int
	son  [t]int
}

func newHuff() *huff {
	h := new(huff)
	for i := 0; i < nChar; i++ {
		h.freq[i] = 1
		h.son[i] = i + t
		h.prnt[i+t] = i
	}
	for i, j := 0, nChar; j <= root; i, j = i+2, j+1 {
		h.freq[j] = h.freq[i] + h.freq[i+1]
		h.son[j] = i
		h.prnt[i] = j
		h.prnt[i+1] = j
	}
	h.freq[t] = 0xffff
	h.prnt[root] = 0
	return h
}

// reconst halves every frequency and rebuilds the tree, when the root's
// count reaches maxFreq.
func (h *huff) reconst() {
	j := 0
	for i := 0; i < t; i++ {
		if h.son[i] >= t {
			h.freq[j] = (h.freq[i] + 1) / 2
			h.son[j] = h.son[i]
			j++
		}
	}
	for i, j := 0, nChar; j < t; i, j = i+2, j+1 {
		fr := h.freq[i] + h.freq[i+1]
		k := j - 1
		for fr < h.freq[k] {
			k--
		}
		k++
		copy(h.freq[k+1:j+1], h.freq[k:j])
		h.freq[k] = fr
		copy(h.son[k+1:j+1], h.son[k:j])
		h.son[k] = i
	}
	for i := 0; i < t; i++ {
		k := h.son[i]
		h.prnt[k] = i
		if k < t {
			h.prnt[k+1] = i
		}
	}
}

// update counts one more of symbol c and swaps nodes to keep the tree in
// frequency order.
func (h *huff) update(c int) {
	if h.freq[root] == maxFreq {
		h.reconst()
	}
	c = h.prnt[c+t]
	for {
		h.freq[c]++
		k := h.freq[c]
		if l := c + 1; k > h.freq[l] {
			for k > h.freq[l+1] {
				l++
			}
			h.freq[c] = h.freq[l]
			h.freq[l] = k

			i := h.son[c]
			h.prnt[i] = l
			if i < t {
				h.prnt[i+1] = l
			}
			j := h.son[l]
			h.son[l] = i
			h.prnt[j] = c
			if j < t {
				h.prnt[j+1] = c
			}
			h.son[c] = j
			c = l
		}
		if c = h.prnt[c]; c == 0 {
			return
		}
	}
}

// The upper 6 bits of a match position have fixed codes, from LZHUF.C.
var pCode = [64]uint8{
	0x00, 0x20, 0x30, 0x40, 0x50, 0x58, 0x60, 0x68,
	0x70, 0x78, 0x80, 0x88, 0x90, 0x94, 0x98, 0x9C,
	0xA0, 0xA4, 0xA8, 0xAC, 0xB0, 0xB4, 0xB8, 0xBC,
	0xC0, 0xC2, 0xC4, 0xC6, 0xC8, 0xCA, 0xCC, 0xCE,
	0xD0, 0xD2, 0xD4, 0xD6, 0xD8, 0xDA, 0xDC, 0xDE,
	0xE0, 0xE2, 0xE4, 0xE6, 0xE8, 0xEA, 0xEC, 0xEE,
	0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7,
	0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE, 0xFF,
}

var pLen = [64]uint8{
	3, 4, 4, 4, 5, 5, 5, 5, 5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

// dCode and dLen invert pCode and pLen: indexed by the next 8 bits of the
// stream, they give the upper 6 bits and the code's length.
var dCode, dLen [256]uint8

func init() {
	for i := 0; i < 64; i++ {
		span := 1 << (8 - pLen[i])
		for k := 0; k < span; k++ {
			dCode[int(pCode[i])+k] = uint8(i)
			dLen[int(pCode[i])+k] = pLen[i]
		}
	}
}

// Compress codes data, with its 4-byte length in front.
func Compress(data []byte) []byte {
	e := newEncoder()
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(data)))
	return append(out, e.encode(data)...)
}

// CompressB2 codes data as B2F sends it: a CRC-16 (little-endian) of what
// Compress returns, then that.
func CompressB2(data []byte) []byte {
	body := Compress(data)
	out := binary.LittleEndian.AppendUint16(nil, crc16(body))
	return append(out, body...)
}

// Decompress decodes what Compress produced. It refuses a stated length
// over limit before decoding anything.
func Decompress(b []byte, limit int) ([]byte, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("%w: %d bytes is shorter than the length header", ErrCorrupt, len(b))
	}
	size := binary.LittleEndian.Uint32(b)
	if uint64(size) > uint64(limit) {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, size, limit)
	}
	return decode(b[4:], int(size))
}

// DecompressB2 checks the CRC-16 in front, then decompresses.
func DecompressB2(b []byte, limit int) ([]byte, error) {
	if len(b) < 6 {
		return nil, fmt.Errorf("%w: %d bytes is shorter than the B2 header", ErrCorrupt, len(b))
	}
	if want, got := binary.LittleEndian.Uint16(b), crc16(b[2:]); want != got {
		return nil, fmt.Errorf("%w: CRC 0x%04x, expected 0x%04x", ErrCorrupt, got, want)
	}
	return Decompress(b[2:], limit)
}

// crc16 is CRC-16/XMODEM: polynomial 0x1021, initial 0, not reflected.
// (FBB code computes it as an augmented CRC with two zero bytes appended,
// which comes to the same thing.)
func crc16(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
