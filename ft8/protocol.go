package ft8

// Timing and modulation. Everything runs at 12 kHz, as WSJT-X does.
const (
	SampleRate    = 12000
	SymbolSamples = 1920 // 0.16 s
	ToneSpacing   = 6.25 // Hz, 1/0.16 s
	NumSymbols    = 79
	SlotSeconds   = 15
	SlotSamples   = SlotSeconds * SampleRate
	// StartSamples is where a transmission nominally starts within its
	// slot (0.5 s). DT is measured from here.
	StartSamples = SampleRate / 2
	// SignalSamples is the length of one transmission (12.64 s).
	SignalSamples = NumSymbols * SymbolSamples
	// Bandwidth is the spectrum one signal occupies above its tone 0.
	Bandwidth = 8 * ToneSpacing
)

// Code sizes.
const (
	payloadBits = 77
	crcBits     = 14
	ldpcK       = payloadBits + crcBits // 91
	ldpcN       = 174
	ldpcM       = ldpcN - ldpcK // 83
	numData     = ldpcN / 3     // 58 data symbols
)

// costas is the 7×7 Costas array sent at symbols 0, 36 and 72.
var costas = [7]int{3, 1, 4, 0, 6, 5, 2}

// costasStart are the first symbols of the three sync blocks.
var costasStart = [3]int{0, 36, 72}

// grayMap turns three code bits (MSB first) into a tone.
var grayMap = [8]int{0, 1, 3, 2, 5, 6, 4, 7}

// toneBits is the inverse of grayMap: the three bits a tone carries.
var toneBits [8]int

func init() {
	for v, t := range grayMap {
		toneBits[t] = v
	}
}

// dataSymbol returns the position of data symbol i (0–57) among the 79.
func dataSymbol(i int) int {
	if i < 29 {
		return 7 + i
	}
	return 43 + i - 29
}
