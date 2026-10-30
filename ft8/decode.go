package ft8

import (
	"fmt"
	"math"
	"math/cmplx"
	"sort"
	"sync"

	"d75scope/dsp"
)

// Decode is one decoded transmission.
type Decode struct {
	Text    string  // as WSJT-X prints it
	SNR     int     // dB in 2500 Hz, as WSJT-X reports it (−24 at the least)
	DT      float64 // seconds from the nominal start, 0.5 s into the slot
	Freq    float64 // Hz of tone 0 in the audio
	Payload Payload
	Tones   [NumSymbols]int
	Pass    int     // decoding pass that found it, from 1
	Sync    float64 // coarse Costas score, for diagnostics
	Err     error   // ErrUnsupported when the type is not unpacked
	start   int     // sample of symbol 0, for subtraction
}

// Decoder decodes one 15-second slot at a time. The zero value is usable
// and decodes 100–3000 Hz in three passes.
type Decoder struct {
	MinFreq, MaxFreq float64    // audio range searched for tone 0, Hz
	MaxCandidates    int        // per pass
	Passes           int        // subtract-and-retry passes
	Calls            *Callsigns // optional, resolves hashed callsigns
}

// Spectrogram used by the coarse search: half-symbol hops and 3.125 Hz bins
// (a symbol of Hann-windowed audio zero-padded to two), so every tone falls
// on an even bin.
const (
	specHop = SymbolSamples / 2
	specN   = 2 * SymbolSamples
	binHz   = float64(SampleRate) / specN // 3.125
)

// The fine stage works on a narrow complex baseband copy of each
// candidate, cut from one large FFT of the whole slot: 4096 of its 2^18
// bins give 187.5 samples a second, exactly 30 a symbol.
const (
	bigN   = 1 << 18
	bbN    = 4096
	bbStep = bigN / bbN // 64 audio samples per baseband sample
	bbRate = float64(SampleRate) / bbStep
	bbSym  = SymbolSamples / bbStep // 30
)

// Search ranges, in spectrogram frames (half symbols) for the start of
// symbol 0: −10 is DT −1.3 s, 32 is DT +2.06 s.
const (
	minStartFrame = -10
	maxStartFrame = 32
)

func (d *Decoder) defaults() (lo, hi float64, maxCand, passes int) {
	lo, hi, maxCand, passes = d.MinFreq, d.MaxFreq, d.MaxCandidates, d.Passes
	if lo <= 0 {
		lo = 100
	}
	if hi <= 0 || hi > 5000 {
		hi = 3000
	}
	if maxCand <= 0 {
		maxCand = 300
	}
	if passes <= 0 {
		passes = 3
	}
	return
}

// Decode decodes a slot of 12 kHz audio whose first sample is the start of
// the slot. Shorter input is padded with silence and longer is cut at 15 s.
// Results are in order of frequency.
func (d *Decoder) Decode(audio []float32) []Decode {
	lo, hi, maxCand, passes := d.defaults()
	x := make([]float64, SlotSamples)
	for i := 0; i < len(audio) && i < SlotSamples; i++ {
		x[i] = float64(audio[i])
	}
	var out []Decode
	seen := map[Payload]bool{}
	for pass := 1; pass <= passes; pass++ {
		spec := newSpectrogram(x, int(hi/binHz)+20)
		cands := spec.candidates(int(lo/binHz), int(hi/binHz), maxCand)
		big := bigSpectrum(x)
		found := d.tryAll(big, cands, pass, seen)
		if len(found) == 0 {
			break
		}
		for i := range found {
			d := &found[i]
			d.start = subtract(x, &d.Tones, d.Freq, d.start)
			d.DT = float64(d.start-StartSamples) / SampleRate
		}
		out = append(out, found...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Freq < out[j].Freq })
	return out
}

// tryAll runs the fine stage on every candidate, in parallel, and keeps
// the first decode of each payload.
func (d *Decoder) tryAll(big []complex128, cands []candidate, pass int, seen map[Payload]bool) []Decode {
	results := make([]*Decode, len(cands))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := newFine()
			for i := range next {
				results[i] = f.try(big, cands[i])
			}
		}()
	}
	for i := range cands {
		next <- i
	}
	close(next)
	wg.Wait()
	var found []Decode
	for _, r := range results {
		if r == nil || seen[r.Payload] {
			continue
		}
		seen[r.Payload] = true
		r.Pass = pass
		r.Text, r.Err = unpack(&r.Payload, d.Calls)
		if r.Err != nil && r.Err != ErrUnsupported {
			continue
		}
		if r.Err != nil {
			i3, n3 := r.Payload.Type()
			r.Text = unsupportedText(i3, n3)
		}
		found = append(found, *r)
	}
	return found
}

