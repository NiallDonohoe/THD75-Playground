// Command d75ft8 decodes FT8 from the TH-D75's USB audio. See docs/FT8.md.
//
// So far it works on files: "decode" reads WAV recordings of 15-second
// slots (such as the ones WSJT-X saves) and prints what it decodes, and
// "encode" writes a WAV of a message, which WSJT-X can decode as a check
// on this program's encoder. The page and the radio arrive in milestones
// F1 and F2.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"d75scope/dsp"
	"d75scope/ft8"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "d75ft8:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  d75ft8 decode [flags] FILE.wav...   decode 15-second recordings
  d75ft8 encode [flags] "MESSAGE"     write a WAV of one transmission

Run "d75ft8 decode -h" or "d75ft8 encode -h" for the flags.
`)
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command")
	}
	switch args[0] {
	case "decode":
		return decode(args[1:])
	case "encode":
		return encode(args[1:])
	case "-h", "-help", "--help", "help":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("unknown command %q", args[0])
}

// WSJT-X names its recordings YYMMDD_HHMMSS.wav.
var slotName = regexp.MustCompile(`(\d{6})_(\d{6})\.wav$`)

func decode(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	minFreq := fs.Float64("min-freq", 100, "lowest audio frequency searched, Hz")
	maxFreq := fs.Float64("max-freq", 3000, "highest audio frequency searched, Hz")
	passes := fs.Int("passes", 3, "decoding passes (each subtracts what the last decoded)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75ft8 decode [flags] FILE.wav...\n\n"+
			"Each file holds one slot, starting at the slot boundary, at 12 kHz or a\n"+
			"multiple of it. Output follows WSJT-X: UTC, SNR (dB), DT (s), audio\n"+
			"frequency (Hz) and the message.\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("no files")
	}
	dec := &ft8.Decoder{MinFreq: *minFreq, MaxFreq: *maxFreq, Passes: *passes, Calls: ft8.NewCallsigns()}
	for _, path := range fs.Args() {
		samples, format, err := dsp.ReadWAVFile(path)
		if err != nil {
			return err
		}
		audio, err := ft8.To12k(samples, format.SampleRate, format.Channels)
		if err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
		utc := "000000"
		if m := slotName.FindStringSubmatch(filepath.Base(path)); m != nil {
			utc = m[2]
		}
		if fs.NArg() > 1 {
			fmt.Printf("# %s\n", path)
		}
		for _, d := range dec.Decode(audio) {
			fmt.Printf("%s %3d %4.1f %4.0f ~  %s\n", utc, d.SNR, d.DT, d.Freq, d.Text)
		}
	}
	return nil
}

func encode(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	freq := fs.Float64("freq", 1500, "audio frequency of tone 0, Hz")
	dt := fs.Float64("dt", 0, "start offset from the nominal 0.5 s, seconds")
	out := fs.String("out", "ft8.wav", "WAV file to write (12 kHz, 16-bit mono, one 15 s slot)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: d75ft8 encode [flags] \"MESSAGE\"\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("give the message as one argument, in quotes")
	}
	tones, err := ft8.Encode(fs.Arg(0))
	if err != nil {
		return err
	}
	if *freq < 100 || *freq > 4000 || *dt < -0.5 || *dt > 2 {
		return errors.New("--freq must be 100–4000 Hz and --dt −0.5 to 2 s")
	}
	slot := make([]float32, ft8.SlotSamples)
	sig := ft8.Synthesize(tones, *freq, 0.5)
	copy(slot[ft8.StartSamples+int(*dt*ft8.SampleRate):], sig)
	w, err := dsp.CreateWAV(*out, dsp.WAVFormat{SampleRate: ft8.SampleRate, Channels: 1, BitsPerSample: 16})
	if err != nil {
		return err
	}
	if err := w.Write(slot); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
