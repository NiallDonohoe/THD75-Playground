package dsp

import "math"

// ChannelLevel summarises one channel of a capture.
type ChannelLevel struct {
	PeakDBFS     float64 `json:"peak_dbfs"`
	RMSDBFS      float64 `json:"rms_dbfs"`
	DCOffset     float64 `json:"dc_offset"`
	ZeroFraction float64 `json:"zero_fraction"` // samples exactly 0
	ClippedCount int     `json:"clipped_count"` // |x| ≥ 0.999
}

// Levels measures each channel of interleaved samples.
func Levels(x []float32, channels int) []ChannelLevel {
	out := make([]ChannelLevel, channels)
	frames := len(x) / channels
	if frames == 0 {
		for c := range out {
			out[c] = ChannelLevel{PeakDBFS: -200, RMSDBFS: -200, ZeroFraction: 1}
		}
		return out
	}
	for c := 0; c < channels; c++ {
		var peak, sum, sum2 float64
		zeros, clipped := 0, 0
		for i := c; i < frames*channels; i += channels {
			v := float64(x[i])
			a := math.Abs(v)
			peak = math.Max(peak, a)
			sum += v
			sum2 += v * v
			if v == 0 {
				zeros++
			}
			if a >= 0.999 {
				clipped++
			}
		}
		n := float64(frames)
		out[c] = ChannelLevel{
			PeakDBFS:     AmplitudeDB(peak),
			RMSDBFS:      PowerDB(sum2 / n),
			DCOffset:     sum / n,
			ZeroFraction: float64(zeros) / n,
			ClippedCount: clipped,
		}
	}
	return out
}

// ChannelComparison says how two channels of a capture relate.
type ChannelComparison struct {
	Identical   bool    `json:"identical"`
	MaxAbsDiff  float64 `json:"max_abs_diff"`
	Correlation float64 `json:"correlation"` // Pearson, −1..1
}

// CompareChannels compares channels a and b of interleaved samples. Two
// channels that differ may be I and Q.
func CompareChannels(x []float32, channels, a, b int) ChannelComparison {
	frames := len(x) / channels
	var maxDiff, sa, sb, saa, sbb, sab float64
	for f := 0; f < frames; f++ {
		va, vb := float64(x[f*channels+a]), float64(x[f*channels+b])
		maxDiff = math.Max(maxDiff, math.Abs(va-vb))
		sa += va
		sb += vb
		saa += va * va
		sbb += vb * vb
		sab += va * vb
	}
	n := float64(frames)
	cmp := ChannelComparison{Identical: maxDiff <= 1e-6, MaxAbsDiff: maxDiff}
	if n > 0 {
		cov := sab/n - sa/n*sb/n
		den := math.Sqrt((saa/n - sa/n*sa/n) * (sbb/n - sb/n*sb/n))
		if den > 0 {
			cmp.Correlation = cov / den
		} else if cmp.Identical {
			cmp.Correlation = 1
		}
	}
	return cmp
}

// Channel extracts one channel of interleaved samples as float64.
func Channel(x []float32, channels, c int) []float64 {
	out := make([]float64, len(x)/channels)
	for i := range out {
		out[i] = float64(x[i*channels+c])
	}
	return out
}
