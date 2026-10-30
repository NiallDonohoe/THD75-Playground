package orbit

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// WGS-72 constants, which element sets are fitted with (Vallado's
// "wgs72" gravity model, as his verification output uses).
const (
	earthRadiusKm = 6378.135
	muKm3s2       = 398600.8
	j2            = 0.001082616
	j3            = -0.00000253881
	j4            = -0.00000165597
	j3oj2         = j3 / j2
	x2o3          = 2.0 / 3.0
)

var (
	xke       = 60 / math.Sqrt(earthRadiusKm*earthRadiusKm*earthRadiusKm/muKm3s2) // per minute
	vkmpersec = earthRadiusKm * xke / 60
)

// Errors from SGP4.
var (
	// ErrDeepSpace refuses element sets with a period of 225 minutes or
	// more, which need SDP4's lunar, solar and resonance terms.
	ErrDeepSpace = errors.New("orbit: deep-space orbit (period ≥ 225 min) is not supported")
	// ErrDecayed means the model puts the satellite below the surface.
	ErrDecayed = errors.New("orbit: satellite has decayed")
	// ErrDiverged means the elements are no longer meaningful at this time
	// (eccentricity or semi-latus rectum out of range).
	ErrDiverged = errors.New("orbit: elements diverged")
)

// Satellite is an element set initialised for SGP4.
type Satellite struct {
	Elements
	no, ecco, inclo, nodeo, argpo, mo, bstar float64

	isimp                                   bool
	aycof, con41, cc1, cc4, cc5, d2, d3, d4 float64
	delmo, eta, argpdot, omgcof, sinmao     float64
	t2cof, t3cof, t4cof, t5cof, x1mth2      float64
	x7thm1, mdot, nodedot, xlcof, xmcof     float64
	nodecf                                  float64
}

