package aprsapp

import (
	"fmt"
	"strings"

	"d75scope/aprs"
)

// summary describes a decoded packet in one line for the packet log.
func summary(d aprs.Data, err error) string {
	if err != nil {
		return "Malformed: " + strings.TrimPrefix(err.Error(), aprs.ErrMalformed.Error()+": ")
	}
	switch d.Kind {
	case aprs.KindPosition:
		s := "Position " + position(d.Position)
		if d.MicE != "" {
			s += " · Mic-E " + d.MicE
		}
		return s
	case aprs.KindObject, aprs.KindItem:
		s := "Object "
		if d.Kind == aprs.KindItem {
			s = "Item "
		}
		s += d.Name
		if d.Killed {
			return s + " deleted"
		}
		return s + " at " + position(d.Position)
	case aprs.KindMessage:
		m := d.Message
		switch m.Kind {
		case aprs.MessageAck:
			return fmt.Sprintf("Ack to %s for #%s", m.To, m.ID)
		case aprs.MessageRej:
			return fmt.Sprintf("Reject to %s for #%s", m.To, m.ID)
		}
		what := "Message"
		if m.IsBulletin() {
			what = "Bulletin"
		}
		s := fmt.Sprintf("%s to %s: %s", what, m.To, m.Text)
		if m.ID != "" {
			s += " (#" + m.ID + ")"
		}
		return s
	case aprs.KindStatus:
		return "Status: " + d.Status
	case aprs.KindThirdParty:
		return "Third-party packet"
	}
	switch d.Type {
	case 0:
		return "Empty"
	case 'T':
		return "Telemetry"
	case '_', '#', '*':
		return "Weather report"
	case '!':
		return "Weather report (Ultimeter)"
	case '<':
		return "Station capabilities"
	case '?':
		return "Query"
	case '$':
		return "Raw GPS data"
	case '{':
		return "User-defined data"
	}
	return fmt.Sprintf("Unknown data type %q", d.Type)
}

func position(p *aprs.Position) string {
	s := fmt.Sprintf("%.4f, %.4f", p.Lat, p.Lon)
	if p.HasCourseSpeed && (p.Speed > 0 || p.Course > 0) {
		s += fmt.Sprintf(" · %.0f kn", p.Speed)
		if p.Course > 0 {
			s += fmt.Sprintf(" at %d°", p.Course)
		}
	}
	if p.HasAltitude {
		s += fmt.Sprintf(" · %.0f m", p.Altitude)
	}
	return s
}
