package orbit

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Elements is one mean element set, as a TLE or an OMM carries it. Angles
// are in degrees and mean motion in revolutions a day, as published.
type Elements struct {
	Name        string
	Catalog     string // NORAD catalogue number, kept as text (Alpha-5, 6 digits)
	Epoch       time.Time
	MeanMotion  float64 // rev/day (Kozai)
	Ecc         float64
	Incl        float64 // degrees
	RAAN        float64 // degrees
	ArgPerigee  float64 // degrees
	MeanAnomaly float64 // degrees
	BStar       float64 // 1/earth radii
	NDot        float64 // rev/day², for reference only; SGP4 does not use it
	RevNumber   int
}

// ParseTLE reads a two-line element set. name may be empty.
func ParseTLE(name, line1, line2 string) (Elements, error) {
	var e Elements
	line1, line2 = strings.TrimRight(line1, " \r"), strings.TrimRight(line2, " \r")
	if len(line1) < 69 || len(line2) < 69 || line1[0] != '1' || line2[0] != '2' {
		return e, errors.New("orbit: not a two-line element set")
	}
	for _, l := range []string{line1, line2} {
		if err := tleChecksum(l); err != nil {
			return e, err
		}
	}
	e.Name = strings.TrimSpace(strings.TrimPrefix(name, "0 "))
	e.Catalog = strings.TrimLeft(strings.TrimSpace(line1[2:7]), "0")
	if strings.TrimSpace(line2[2:7]) != strings.TrimSpace(line1[2:7]) {
		return e, errors.New("orbit: TLE lines are for different objects")
	}
	var err error
	field := func(s string) float64 {
		if err != nil {
			return 0
		}
		var v float64
		v, err = strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v
	}
	yy := field(line1[18:20])
	day := field(line1[20:32])
	e.NDot = field(line1[33:43])
	e.BStar = expField(line1[53:61], &err)
	e.Incl = field(line2[8:16])
	e.RAAN = field(line2[17:25])
	e.Ecc = field("0." + strings.TrimSpace(line2[26:33]))
	e.ArgPerigee = field(line2[34:42])
	e.MeanAnomaly = field(line2[43:51])
	e.MeanMotion = field(line2[52:63])
	if rev := strings.TrimSpace(line2[63:68]); rev != "" && err == nil {
		e.RevNumber, _ = strconv.Atoi(rev)
	}
	if err != nil {
		return e, fmt.Errorf("orbit: TLE field: %v", err)
	}
	year := 1900 + int(yy)
	if yy < 57 {
		year = 2000 + int(yy)
	}
	e.Epoch = dayOfYear(year, day)
	return e, e.check()
}

// expField reads TLE's assumed-decimal exponent form: " 28098-4" is
// 0.28098e-4.
func expField(s string, err *error) float64 {
	s = strings.TrimSpace(s)
	if *err != nil || s == "" {
		return 0
	}
	sign := 1.0
	if s[0] == '-' || s[0] == '+' {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	i := strings.LastIndexAny(s, "+-")
	if i <= 0 {
		*err = fmt.Errorf("bad exponent field %q", s)
		return 0
	}
	m, e1 := strconv.ParseFloat("0."+s[:i], 64)
	x, e2 := strconv.Atoi(s[i:])
	if e1 != nil || e2 != nil {
		*err = fmt.Errorf("bad exponent field %q", s)
		return 0
	}
	return sign * m * math.Pow(10, float64(x))
}

func tleChecksum(l string) error {
	sum := 0
	for _, c := range l[:68] {
		switch {
		case c >= '0' && c <= '9':
			sum += int(c - '0')
		case c == '-':
			sum++
		}
	}
	if want := int(l[68] - '0'); sum%10 != want {
		return fmt.Errorf("orbit: TLE line %c checksum %d, want %d", l[0], sum%10, want)
	}
	return nil
}

// dayOfYear turns a TLE epoch (year, fractional day of year, 1 = 1 Jan
// 00:00) into a time.
func dayOfYear(year int, day float64) time.Time {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	ns := math.Round((day - 1) * 86400e9)
	return start.Add(time.Duration(ns))
}

// ParseTLEs reads a file of element sets in two- or three-line form, as
// CelesTrak serves them, skipping blank lines. A bad set is reported with
// its line number and the rest are still returned.
func ParseTLEs(text string) ([]Elements, []error) {
	var out []Elements
	var errs []error
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimRight(l, " \r"); l != "" {
			lines = append(lines, l)
		}
	}
	for i := 0; i < len(lines); {
		name := ""
		if lines[i][0] != '1' || len(lines[i]) < 69 {
			name = lines[i]
			i++
		}
		if i+1 >= len(lines) {
			errs = append(errs, fmt.Errorf("orbit: element set %q is cut short", name))
			break
		}
		e, err := ParseTLE(name, lines[i], lines[i+1])
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", i+1, err))
		} else {
			out = append(out, e)
		}
		i += 2
	}
	return out, errs
}