// New initialises SGP4 for an element set.
func New(e Elements) (*Satellite, error) {
	if err := e.check(); err != nil {
		return nil, err
	}
	const deg = math.Pi / 180
	s := &Satellite{Elements: e}
	s.no = e.MeanMotion * 2 * math.Pi / 1440 // rad/min, Kozai
	s.ecco, s.inclo = e.Ecc, e.Incl*deg
	s.nodeo, s.argpo, s.mo = e.RAAN*deg, e.ArgPerigee*deg, e.MeanAnomaly*deg
	s.bstar = e.BStar

	// initl: recover the Brouwer mean motion and semi-major axis.
	eccsq := s.ecco * s.ecco
	omeosq := 1 - eccsq
	rteosq := math.Sqrt(omeosq)
	cosio := math.Cos(s.inclo)
	cosio2 := cosio * cosio
	ak := math.Pow(xke/s.no, x2o3)
	d1 := 0.75 * j2 * (3*cosio2 - 1) / (rteosq * omeosq)
	del := d1 / (ak * ak)
	adel := ak * (1 - del*del - del*(1.0/3+134*del*del/81))
	del = d1 / (adel * adel)
	s.no /= 1 + del
	if 2*math.Pi/s.no >= 225 {
		return nil, ErrDeepSpace
	}
	ao := math.Pow(xke/s.no, x2o3)
	sinio := math.Sin(s.inclo)
	po := ao * omeosq
	con42 := 1 - 5*cosio2
	s.con41 = -con42 - cosio2 - cosio2
	posq := po * po
	rp := ao * (1 - s.ecco)

	// sgp4init, near-Earth branch.
	ss := 78/earthRadiusKm + 1
	qzms2t := math.Pow((120-78)/earthRadiusKm, 4)
	s.isimp = rp < 220/earthRadiusKm+1
	sfour, qzms24 := ss, qzms2t
	if perige := (rp - 1) * earthRadiusKm; perige < 156 {
		sfour = perige - 78
		if perige < 98 {
			sfour = 20
		}
		qzms24 = math.Pow((120-sfour)/earthRadiusKm, 4)
		sfour = sfour/earthRadiusKm + 1
	}
	pinvsq := 1 / posq
	tsi := 1 / (ao - sfour)
	s.eta = ao * s.ecco * tsi
	etasq := s.eta * s.eta
	eeta := s.ecco * s.eta
	psisq := math.Abs(1 - etasq)
	coef := qzms24 * math.Pow(tsi, 4)
	coef1 := coef / math.Pow(psisq, 3.5)
	cc2 := coef1 * s.no * (ao*(1+1.5*etasq+eeta*(4+etasq)) +
		0.375*j2*tsi/psisq*s.con41*(8+3*etasq*(8+etasq)))
	s.cc1 = s.bstar * cc2
	cc3 := 0.0
	if s.ecco > 1e-4 {
		cc3 = -2 * coef * tsi * j3oj2 * s.no * sinio / s.ecco
	}
	s.x1mth2 = 1 - cosio2
	s.cc4 = 2 * s.no * coef1 * ao * omeosq * (s.eta*(2+0.5*etasq) + s.ecco*(0.5+2*etasq) -
		j2*tsi/(ao*psisq)*(-3*s.con41*(1-2*eeta+etasq*(1.5-0.5*eeta))+
			0.75*s.x1mth2*(2*etasq-eeta*(1+etasq))*math.Cos(2*s.argpo)))
	s.cc5 = 2 * coef1 * ao * omeosq * (1 + 2.75*(etasq+eeta) + eeta*etasq)
	cosio4 := cosio2 * cosio2
	temp1 := 1.5 * j2 * pinvsq * s.no
	temp2 := 0.5 * temp1 * j2 * pinvsq
	temp3 := -0.46875 * j4 * pinvsq * pinvsq * s.no
	s.mdot = s.no + 0.5*temp1*rteosq*s.con41 + 0.0625*temp2*rteosq*(13-78*cosio2+137*cosio4)
	s.argpdot = -0.5*temp1*con42 + 0.0625*temp2*(7-114*cosio2+395*cosio4) +
		temp3*(3-36*cosio2+49*cosio4)
	xhdot1 := -temp1 * cosio
	s.nodedot = xhdot1 + (0.5*temp2*(4-19*cosio2)+2*temp3*(3-7*cosio2))*cosio
	s.omgcof = s.bstar * cc3 * math.Cos(s.argpo)
	if s.ecco > 1e-4 {
		s.xmcof = -x2o3 * coef * s.bstar / eeta
	}
	s.nodecf = 3.5 * omeosq * xhdot1 * s.cc1
	s.t2cof = 1.5 * s.cc1
	if math.Abs(cosio+1) > 1.5e-12 {
		s.xlcof = -0.25 * j3oj2 * sinio * (3 + 5*cosio) / (1 + cosio)
	} else {
		s.xlcof = -0.25 * j3oj2 * sinio * (3 + 5*cosio) / 1.5e-12
	}
	s.aycof = -0.5 * j3oj2 * sinio
	s.delmo = math.Pow(1+s.eta*math.Cos(s.mo), 3)
	s.sinmao = math.Sin(s.mo)
	s.x7thm1 = 7*cosio2 - 1
	if !s.isimp {
		cc1sq := s.cc1 * s.cc1
		s.d2 = 4 * ao * tsi * cc1sq
		temp := s.d2 * tsi * s.cc1 / 3
		s.d3 = (17*ao + sfour) * temp
		s.d4 = 0.5 * temp * ao * tsi * (221*ao + 31*sfour) * s.cc1
		s.t3cof = s.d2 + 2*cc1sq
		s.t4cof = 0.25 * (3*s.d3 + s.cc1*(12*s.d2+10*cc1sq))
		s.t5cof = 0.2 * (3*s.d4 + 12*s.cc1*s.d3 + 6*s.d2*s.d2 + 15*cc1sq*(2*s.d2+cc1sq))
	}
	// Vallado's sgp4init ends by propagating to the epoch, which catches
	// element sets that are bad from the start.
	if _, _, err := s.propagate(0); err != nil {
		return nil, err
	}
	return s, nil
}

// Vec is a Cartesian vector in km or km/s.
type Vec struct{ X, Y, Z float64 }

func (a Vec) Sub(b Vec) Vec       { return Vec{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec) Dot(b Vec) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec) Norm() float64       { return math.Sqrt(a.Dot(a)) }
func (a Vec) Scale(k float64) Vec { return Vec{a.X * k, a.Y * k, a.Z * k} }
func (a Vec) String() string      { return fmt.Sprintf("(%.3f, %.3f, %.3f)", a.X, a.Y, a.Z) }

// Propagate returns the satellite's position (km) and velocity (km/s) in
// the TEME frame at t.
func (s *Satellite) Propagate(t time.Time) (pos, vel Vec, err error) {
	return s.propagate(t.Sub(s.Epoch).Minutes())
}

