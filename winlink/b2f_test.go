package winlink

import (
	"bufio"
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Vectors from Pat (wl2k-go fbb/secure_test.go): the published algorithm
// against its own implementation, P01 in docs/WINLINK.md.
func TestSecureLoginResponse(t *testing.T) {
	for _, v := range []struct{ challenge, password, want string }{
		{"23753528", "FOOBAR", "72768415"},
		{"23753528", "FooBar", "95074758"},
	} {
		if got := SecureLoginResponse(v.challenge, v.password); got != v.want {
			t.Errorf("SecureLoginResponse(%q, %q) = %q, want %q", v.challenge, v.password, got, v.want)
		}
	}
}

func TestSID(t *testing.T) {
	if got := formatSID("d75winlink", "0.1.0"); got != "[d75winlink-0.1.0-B2FHM$]" {
		t.Errorf("formatSID = %q", got)
	}
	for line, want := range map[string]string{
		"[WL2K-5.0-B2FWIHJM$]":          "B2FWIHJM$",
		"[RMS Express-1.7.12.0-B2FHM$]": "B2FHM$",
		"[pat-0.16.0-b2fhm$]":           "B2FHM$",
	} {
		if got, ok := sidFlags(line); !ok || got != want {
			t.Errorf("sidFlags(%q) = %q, %v", line, got, ok)
		}
	}
	for _, line := range []string{"WL2K-5.0-B2F", "[NOFLAGS]", "CMS >"} {
		if _, ok := sidFlags(line); ok {
			t.Errorf("sidFlags(%q) accepted", line)
		}
	}
}

func TestParseProposal(t *testing.T) {
	// From Pat's fbb/proposal_test.go.
	p, code, err := parseProposal("FC EM TJKYEIMMHSRB 527 123 0")
	if err != nil || code != 'C' || p != (Proposal{MID: "TJKYEIMMHSRB", Size: 527, CompressedSize: 123}) {
		t.Errorf("parseProposal = %+v, %c, %v", p, code, err)
	}
	if p.line() != "FC EM TJKYEIMMHSRB 527 123 0" {
		t.Errorf("line() = %q", p.line())
	}
	if _, code, err := parseProposal("FB P Q1ABC Q2XYZ 12345_Q1ABC 300"); err != nil || code != 'B' {
		t.Errorf("FB proposal: %c, %v", code, err)
	}
	for _, bad := range []string{"FC", "FC EM", "FC XX MID 1 10 0", "FC EM MID x 10 0", "FC EM MID 1 2 0", "FC EM TOOLONGMIDXXXX 1 10 0", "FCEM MID 1 10 0"} {
		if _, _, err := parseProposal(bad); !errors.Is(err, ErrProtocol) {
			t.Errorf("parseProposal(%q): %v, want ErrProtocol", bad, err)
		}
	}
}

func TestParseAnswers(t *testing.T) {
	// From Pat's fbb/b2f_test.go.
	for line, want := range map[string]struct {
		answers string
		offsets []int
	}{
		"FS YLA3350RH": {"+=+-=", []int{0, 0, 3350, 0, 0}},
		"FS +=!3350-+": {"+=+-+", []int{0, 0, 3350, 0, 0}},
	} {
		answers, offsets, err := parseAnswers(line, 5)
		if err != nil || string(answers) != want.answers || !reflect.DeepEqual(offsets, want.offsets) {
			t.Errorf("parseAnswers(%q) = %q, %v, %v", line, answers, offsets, err)
		}
	}
	for _, bad := range []string{"FS ++", "FS +++++-", "FS +?+++", "FS !+++++", "F> 00"} {
		if _, _, err := parseAnswers(bad, 5); !errors.Is(err, ErrProtocol) {
			t.Errorf("parseAnswers(%q): %v, want ErrProtocol", bad, err)
		}
	}
}

func TestProposalChecksum(t *testing.T) {
	// Worked by hand: "FC EM A 1 6 0" sums to 659, plus CR (13) is 672,
	// 672 mod 256 is 160 (0xA0), and the F> value is -0xA0 = 0x60.
	if sum := proposalChecksum(0, "FC EM A 1 6 0"); -sum != 0x60 {
		t.Errorf("checksum = %02X, want 60", -sum)
	}
}

func TestBlocksRoundTrip(t *testing.T) {
	for _, size := range []int{6, 250, 251, 500, 1234} {
		data := bytes.Repeat([]byte{0xa5, 0x01, 0xfe}, size)[:size]
		var buf bytes.Buffer
		if err := writeBlocks(&buf, "Grüße", 0, data); err != nil {
			t.Fatal(err)
		}
		title, got, err := readBlocks(bufio.NewReader(&buf), size)
		if err != nil || title != "Grüße" || !bytes.Equal(got, data) {
			t.Errorf("size %d: title %q, %d bytes, %v", size, title, len(got), err)
		}
	}
}

func TestReadBlocks(t *testing.T) {
	block := func(n int, fill byte) []byte {
		k := byte(n)
		return append([]byte{stx, k}, bytes.Repeat([]byte{fill}, n)...)
	}
	header := []byte("\x01\x08Title\x000\x00")
	// An STX length of 0 means 256 bytes.
	stream := append(append([]byte{}, header...), block(256, 1)...)
	stream = append(stream, eot, 0)
	if _, got, err := readBlocks(bufio.NewReader(bytes.NewReader(stream)), 256); err != nil || len(got) != 256 {
		t.Errorf("256-byte block: %d bytes, %v", len(got), err)
	}

	bad := map[string][]byte{
		"bad checksum": append(append(append([]byte{}, header...), block(3, 1)...), eot, 0),
		"too long":     append(append(append([]byte{}, header...), block(9, 1)...), eot, 0xf7),
		"no SOH":       {stx, 1, 0, eot, 0},
		"offset":       append([]byte("\x01\x08Title\x005\x00"), eot, 0),
		"junk":         append(append([]byte{}, header...), 'x'),
	}
	for name, s := range bad {
		if _, _, err := readBlocks(bufio.NewReader(bytes.NewReader(s)), 4); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %v, want ErrProtocol", name, err)
		}
	}
	_, _, err := readBlocks(bufio.NewReader(strings.NewReader("*** Message too large\r")), 4)
	var re *RemoteError
	if !errors.As(err, &re) || re.Text != "Message too large" {
		t.Errorf("remote error: %v", err)
	}
}

func TestReadLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("one\r\ntwo\r\r  three  \r"))
	for _, want := range []string{"one", "two", "", "three"} {
		if got, err := readLine(r); err != nil || got != want {
			t.Errorf("readLine = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := readLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", 2000) + "\r"))); !errors.Is(err, ErrProtocol) {
		t.Errorf("long line: %v", err)
	}
}

func FuzzReadBlocks(f *testing.F) {
	var buf bytes.Buffer
	writeBlocks(&buf, "t", 0, []byte("abcdefgh"))
	f.Add(buf.Bytes(), 8)
	f.Fuzz(func(t *testing.T, b []byte, size int) {
		if size < 0 || size > 1<<16 {
			return
		}
		_, data, err := readBlocks(bufio.NewReader(bytes.NewReader(b)), size)
		if err == nil && len(data) != size {
			t.Errorf("returned %d bytes for size %d", len(data), size)
		}
	})
}