func unsupportedText(i3, n3 int) string {
	if i3 == 0 {
		return fmt.Sprintf("[message type 0.%d]", n3)
	}
	return fmt.Sprintf("[message type %d]", i3)
}

type spectrogram struct {
	frames, bins int
	p            [][]float32 // power, [frame][bin]
}

func newSpectrogram(x []float64, bins int) *spectrogram {
	dft, _ := dsp.NewDFT(specN)
	win := make([]float64, SymbolSamples)
	for i := range win {
		win[i] = 0.5 - 0.5*math.Cos(2*math.Pi*(float64(i)+0.5)/SymbolSamples)
	}
	frames := (len(x)-SymbolSamples)/specHop + 1
	s := &spectrogram{frames: frames, bins: bins, p: make([][]float32, frames)}
	buf := make([]complex128, specN)
	for f := range s.p {
		for i := range buf {
			buf[i] = 0
		}
		for i, w := range win {
			buf[i] = complex(x[f*specHop+i]*w, 0)
		}
		dft.Transform(buf)
		row := make([]float32, bins)
		for b := range row {
			v := buf[b]
			row[b] = float32(real(v)*real(v) + imag(v)*imag(v))
		}
		s.p[f] = row
	}
	return s
}

type candidate struct {
	frame int // spectrogram frame where symbol 0 starts
	bin   int // bin of tone 0
	score float64
}

// costasScore compares the power in each Costas tone with the average of
// the other seven tones at that symbol, over the sync blocks in blocks.
func (s *spectrogram) costasScore(frame, bin int, blocks []int) (float64, bool) {
	var sig, tot float64
	used := 0
	for _, blk := range blocks {
		start := frame + 2*costasStart[blk]
		if start < 0 || start+12 >= s.frames {
			continue
		}
		for k, t := range costas {
			row := s.p[start+2*k]
			for tone := 0; tone < 8; tone++ {
				tot += float64(row[bin+2*tone])
			}
			sig += float64(row[bin+2*t])
		}
		used++
	}
	if used < 2 || tot <= sig {
		return 0, false
	}
	return sig / ((tot - sig) / 7), true
}