// omm holds the CCSDS OMM keys CelesTrak's JSON uses.
type omm struct {
	ObjectName  string      `json:"OBJECT_NAME"`
	NoradCatID  json.Number `json:"NORAD_CAT_ID"`
	Epoch       string      `json:"EPOCH"`
	MeanMotion  float64     `json:"MEAN_MOTION"`
	Ecc         float64     `json:"ECCENTRICITY"`
	Incl        float64     `json:"INCLINATION"`
	RAAN        float64     `json:"RA_OF_ASC_NODE"`
	ArgPerigee  float64     `json:"ARG_OF_PERICENTER"`
	MeanAnomaly float64     `json:"MEAN_ANOMALY"`
	BStar       float64     `json:"BSTAR"`
	NDot        float64     `json:"MEAN_MOTION_DOT"`
	RevAtEpoch  int         `json:"REV_AT_EPOCH"`
}

// ParseOMMJSON reads CCSDS OMM records in JSON, CelesTrak's FORMAT=json.
// OMM carries catalogue numbers that no longer fit a TLE.
func ParseOMMJSON(data []byte) ([]Elements, []error) {
	var recs []omm
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, []error{fmt.Errorf("orbit: OMM JSON: %v", err)}
	}
	var out []Elements
	var errs []error
	for _, r := range recs {
		t, err := time.Parse("2006-01-02T15:04:05.999999", r.Epoch)
		if err != nil {
			errs = append(errs, fmt.Errorf("orbit: %s: epoch %q", r.ObjectName, r.Epoch))
			continue
		}
		e := Elements{
			Name: r.ObjectName, Catalog: r.NoradCatID.String(), Epoch: t.UTC(),
			MeanMotion: r.MeanMotion, Ecc: r.Ecc, Incl: r.Incl, RAAN: r.RAAN,
			ArgPerigee: r.ArgPerigee, MeanAnomaly: r.MeanAnomaly, BStar: r.BStar,
			NDot: r.NDot, RevNumber: r.RevAtEpoch,
		}
		if err := e.check(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.ObjectName, err))
			continue
		}
		out = append(out, e)
	}
	return out, errs
}

func (e *Elements) check() error {
	for _, v := range []float64{e.MeanMotion, e.Ecc, e.Incl, e.RAAN, e.ArgPerigee, e.MeanAnomaly, e.BStar} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("orbit: element is not a finite number")
		}
	}
	switch {
	case e.MeanMotion <= 0 || e.MeanMotion > 20:
		return fmt.Errorf("orbit: mean motion %g rev/day is not an Earth orbit", e.MeanMotion)
	case e.Ecc < 0 || e.Ecc >= 1:
		return fmt.Errorf("orbit: eccentricity %g", e.Ecc)
	case e.Incl < 0 || e.Incl > 180:
		return fmt.Errorf("orbit: inclination %g°", e.Incl)
	}
	return nil
}
