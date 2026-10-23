package probe

import (
	"fmt"
	"strings"
)

// Summary renders the report as plain language for the owner.
func Summary(r *Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	w("D75 probe report — %s (%s)", r.Started.Local().Format("2 Jan 2006 15:04"), r.Mode)
	if r.Mock {
		w("MOCK RUN: fake radio and synthetic IF, not real hardware.")
	}
	w("")
	if r.CAT.Responding {
		w("Radio: %s, firmware %s, on %s at %d baud.", r.CAT.Identity.Model, r.CAT.Identity.Firmware, r.Serial.Port, r.Serial.Baud)
	} else {
		w("Radio: not answering CAT on %q.", r.Serial.Port)
	}
	if d := r.Audio.Radio; d != nil {
		w("Audio: %q (%s), %g Hz, %d channel(s).", d.Name, r.Audio.MatchReason, d.NominalRate, d.InputChannels)
	} else {
		w("Audio: radio's USB audio input not found. %s", r.Audio.Error)
	}
	if r.PreviousSession != nil {
		if r.PreviousSession.Restored {
			w("A previous session had left the radio modified; it was restored first.")
		} else {
			w("A previous session left the radio modified and it was NOT restored: %s", r.PreviousSession.Error)
		}
	}
	if r.Restore != nil {
		if r.Restore.Clean {
			w("Radio settings were restored and verified.")
		} else {
			w("WARNING: the radio was not fully restored. Check Menu 102 (USB Out Select), the mode and the frequency by hand.")
			for _, f := range r.Restore.Fields {
				if f.Outcome == "failed" {
					w("  %s: wanted %q, holds %q: %s", f.Field, f.Original, f.Final, f.Error)
				}
			}
		}
	}
	w("")
	w("Answers (see HARDWARE_QUESTIONS.md):")
	for _, a := range r.Answers {
		w("  %s %s", a.ID, a.Question)
		w("      → %s [%s]", a.Answer, a.Basis)
	}
	if len(r.Warnings) > 0 {
		w("")
		w("Warnings:")
		for _, s := range r.Warnings {
			w("  - %s", s)
		}
	}
	if len(r.Errors) > 0 {
		w("")
		w("Errors:")
		for _, s := range r.Errors {
			w("  - %s", s)
		}
	}
	w("")
	w("Recordings: %d WAV files alongside this summary.", len(r.Recordings))
	return b.String()
}
