package probe

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"d75scope/radio"
)

// Questions are the IDs and text of HARDWARE_QUESTIONS.md, in order.
var Questions = []struct{ ID, Text string }{
	{"Q01", "USB audio format: sample rate, bit depth, channel count"},
	{"Q02", "If stereo, are the two channels identical, or I and Q?"},
	{"Q03", "USB identity: vendor ID, product ID, product string, serial number"},
	{"Q04", "Serial port: device path, a baud rate that works, exclusive lock"},
	{"Q05", "Do the D74 CAT commands (ID FV FO FQ MD IO FT FS SH BY AI) reply as the D74 reference says?"},
	{"Q06", "IO 0: is it AF (as the spec says) or Off (as the D74 reference says)?"},
	{"Q07", "IF centre and bandwidth as delivered over USB"},
	{"Q08", "Is the IF spectrally inverted, per mode (USB, LSB, CW, AM)?"},
	{"Q09", "Where does the dial frequency land in the IF, per mode (Δ_mode)?"},
	{"Q10", "Does the CW pitch (Menu 170) shift the CW IF?"},
	{"Q11", "Does IF output work in FM and NFM, or only SSB, CW and AM?"},
	{"Q12", "Is the IF taken before or after the radio's receive filters (Menus 120–122) and AGC?"},
	{"Q13", "Does the radio's speaker keep playing while IF output is active?"},
	{"Q14", "FT and FS have no band parameter: do they act on band B?"},
	{"Q15", "FQ resolution: does the radio round frequencies, and to what step?"},
	{"Q16", "What do unsolicited AI lines look like?"},
	{"Q17", "What does the Detect output (IO 2) contain?"},
	{"Q18", "Does changing mode (MD) move the dial (FQ)?"},
	{"Q19", "Input level: is Menu 91A set so the IF peaks below −6 dBFS?"},
	{"Q20", "Is NMEA, APRS or KISS traffic arriving on the USB serial port?"},
	{"Q21", "Does capture deliver real samples (microphone permission granted)?"},
}

func (p *prober) answer(id, ans, basis, evidence string) {
	q := ""
	for _, x := range Questions {
		if x.ID == id {
			q = x.Text
		}
	}
	p.rep.Answers = append(p.rep.Answers, Answer{ID: id, Question: q, Answer: ans, Basis: basis, Evidence: evidence})
}

func (p *prober) recording(name string) *Recording {
	for i := range p.rep.Recordings {
		if p.rep.Recordings[i].Name == name {
			return &p.rep.Recordings[i]
		}
	}
	return nil
}

