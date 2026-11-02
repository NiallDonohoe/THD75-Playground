package lzhuf

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

const testLimit = 1 << 20

// huffSize is the Huffman table size t, which test functions shadow.
const huffSize = t

// Vectors from wl2k-go (see testdata/README.md), in B2F form.
var vectors = []struct {
	plain string
	b2    []byte
}{
	{"\n", []byte{0xe, 0x8f, 0x1, 0x0, 0x0, 0x0, 0xcb, 0x0}},
	{"foo", []byte{0xb6, 0x47, 0x3, 0x0, 0x0, 0x0, 0xf9, 0x7e, 0xf1, 0x0}},
	{"bar", []byte{0xc7, 0xef, 0x03, 0x00, 0x00, 0x00, 0xf7, 0x7b, 0x7f, 0xc0}},
	{"The quick brown fox jumps over the lazy dog\r\nThe quick brown fox jumps over the lazy dog",
		[]byte{0x76, 0x25, 0x58, 0x0, 0x0, 0x0, 0xf0, 0x7d, 0x3e, 0x3a, 0xcf, 0xe8, 0xf, 0xd7, 0xdf, 0xf7, 0xc2, 0xf7, 0x7f, 0xbf, 0x60, 0x7f, 0xab, 0x7f, 0x2b, 0xa0, 0x4b, 0x7f, 0x6c, 0xf, 0xcf, 0xf3, 0xff, 0x55, 0x60, 0x2c, 0x3b, 0xba, 0x80, 0x23, 0x3, 0xdf, 0x8f, 0x68, 0x30, 0x2d, 0x3f, 0xa, 0xff, 0x3c, 0xce, 0x5b, 0xf2, 0x2c}},
}

func TestVectors(t *testing.T) {
	for _, v := range vectors {
		if got := CompressB2([]byte(v.plain)); !bytes.Equal(got, v.b2) {
			t.Errorf("CompressB2(%q) = % x\nwant % x", v.plain, got, v.b2)
		}
		got, err := DecompressB2(v.b2, testLimit)
		if err != nil || string(got) != v.plain {
			t.Errorf("DecompressB2 for %q = %q, %v", v.plain, got, err)
		}
	}
}

// The fixture was compressed by a reference implementation: decoding it
// checks the decoder, and matching it byte for byte checks the encoder.
func TestFixture(t *testing.T) {
	plain, err := os.ReadFile("testdata/gettysburg.txt")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile("testdata/gettysburg.txt.lzh")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecompressB2(b2, testLimit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Error("decompressed fixture differs from the text")
	}
	if enc := CompressB2(plain); !bytes.Equal(enc, b2) {
		t.Errorf("CompressB2 differs from the fixture: %d bytes, fixture %d", len(enc), len(b2))
	}
}

func TestCRC16(t *testing.T) {
	// The standard check value for CRC-16/XMODEM.
	if got := crc16([]byte("123456789")); got != 0x31c3 {
		t.Errorf("crc16 = 0x%04x, want 0x31c3", got)
	}
}

func TestEmpty(t *testing.T) {
	b2 := CompressB2(nil)
	if len(b2) != 6 {
		t.Errorf("empty input compressed to %d bytes, want 6 (CRC and length)", len(b2))
	}
	got, err := DecompressB2(b2, testLimit)
	if err != nil || len(got) != 0 {
		t.Errorf("DecompressB2(empty) = %q, %v", got, err)
	}
}

func roundTrip(t *testing.T, name string, data []byte) {
	t.Helper()
	b2 := CompressB2(data)
	got, err := DecompressB2(b2, testLimit)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if !bytes.Equal(got, data) {
		t.Errorf("%s: round trip differs (%d bytes in, %d out)", name, len(data), len(got))
	}
}

