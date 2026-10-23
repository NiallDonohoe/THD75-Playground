package dsp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// WAVFormat describes a WAV file's sample format.
type WAVFormat struct {
	SampleRate    int  `json:"sample_rate"`
	Channels      int  `json:"channels"`
	BitsPerSample int  `json:"bits_per_sample"`
	Float         bool `json:"float"`
}

// WAVWriter streams interleaved float32 samples to a WAV file. Samples are
// stored as 32-bit IEEE float (exactly what was captured) unless the format
// asks for 16- or 24-bit PCM.
type WAVWriter struct {
	f      *os.File
	format WAVFormat
	frames int64
	buf    []byte
}

// CreateWAV creates path and writes a header; Close fixes up the sizes.
func CreateWAV(path string, format WAVFormat) (*WAVWriter, error) {
	if format.Channels < 1 || format.SampleRate < 1 {
		return nil, fmt.Errorf("bad WAV format %+v", format)
	}
	if format.BitsPerSample == 0 {
		format.BitsPerSample, format.Float = 32, true
	}
	if format.Float && format.BitsPerSample != 32 {
		return nil, errors.New("float WAV must be 32-bit")
	}
	if !format.Float && format.BitsPerSample != 16 && format.BitsPerSample != 24 {
		return nil, errors.New("PCM WAV must be 16- or 24-bit")
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &WAVWriter{f: f, format: format}
	if err := w.writeHeader(); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *WAVWriter) writeHeader() error {
	fm := w.format
	blockAlign := fm.Channels * fm.BitsPerSample / 8
	dataBytes := uint32(w.frames) * uint32(blockAlign)
	formatTag := uint16(1)
	if fm.Float {
		formatTag = 3
	}
	// RIFF header, a 16-byte fmt chunk, a fact chunk (required for float),
	// then data.
	h := make([]byte, 0, 56)
	le := binary.LittleEndian
	h = append(h, "RIFF"...)
	h = le.AppendUint32(h, 4+(8+16)+(8+4)+8+dataBytes)
	h = append(h, "WAVE"...)
	h = append(h, "fmt "...)
	h = le.AppendUint32(h, 16)
	h = le.AppendUint16(h, formatTag)
	h = le.AppendUint16(h, uint16(fm.Channels))
	h = le.AppendUint32(h, uint32(fm.SampleRate))
	h = le.AppendUint32(h, uint32(fm.SampleRate*blockAlign))
	h = le.AppendUint16(h, uint16(blockAlign))
	h = le.AppendUint16(h, uint16(fm.BitsPerSample))
	h = append(h, "fact"...)
	h = le.AppendUint32(h, 4)
	h = le.AppendUint32(h, uint32(w.frames))
	h = append(h, "data"...)
	h = le.AppendUint32(h, dataBytes)
	_, err := w.f.WriteAt(h, 0)
	return err
}

const wavHeaderSize = 56

// Write appends interleaved samples (a whole number of frames).
func (w *WAVWriter) Write(samples []float32) error {
	ch := w.format.Channels
	if len(samples)%ch != 0 {
		return fmt.Errorf("%d samples is not a whole number of %d-channel frames", len(samples), ch)
	}
	bps := w.format.BitsPerSample / 8
	need := len(samples) * bps
	if cap(w.buf) < need {
		w.buf = make([]byte, need)
	}
	b := w.buf[:need]
	for i, s := range samples {
		o := b[i*bps:]
		switch {
		case w.format.Float:
			binary.LittleEndian.PutUint32(o, math.Float32bits(s))
		case bps == 2:
			binary.LittleEndian.PutUint16(o, uint16(int16(quantise(s, 32767))))
		default:
			v := quantise(s, 8388607)
			o[0], o[1], o[2] = byte(v), byte(v>>8), byte(v>>16)
		}
	}
	off := int64(wavHeaderSize) + w.frames*int64(ch*bps)
	if _, err := w.f.WriteAt(b, off); err != nil {
		return err
	}
	w.frames += int64(len(samples) / ch)
	return nil
}

func quantise(s float32, full float64) int32 {
	v := math.Round(float64(s) * full)
	return int32(math.Max(-full-1, math.Min(full, v)))
}

// Frames returns the number of frames written.
func (w *WAVWriter) Frames() int64 { return w.frames }

// Close writes the final sizes and closes the file.
func (w *WAVWriter) Close() error {
	err := w.writeHeader()
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// WAVReader reads a WAV file's samples as interleaved float32.
type WAVReader struct {
	r         io.ReadSeeker
	format    WAVFormat
	dataStart int64
	dataLen   int64
	pos       int64 // bytes into data
	closer    io.Closer
}

// OpenWAV opens a WAV file for reading.
func OpenWAV(path string) (*WAVReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := NewWAVReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.closer = f
	return r, nil
}

// NewWAVReader parses the header of a WAV stream. It accepts 16-, 24- and
// 32-bit PCM, 32-bit float and WAVE_FORMAT_EXTENSIBLE wrappers of those.
func NewWAVReader(r io.ReadSeeker) (*WAVReader, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	w := &WAVReader{r: r}
	gotFmt := false
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return nil, errors.New("no data chunk")
		}
		id, size := string(ch[0:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		switch id {
		case "fmt ":
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, err
			}
			if size < 16 {
				return nil, errors.New("short fmt chunk")
			}
			tag := binary.LittleEndian.Uint16(b[0:2])
			w.format.Channels = int(binary.LittleEndian.Uint16(b[2:4]))
			w.format.SampleRate = int(binary.LittleEndian.Uint32(b[4:8]))
			w.format.BitsPerSample = int(binary.LittleEndian.Uint16(b[14:16]))
			if tag == 0xFFFE && size >= 26 {
				tag = binary.LittleEndian.Uint16(b[24:26]) // sub-format GUID's first two bytes
			}
			switch tag {
			case 1:
			case 3:
				w.format.Float = true
			default:
				return nil, fmt.Errorf("unsupported WAV format tag %#x", tag)
			}
			gotFmt = true
			if size%2 == 1 {
				r.Seek(1, io.SeekCurrent)
			}
		case "data":
			if !gotFmt {
				return nil, errors.New("data before fmt")
			}
			pos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, err
			}
			w.dataStart, w.dataLen = pos, size
			return w, nil
		default:
			if _, err := r.Seek(size+size%2, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
}

// Format returns the file's format.
func (w *WAVReader) Format() WAVFormat { return w.format }

// Frames returns the number of frames in the file.
func (w *WAVReader) Frames() int64 {
	return w.dataLen / int64(w.format.Channels*w.format.BitsPerSample/8)
}

// Read fills out with interleaved samples and returns how many it read.
// It returns io.EOF at the end.
func (w *WAVReader) Read(out []float32) (int, error) {
	bps := w.format.BitsPerSample / 8
	remaining := (w.dataLen - w.pos) / int64(bps)
	if remaining <= 0 {
		return 0, io.EOF
	}
	n := len(out)
	if int64(n) > remaining {
		n = int(remaining)
	}
	b := make([]byte, n*bps)
	got, err := io.ReadFull(w.r, b)
	n = got / bps
	w.pos += int64(n * bps)
	for i := 0; i < n; i++ {
		o := b[i*bps:]
		switch {
		case w.format.Float && bps == 4:
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(o))
		case bps == 2:
			out[i] = float32(int16(binary.LittleEndian.Uint16(o))) / 32768
		case bps == 3:
			v := int32(o[0]) | int32(o[1])<<8 | int32(int8(o[2]))<<16
			out[i] = float32(v) / 8388608
		case bps == 4:
			out[i] = float32(float64(int32(binary.LittleEndian.Uint32(o))) / 2147483648)
		}
	}
	if err == io.ErrUnexpectedEOF {
		err = nil
	}
	return n, err
}

// SeekFrame moves to a frame.
func (w *WAVReader) SeekFrame(frame int64) error {
	bpf := int64(w.format.Channels * w.format.BitsPerSample / 8)
	off := frame * bpf
	if off < 0 || off > w.dataLen {
		return fmt.Errorf("frame %d out of range", frame)
	}
	if _, err := w.r.Seek(w.dataStart+off, io.SeekStart); err != nil {
		return err
	}
	w.pos = off
	return nil
}

// Close closes the underlying file, if OpenWAV opened it.
func (w *WAVReader) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

// ReadWAVFile reads a whole WAV file into memory.
func ReadWAVFile(path string) ([]float32, WAVFormat, error) {
	r, err := OpenWAV(path)
	if err != nil {
		return nil, WAVFormat{}, err
	}
	defer r.Close()
	out := make([]float32, r.Frames()*int64(r.Format().Channels))
	n, err := r.Read(out)
	if err != nil && err != io.EOF {
		return nil, WAVFormat{}, err
	}
	return out[:n], r.Format(), nil
}
