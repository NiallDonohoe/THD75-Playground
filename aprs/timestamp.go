package aprs

import (
	"fmt"
	"time"
)

// ParseTimestamp resolves a 7-character APRS timestamp to a time, taking
// the missing parts of the date from now. "DDHHMMz" is UTC and "DDHHMM/" is
// local time (now's location): a time more than an hour after now is taken
// to be in an earlier month. "HHMMSSh" is UTC: more than an hour after now
// means yesterday.
func ParseTimestamp(ts string, now time.Time) (time.Time, error) {
	if len(ts) != 7 || !allDigits(ts[:6]) {
		return time.Time{}, fmt.Errorf("%w: timestamp %q", ErrMalformed, ts)
	}
	a := int(ts[0]-'0')*10 + int(ts[1]-'0')
	b := int(ts[2]-'0')*10 + int(ts[3]-'0')
	c := int(ts[4]-'0')*10 + int(ts[5]-'0')
	limit := now.Add(time.Hour)
	switch ts[6] {
	case 'z', '/':
		day, hour, minute := a, b, c
		if day < 1 || day > 31 || hour > 23 || minute > 59 {
			return time.Time{}, fmt.Errorf("%w: timestamp %q", ErrMalformed, ts)
		}
		loc := time.UTC
		if ts[6] == '/' {
			loc = now.Location()
		}
		n := now.In(loc)
		for back := 0; back < 3; back++ {
			t := time.Date(n.Year(), n.Month()-time.Month(back), day, hour, minute, 0, 0, loc)
			if t.Day() == day && !t.After(limit) {
				return t, nil
			}
		}
	case 'h':
		hour, minute, second := a, b, c
		if hour > 23 || minute > 59 || second > 59 {
			return time.Time{}, fmt.Errorf("%w: timestamp %q", ErrMalformed, ts)
		}
		n := now.UTC()
		t := time.Date(n.Year(), n.Month(), n.Day(), hour, minute, second, 0, time.UTC)
		if t.After(limit) {
			t = t.AddDate(0, 0, -1)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: timestamp %q", ErrMalformed, ts)
}