func TestRoundTrips(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	random := func(size int) []byte {
		b := make([]byte, size)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		return b
	}
	text := strings.Repeat("Mid: Q1ABCDEFGHIJ\r\nSubject: test from Q1ABC\r\n73 de Q1ABC\r\n", 3000)
	// Skewed: one byte very common, a few rare ones, so the tree gets deep
	// and maxFreq forces rebuilds.
	skewed := make([]byte, 300000)
	for i := range skewed {
		switch r := rng.IntN(100000); {
		case r < 40:
			skewed[i] = byte(r)
		case r < 30000:
			skewed[i] = byte('a' + rng.IntN(26))
		default:
			skewed[i] = 'x'
		}
	}
	cases := map[string][]byte{
		"one byte":      {0},
		"window":        random(n),
		"window plus f": random(n + f + 1),
		"random 100k":   random(100000),
		"text 180k":     []byte(text),
		"spaces":        bytes.Repeat([]byte{' '}, 5000),
		"skewed 300k":   skewed,
		"all bytes": func() []byte {
			b := make([]byte, 256*40)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
		"long runs": bytes.Repeat(append(bytes.Repeat([]byte{'A'}, 200), 'B'), 300),
	}
	for name, data := range cases {
		roundTrip(t, name, data)
	}
}

// A tree with codes over 16 bits, which LZHUF.C would cut short, built
// directly. The 314 leaves start at count 1, so together they weigh 314.
// Each new count is one more than the running total two merges back, so
// Huffman merges it with the whole tree so far, one level per symbol (the
// Fibonacci worst case). Text rarely gets there, so this drives the tree by
// hand.
func TestLongCodes(t *testing.T) {
	h := newHuff()
	counts := []int{200}
	before, total := 314, 514
	for len(counts) < 9 {
		c := before + 1
		counts = append(counts, c)
		before, total = total, total+c
	}
	for sym, c := range counts {
		for i := 0; i < c; i++ {
			h.update(sym)
		}
	}
	depth := func(h *huff, c int) int {
		bits := 0
		for k := h.prnt[c+huffSize]; k != root; k = h.prnt[k] {
			bits++
		}
		return bits
	}
	const rare = 300 // a match-length symbol never used, still at count 1
	if d := depth(h, rare); d <= 16 {
		t.Fatalf("symbol %d is %d bits deep; the test needs over 16", rare, d)
	}

	// Encode some symbols, deep and shallow, from a copy of the tree, and
	// decode them by walking another copy.
	syms := []int{rare, 9, rare + 1, 0, rare}
	enc := newEncoder()
	*enc.h = *h
	for _, c := range syms {
		enc.encodeChar(c)
	}
	if enc.putlen > 0 {
		enc.out = append(enc.out, byte(enc.putbuf>>8))
	}
	dec := new(huff)
	*dec = *h
	br := &bitReader{b: enc.out}
	for i, want := range syms {
		c := dec.son[root]
		for c < huffSize {
			c = dec.son[c+br.bit()]
		}
		c -= huffSize
		dec.update(c)
		if br.fail || c != want {
			t.Fatalf("symbol %d: decoded %d (ran out: %v), want %d", i, c, br.fail, want)
		}
	}
}

func TestCorrupt(t *testing.T) {
	good := CompressB2([]byte(strings.Repeat("Winlink over the TH-D75. ", 40)))

	bad := bytes.Clone(good)
	bad[10] ^= 0x40
	if _, err := DecompressB2(bad, testLimit); !errors.Is(err, ErrCorrupt) {
		t.Errorf("flipped bit: got %v, want ErrCorrupt", err)
	}
	if _, err := DecompressB2(good[:len(good)-5], testLimit); !errors.Is(err, ErrCorrupt) {
		t.Errorf("cut short: got %v, want ErrCorrupt", err)
	}
	// Cut short, with the CRC recomputed so only the length gives it away.
	short := Compress([]byte(strings.Repeat("abc", 1000)))
	short = short[:len(short)-3]
	if _, err := Decompress(short, testLimit); !errors.Is(err, ErrCorrupt) {
		t.Errorf("cut short without CRC: got %v, want ErrCorrupt", err)
	}
	if _, err := DecompressB2(good, 100); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the limit: got %v, want ErrTooLarge", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := DecompressB2(good[:i], testLimit); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%d bytes: got %v, want ErrCorrupt", i, err)
		}
	}
}

// Compressed data arrives off the air and from the internet.
func FuzzDecompress(f *testing.F) {
	for _, v := range vectors {
		f.Add(v.b2[2:])
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		out, err := Decompress(b, 1<<16)
		if err == nil && len(out) != int(uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16|uint32(b[3])<<24) {
			t.Errorf("decoded %d bytes, header says otherwise", len(out))
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	for _, v := range vectors {
		f.Add([]byte(v.plain))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := DecompressB2(CompressB2(data), len(data))
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("round trip of %d bytes: %v", len(data), err)
		}
	})
}