func (p *prober) finish() {
	p.rep.Finished = time.Now().UTC()
	if p.cat != nil {
		p.rep.CAT.Hygiene = p.cat.Session().Stats()
	}
	r := p.rep
	unknown := func(id, why string) { p.answer(id, "not answered: "+why, "unknown", "") }

	// Q01, Q02, Q21: audio.
	if d := r.Audio.Radio; d != nil {
		var fm []string
		for _, f := range d.InputFormats {
			kind := "integer"
			if f.Float {
				kind = "float"
			}
			fm = append(fm, fmt.Sprintf("%g Hz, %d-bit %s, %d channel(s)", f.SampleRate, f.BitsPerChannel, kind, f.Channels))
		}
		p.answer("Q01", fmt.Sprintf("%q: nominal %g Hz, %d input channel(s); physical stream format %s", d.Name, d.NominalRate, d.InputChannels, strings.Join(fm, "; ")),
			"measured", "Core Audio device properties")
	} else {
		unknown("Q01", "no radio audio device found")
	}
	var anyIF *Recording
	for i := range r.Recordings {
		if strings.HasPrefix(r.Recordings[i].Name, "if-") {
			anyIF = &r.Recordings[i]
			break
		}
	}
	sample := anyIF
	if sample == nil {
		sample = p.recording("as-found")
	}
	switch {
	case sample == nil:
		unknown("Q02", "no recording")
		unknown("Q21", "no recording")
	default:
		a := sample.Analysis
		if c := a.Channels; c != nil {
			if c.Identical {
				p.answer("Q02", "identical: the second channel is a copy (treat as mono)", "measured", "recording "+sample.Name)
			} else {
				p.answer("Q02", fmt.Sprintf("DIFFERENT (correlation %.3f, max difference %.3g): may be I and Q — stop and decide before M1", c.Correlation, c.MaxAbsDiff), "measured", "recording "+sample.Name)
				p.warnf("the two audio channels differ; they may be I and Q. The DSP plan assumes a real IF: this needs a decision")
			}
		} else {
			p.answer("Q02", "mono: one channel", "measured", "")
		}
		if a.Silent {
			p.answer("Q21", "NO: every sample was zero. Grant microphone access to the terminal (System Settings → Privacy & Security → Microphone) and run again", "measured", "recording "+sample.Name)
		} else {
			p.answer("Q21", fmt.Sprintf("yes: peak %.1f dBFS, RMS %.1f dBFS", a.Levels[0].PeakDBFS, a.Levels[0].RMSDBFS), "measured", "recording "+sample.Name)
		}
	}

	// Q03, Q04: USB and serial.
	var usb *radio.USBDevice
	for _, pt := range r.Serial.Ports {
		if pt.Path == r.Serial.Port && pt.USB != nil {
			usb = pt.USB
		}
	}
	if usb != nil {
		p.answer("Q03", fmt.Sprintf("vendor 0x%04X (%s), product 0x%04X %q, serial %q", usb.VendorID, usb.Vendor, usb.ProductID, usb.Product, usb.Serial), "measured", "I/O Registry")
	} else {
		unknown("Q03", "the port's USB identity was not found")
	}
	if r.CAT.Responding {
		p.answer("Q04", fmt.Sprintf("%s answers at %d baud; exclusive lock: %s", r.Serial.Port, r.Serial.Baud, r.Serial.ExclusiveLock), "measured", "")
	} else {
		unknown("Q04", "the radio did not answer CAT")
	}

	// Q05: CAT reply formats.
	if len(r.CAT.Reads) > 0 {
		var bad []string
		for _, c := range r.CAT.Reads {
			if c.Error != "" {
				bad = append(bad, c.Command+": "+c.Error)
			}
		}
		if len(bad) == 0 {
			p.answer("Q05", "every read replied and parsed; see cat.reads for the exact replies", "measured", "")
		} else {
			p.answer("Q05", "some reads failed: "+strings.Join(bad, "; "), "measured", "")
		}
	} else {
		unknown("Q05", "no CAT reads")
	}

	// Q06: IO 0.
	if io, ok := r.CAT.State["io"].(string); ok && io == radio.IOAF.String() {
		if a := p.recording("as-found"); a != nil {
			switch {
			case a.Analysis.Silent:
				p.answer("Q06", "IO was 0 and the USB audio was silent: consistent with Off", "measured", "as-found recording")
			case a.Analysis.LowBandFraction > 0.8:
				p.answer("Q06", "IO was 0 and the USB audio looked like audio (energy below 4 kHz): consistent with AF", "inferred", "as-found recording")
			default:
				p.answer("Q06", fmt.Sprintf("IO was 0; USB audio was not silent but not clearly audio (%.0f%% below 4 kHz)", 100*a.Analysis.LowBandFraction), "inferred", "as-found recording")
			}
		} else {
			unknown("Q06", "no as-found recording")
		}
	} else {
		unknown("Q06", "IO was not 0 when the probe started; set Menu 102 to AF and run read-only again")
	}

	// Q07: IF centre and width.
	if a := p.recording("if-usb"); a != nil && a.Analysis.Passband != nil {
		pb := a.Analysis.Passband
		if pb.Shaped {
			p.answer("Q07", fmt.Sprintf("noise passband −6 dB %.0f–%.0f Hz (centre %.0f Hz, width %.0f Hz); −20 dB %.0f–%.0f Hz", pb.Low6Hz, pb.High6Hz, pb.CentreHz, pb.WidthHz, pb.Low20Hz, pb.High20Hz), "measured", "if-usb recording")
		} else {
			p.answer("Q07", "the spectrum was flat to the band edges, so no passband shape could be measured (antenna disconnected? attenuator on?)", "measured", "if-usb recording")
		}
	} else {
		unknown("Q07", "needs --allow-writes")
	}

	// Q08, Q09: inversion and offset.
	if len(r.Inversion) > 0 {
		var parts []string
		for _, inv := range r.Inversion {
			s := "unknown"
			if inv.S == 1 {
				s = "s=+1"
			} else if inv.S == -1 {
				s = "s=−1"
			}
			parts = append(parts, fmt.Sprintf("%s %s (shift %+.1f Hz for a %+d Hz retune, score %.2f): %s", inv.Mode, s, inv.MeasuredShiftHz, inv.DialStepHz, inv.Score, inv.Verdict))
		}
		p.answer("Q08", strings.Join(parts, "; "), "measured", "if-<mode> vs if-<mode>-up recordings")
	} else {
		unknown("Q08", "needs --allow-writes")
	}
	if len(r.Offsets) > 0 {
		var parts []string
		for _, o := range r.Offsets {
			parts = append(parts, fmt.Sprintf("%s Δ=%+.0f Hz (peak at %.1f Hz in the IF, dial %d, carrier %d)", o.Mode, o.DeltaHz, o.PeakIFHz, o.DialHz, o.RefCarrierHz))
		}
		p.answer("Q09", strings.Join(parts, "; ")+" — assumes the strongest peak is the reference carrier", "measured", "")
	} else if r.Settings.RefCarrierHz == 0 {
		unknown("Q09", "run with --ref-carrier <Hz> while tuned near a carrier of known frequency")
	} else {
		unknown("Q09", "no usable peak or inversion result")
	}

	// Q10: CW pitch.
	if v, ok := r.Notes["menu_170_cw_pitch"]; ok {
		cw := ""
		for _, o := range r.Offsets {
			if o.Mode == "CW" {
				cw = fmt.Sprintf("; CW Δ=%+.0f Hz", o.DeltaHz)
			}
		}
		p.answer("Q10", "Menu 170 was "+v+cw+". Run again with a different pitch to see whether Δ_CW changes", "owner", "")
	} else {
		unknown("Q10", "record Menu 170 with --note menu_170_cw_pitch=<Hz>, run twice with different pitches")
	}

	// Q11: IF in FM/NFM.
	var fm []string
	for _, m := range extraModes {
		a := p.recording("mode-" + strings.ToLower(m.String()))
		if a == nil {
			continue
		}
		ref := p.recording("if-usb")
		switch {
		case a.Analysis.Silent:
			fm = append(fm, m.String()+": silent")
		case ref != nil && a.Analysis.Levels[0].RMSDBFS < ref.Analysis.Levels[0].RMSDBFS-20:
			fm = append(fm, fmt.Sprintf("%s: %.0f dB below USB — probably no IF", m, ref.Analysis.Levels[0].RMSDBFS-a.Analysis.Levels[0].RMSDBFS))
		case a.Analysis.Passband != nil && a.Analysis.Passband.Shaped:
			fm = append(fm, fmt.Sprintf("%s: IF present, passband %.0f–%.0f Hz", m, a.Analysis.Passband.Low6Hz, a.Analysis.Passband.High6Hz))
		default:
			fm = append(fm, fmt.Sprintf("%s: audio present (%.1f dBFS RMS) but no IF passband shape", m, a.Analysis.Levels[0].RMSDBFS))
		}
	}
	if len(fm) > 0 {
		p.answer("Q11", strings.Join(fm, "; "), "inferred", "mode-fm and mode-nfm recordings")
	} else {
		unknown("Q11", "needs --allow-writes")
	}

	// Q12: before/after filters.
	if a := p.recording("if-usb"); a != nil && a.Analysis.Passband != nil && a.Analysis.Passband.Shaped {
		w := a.Analysis.Passband.WidthHz
		ssb, _ := r.CAT.State["filter_ssb_hz"].(int)
		if ssb > 0 && w > 2*float64(ssb) {
			p.answer("Q12", fmt.Sprintf("before the SSB filter: the IF is %.0f Hz wide while the radio's SSB filter is %d Hz. AGC: not measured", w, ssb), "inferred", "if-usb passband vs SH 0")
		} else {
			p.answer("Q12", fmt.Sprintf("the IF is %.0f Hz wide; the radio's SSB filter is %d Hz. AGC: not measured", w, ssb), "inferred", "if-usb passband vs SH 0")
		}
	} else {
		unknown("Q12", "needs an IF recording with a measurable passband")
	}

	// Q13: owner.
	if v, ok := r.Notes["speaker_during_if"]; ok {
		p.answer("Q13", v, "owner", "")
	} else {
		unknown("Q13", "the owner's answer (interactive run, or --note speaker_during_if=yes|no)")
	}

	// Q14: FT/FS.
	if v, ok := r.Notes["band_a_changed"]; ok {
		p.answer("Q14", "band A's display changed during the probe: "+v+". FT/FS read-backs are in steps", "owner", "")
	} else {
		unknown("Q14", "the owner's observation of band A while FT/FS were set (interactive run)")
	}

	// Q15: resolution.
	for _, s := range r.Steps {
		if strings.HasPrefix(s.Action, "resolution test") {
			p.answer("Q15", s.Action+": "+s.Result+" (fine tuning on, 20 Hz step)", "measured", "")
		}
	}
	if !slices.ContainsFunc(r.Answers, func(a Answer) bool { return a.ID == "Q15" }) {
		unknown("Q15", "needs --allow-writes")
	}

	// Q16: AI.
	switch {
	case r.AITest != nil && len(r.AITest.Lines) > 0:
		p.answer("Q16", "unsolicited lines: "+strings.Join(r.AITest.Lines[:min(8, len(r.AITest.Lines))], " | "), "measured", "AI test")
	case r.AITest != nil:
		p.answer("Q16", "AI test ran: "+r.AITest.Note, "measured", "AI test")
	default:
		unknown("Q16", "needs an interactive --allow-writes run")
	}

	// Q17: Detect.
	if a := p.recording("detect-usb"); a != nil {
		an := a.Analysis
		desc := fmt.Sprintf("RMS %.1f dBFS, %.0f%% of power below 4 kHz", an.Levels[0].RMSDBFS, 100*an.LowBandFraction)
		if an.Silent {
			desc = "silent"
		}
		p.answer("Q17", desc+"; listen to detect-usb.wav", "measured", "detect-usb recording")
	} else {
		unknown("Q17", "needs --allow-writes")
	}

	// Q18: MD moves FQ.
	var moved []string
	for _, w := range r.Warnings {
		if strings.HasPrefix(w, "changing mode to") {
			moved = append(moved, w)
		}
	}
	if len(moved) > 0 {
		p.answer("Q18", "yes: "+strings.Join(moved, "; "), "measured", "")
	} else if len(r.Inversion) > 0 {
		p.answer("Q18", "no: the dial read back the same after every mode change", "measured", "")
	} else {
		unknown("Q18", "needs --allow-writes")
	}

	// Q19: level.
	if anyIF != nil {
		peak := math.Inf(-1)
		for _, rec := range r.Recordings {
			if strings.HasPrefix(rec.Name, "if-") {
				peak = math.Max(peak, rec.Analysis.Levels[0].PeakDBFS)
			}
		}
		verdict := "fine"
		switch {
		case peak > -1:
			verdict = "too hot: lower Menu 91A"
		case peak > -6:
			verdict = "a little hot: lower Menu 91A by one"
		case peak < -40:
			verdict = "very low: raise Menu 91A"
		}
		p.answer("Q19", fmt.Sprintf("IF peak %.1f dBFS across all IF recordings: %s", peak, verdict), "measured", "")
	} else {
		unknown("Q19", "needs IF recordings")
	}

	// Q20: foreign traffic.
	h := r.CAT.Hygiene
	if h.NMEALines+h.APRSLines+h.KISSFrames > 0 {
		p.answer("Q20", fmt.Sprintf("YES: %d NMEA, %d APRS lines, %d KISS frames — set Menus 981–983 to Bluetooth", h.NMEALines, h.APRSLines, h.KISSFrames), "measured", strings.Join(r.CAT.Foreign, " | "))
	} else if r.CAT.Responding {
		p.answer("Q20", "no foreign traffic seen", "measured", "")
	} else {
		unknown("Q20", "the radio did not answer")
	}
}