// candidates returns the strongest Costas matches with tone 0 in bins
// lo..hi: the best start time for each bin, kept where it is a local
// maximum across neighbouring bins and stands out from the band's typical
// score.
func (s *spectrogram) candidates(lo, hi, max int) []candidate {
	if hi+16 > s.bins {
		hi = s.bins - 16
	}
	if lo < 1 {
		lo = 1
	}
	best := make([]candidate, hi+2)
	for b := lo; b <= hi; b++ {
		best[b] = candidate{bin: b}
		for f := minStartFrame; f <= maxStartFrame; f++ {
			for _, blocks := range [][]int{{0, 1, 2}, {1, 2}, {0, 1}} {
				if sc, ok := s.costasScore(f, b, blocks); ok && sc > best[b].score {
					best[b] = candidate{frame: f, bin: b, score: sc}
				}
			}
		}
	}
	scores := make([]float64, 0, hi-lo+1)
	for b := lo; b <= hi; b++ {
		scores = append(scores, best[b].score)
	}
	sort.Float64s(scores)
	base := scores[len(scores)*4/10]
	if base <= 0 {
		base = 1
	}
	var out []candidate
	for b := lo; b <= hi; b++ {
		c := best[b]
		c.score /= base
		if c.score < 1.3 || best[b].score < best[b-1].score || best[b].score < best[b+1].score {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// bigSpectrum is the FFT of the whole slot, zero-padded to 2^18.
func bigSpectrum(x []float64) []complex128 {
	fft, _ := dsp.NewFFT(bigN)
	buf := make([]complex128, bigN)
	for i, v := range x {
		buf[i] = complex(v, 0)
	}
	fft.Transform(buf)
	return buf
}

// fine holds one worker's buffers for the fine stage.
type fine struct {
	fft   *dsp.FFT
	bb    []complex128 // baseband, tone 0 near 0 Hz
	mixed []complex128
	tw    [8][bbSym]complex128 // e^{-j2π tone n/30}
}

func newFine() *fine {
	fft, _ := dsp.NewFFT(bbN)
	f := &fine{fft: fft, bb: make([]complex128, bbN), mixed: make([]complex128, bbN)}
	for t := range f.tw {
		for n := range f.tw[t] {
			f.tw[t][n] = cmplx.Rect(1, -2*math.Pi*float64(t*n)/bbSym)
		}
	}
	return f
}

// baseband cuts the bins around a candidate out of the big spectrum and
// returns them to the time domain, so that tone 0 sits near 0 Hz.
func (f *fine) baseband(big []complex128, freq float64) float64 {
	df := float64(SampleRate) / bigN
	i0 := int(math.Round(freq / df))
	for k := range f.bb {
		o := k
		if k >= bbN/2 {
			o = k - bbN
		}
		hz := float64(o) * df
		// Flat from 1.5 tones below tone 0 to 8.5 tones above it (tone 7
		// plus the fine search), with 6 Hz cosine edges. Anything wider
		// lets a strong neighbour leak into the tones through the
		// sidelobes of the 30-sample symbol DFTs.
		const lo, hi, edge = -1.5 * ToneSpacing, 8.5 * ToneSpacing, 6.0
		w := 0.0
		switch {
		case hz >= lo && hz <= hi:
			w = 1
		case hz > lo-edge && hz < lo:
			w = 0.5 + 0.5*math.Cos(math.Pi*(lo-hz)/edge)
		case hz > hi && hz < hi+edge:
			w = 0.5 + 0.5*math.Cos(math.Pi*(hz-hi)/edge)
		}
		i := i0 + o
		if w == 0 || i <= 0 || i >= bigN/2 {
			f.bb[k] = 0
			continue
		}
		f.bb[k] = cmplx.Conj(big[i]) * complex(w, 0)
	}
	// Inverse FFT by conjugation; the scale does not matter, only ratios.
	f.fft.Transform(f.bb)
	for k := range f.bb {
		f.bb[k] = cmplx.Conj(f.bb[k])
	}
	return float64(i0) * df
}

// mix shifts the baseband by −dHz into f.mixed.
func (f *fine) mix(dHz float64) {
	step := cmplx.Rect(1, -2*math.Pi*dHz/bbRate)
	r := complex(1, 0)
	for k, v := range f.bb {
		if k%256 == 0 { // re-anchor to stop rounding drift
			r = cmplx.Rect(1, -2*math.Pi*dHz*float64(k)/bbRate)
		}
		f.mixed[k] = v * r
		r *= step
	}
}

// symbol returns the complex amplitude of one tone over one symbol of
// f.mixed starting at sample m, or 0 off the ends.
func (f *fine) symbol(m, tone int) complex128 {
	if m < 0 || m+bbSym > bbN {
		return 0
	}
	var acc complex128
	for n, w := range f.tw[tone] {
		acc += f.mixed[m+n] * w
	}
	return acc
}

func (f *fine) syncPower(m0 int) float64 {
	var s float64
	for _, start := range costasStart {
		for k, t := range costas {
			v := f.symbol(m0+(start+k)*bbSym, t)
			s += real(v)*real(v) + imag(v)*imag(v)
		}
	}
	return s
}

// try runs the fine stage on one candidate and returns a decode whose CRC
// checks, or nil.
func (f *fine) try(big []complex128, c candidate) *Decode {
	f0 := f.baseband(big, float64(c.bin)*binHz)
	// Fine search: ±2.5 Hz in 0.25 Hz steps, ±12 baseband samples
	// (±64 ms) around the coarse start.
	m0 := c.frame * specHop / bbStep
	bestP, bestM, bestDF := -1.0, 0, 0.0
	for i := -10; i <= 10; i++ {
		dHz := float64(i) * 0.25
		f.mix(dHz)
		for m := m0 - 12; m <= m0+12; m++ {
			if p := f.syncPower(m); p > bestP {
				bestP, bestM, bestDF = p, m, dHz
			}
		}
	}
	f.mix(bestDF)
	var cs [NumSymbols][8]complex128
	var mag [NumSymbols][8]float64
	for s := range cs {
		for t := 0; t < 8; t++ {
			cs[s][t] = f.symbol(bestM+s*bbSym, t)
			mag[s][t] = cmplx.Abs(cs[s][t])
		}
	}
	// A real signal shows most of its 21 Costas tones as the loudest
	// tone of their symbol; noise rarely shows more than a few.
	hits := 0
	for _, start := range costasStart {
		for k, t := range costas {
			if argmax(&mag[start+k]) == t {
				hits++
			}
		}
	}
	if hits < 7 {
		return nil
	}
	// Try the single-symbol metric first, then two and three symbols
	// combined, which gain on weak signals and lose on fading ones.
	var pl [payloadBits]uint8
	ok := false
	for nsym := 1; nsym <= 3 && !ok; nsym++ {
		llr := bitMetrics(&cs, nsym)
		cw, errs := bpDecode(&llr, 30)
		if errs != 0 {
			continue
		}
		a := [ldpcK]uint8(cw[:ldpcK])
		pl, ok = checkCRC(&a)
		if pl == ([payloadBits]uint8{}) {
			ok = false
		}
	}
	if !ok {
		return nil
	}
	d := &Decode{Payload: Payload(pl), Sync: c.score}
	d.Tones = d.Payload.Tones()
	d.Freq = f0 + bestDF
	d.start = bestM * bbStep
	d.DT = float64(d.start-StartSamples) / SampleRate
	d.SNR = snr(&cs, &d.Tones)
	return d
}

func argmax(v *[8]float64) int {
	best := 0
	for i := range v {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// bitMetrics turns symbol spectra into a log-likelihood ratio per code
// bit (positive means 0). Data symbols are taken nsym at a time (1–3, the
// last group of each half shorter): for every combination of tones the
// complex amplitudes add coherently, since each tone runs a whole number of
// cycles per symbol and FT8 keeps its phase continuous. A bit's metric is
// the best combination with it 1 against the best with it 0. The metrics
// are normalised to unit spread and scaled as WSJT-X scales them.
func bitMetrics(cs *[NumSymbols][8]complex128, nsym int) (llr [ldpcN]float64) {
	var bm [ldpcN]float64
	var amp [512]float64
	for half := 0; half < 2; half++ {
		for k := 0; k < 29; k += nsym {
			g := min(nsym, 29-k)
			first := half*29 + k // data symbol index
			combos := 1 << (3 * g)
			for c := 0; c < combos; c++ {
				var sum complex128
				for j := 0; j < g; j++ {
					t := c >> (3 * (g - 1 - j)) & 7
					sum += cs[dataSymbol(first+j)][grayMap[t]]
				}
				amp[c] = cmplx.Abs(sum)
			}
			for b := 0; b < 3*g; b++ {
				shift := 3*g - 1 - b
				max1, max0 := 0.0, 0.0
				for c := 0; c < combos; c++ {
					if c>>shift&1 == 1 {
						max1 = math.Max(max1, amp[c])
					} else {
						max0 = math.Max(max0, amp[c])
					}
				}
				bm[3*first+b] = max1 - max0
			}
		}
	}
	var sum, sum2 float64
	for _, v := range bm {
		sum += v
		sum2 += v * v
	}
	mean := sum / ldpcN
	sig := math.Sqrt(sum2/ldpcN - mean*mean)
	if sig <= 0 {
		sig = 1
	}
	for i, v := range bm {
		llr[i] = -2.83 * v / sig
	}
	return llr
}

// snr estimates the signal-to-noise ratio in 2500 Hz, the figure WSJT-X
// reports. The noise is the median power of the tones not sent, over all
// 79 symbols (the median of exponentially distributed noise power is ln 2
// times its mean), which shrugs off neighbouring signals in a busy band.
// The signal is the power in the sent tones less that noise. Both are in
// one 6.25 Hz DFT bin, so the ratio is referred to 2500 Hz: −26 dB.
func snr(cs *[NumSymbols][8]complex128, tones *[NumSymbols]int) int {
	var sig float64
	others := make([]float64, 0, NumSymbols*7)
	for s, t := range tones {
		for k, v := range cs[s] {
			p := real(v)*real(v) + imag(v)*imag(v)
			if k == t {
				sig += p
			} else {
				others = append(others, p)
			}
		}
	}
	sort.Float64s(others)
	noise := others[len(others)/2] / math.Ln2
	if noise <= 0 {
		return 30
	}
	ratio := (sig/NumSymbols - noise) / noise
	v := 10*math.Log10(math.Max(ratio, 1e-3)) - 26
	return int(math.Round(math.Max(v, -24)))
}