// propagate is SGP4 proper, tsince in minutes from the epoch.
func (s *Satellite) propagate(tsince float64) (pos, vel Vec, err error) {
	const twoPi = 2 * math.Pi
	t := tsince
	xmdf := s.mo + s.mdot*t
	argpdf := s.argpo + s.argpdot*t
	nodedf := s.nodeo + s.nodedot*t
	argpm, mm := argpdf, xmdf
	t2 := t * t
	nodem := nodedf + s.nodecf*t2
	tempa := 1 - s.cc1*t
	tempe := s.bstar * s.cc4 * t
	templ := s.t2cof * t2
	if !s.isimp {
		delomg := s.omgcof * t
		delm := s.xmcof * (math.Pow(1+s.eta*math.Cos(xmdf), 3) - s.delmo)
		temp := delomg + delm
		mm = xmdf + temp
		argpm = argpdf - temp
		t3 := t2 * t
		t4 := t3 * t
		tempa -= s.d2*t2 + s.d3*t3 + s.d4*t4
		tempe += s.bstar * s.cc5 * (math.Sin(mm) - s.sinmao)
		templ += s.t3cof*t3 + t4*(s.t4cof+t*s.t5cof)
	}
	nm, em, inclm := s.no, s.ecco, s.inclo
	am := math.Pow(xke/nm, x2o3) * tempa * tempa
	nm = xke / math.Pow(am, 1.5)
	em -= tempe
	if em >= 1 || em < -0.001 || am < 0.95 {
		return pos, vel, ErrDiverged
	}
	if em < 1e-6 {
		em = 1e-6
	}
	mm += s.no * templ
	xlm := mm + argpm + nodem
	nodem = math.Mod(nodem, twoPi)
	argpm = math.Mod(argpm, twoPi)
	xlm = math.Mod(xlm, twoPi)
	mm = math.Mod(xlm-argpm-nodem, twoPi)

	sinip, cosip := math.Sin(inclm), math.Cos(inclm)
	ep, argpp, nodep, mp := em, argpm, nodem, mm

	// Long-period periodics.
	axnl := ep * math.Cos(argpp)
	temp := 1 / (am * (1 - ep*ep))
	aynl := ep*math.Sin(argpp) + temp*s.aycof
	xl := mp + argpp + nodep + temp*s.xlcof*axnl

	// Kepler's equation.
	u := math.Mod(xl-nodep, twoPi)
	eo1 := u
	var sineo1, coseo1 float64
	for k, tem5 := 0, 1.0; math.Abs(tem5) >= 1e-12 && k < 10; k++ {
		sineo1, coseo1 = math.Sin(eo1), math.Cos(eo1)
		tem5 = 1 - coseo1*axnl - sineo1*aynl
		tem5 = (u - aynl*coseo1 + axnl*sineo1 - eo1) / tem5
		if math.Abs(tem5) >= 0.95 {
			tem5 = math.Copysign(0.95, tem5)
		}
		eo1 += tem5
	}

	// Short-period preliminary quantities.
	ecose := axnl*coseo1 + aynl*sineo1
	esine := axnl*sineo1 - aynl*coseo1
	el2 := axnl*axnl + aynl*aynl
	pl := am * (1 - el2)
	if pl < 0 {
		return pos, vel, ErrDiverged
	}
	rl := am * (1 - ecose)
	rdotl := math.Sqrt(am) * esine / rl
	rvdotl := math.Sqrt(pl) / rl
	betal := math.Sqrt(1 - el2)
	temp = esine / (1 + betal)
	sinu := am / rl * (sineo1 - aynl - axnl*temp)
	cosu := am / rl * (coseo1 - axnl + aynl*temp)
	su := math.Atan2(sinu, cosu)
	sin2u := (cosu + cosu) * sinu
	cos2u := 1 - 2*sinu*sinu
	temp = 1 / pl
	temp1 := 0.5 * j2 * temp
	temp2 := temp1 * temp

	// Short-period periodics.
	mrt := rl*(1-1.5*temp2*betal*s.con41) + 0.5*temp1*s.x1mth2*cos2u
	su -= 0.25 * temp2 * s.x7thm1 * sin2u
	xnode := nodep + 1.5*temp2*cosip*sin2u
	xinc := inclm + 1.5*temp2*cosip*sinip*cos2u
	mvt := rdotl - nm*temp1*s.x1mth2*sin2u/xke
	rvdot := rvdotl + nm*temp1*(s.x1mth2*cos2u+1.5*s.con41)/xke

	// Orientation vectors.
	sinsu, cossu := math.Sin(su), math.Cos(su)
	snod, cnod := math.Sin(xnode), math.Cos(xnode)
	sini, cosi := math.Sin(xinc), math.Cos(xinc)
	xmx, xmy := -snod*cosi, cnod*cosi
	ux, uy, uz := xmx*sinsu+cnod*cossu, xmy*sinsu+snod*cossu, sini*sinsu
	vx, vy, vz := xmx*cossu-cnod*sinsu, xmy*cossu-snod*sinsu, sini*cossu

	pos = Vec{mrt * ux, mrt * uy, mrt * uz}.Scale(earthRadiusKm)
	vel = Vec{mvt*ux + rvdot*vx, mvt*uy + rvdot*vy, mvt*uz + rvdot*vz}.Scale(vkmpersec)
	if mrt < 1 {
		return pos, vel, ErrDecayed
	}
	return pos, vel, nil
}
