package orbit

import (
	"math"
	"time"
)

// WGS-84, for the station and for latitudes and heights. SGP4 itself uses
// WGS-72; mixing the two at this point is the usual practice and costs
// far less than the elements' own error.
const (
	wgs84A     = 6378.137 // km
	wgs84F     = 1 / 298.257223563
	earthOmega = 7.292115146706979e-5 // rad/s
	speedOfKmS = 299792.458
)

const deg = math.Pi / 180

// julian returns the Julian date of t (UTC, standing in for UT1).
func julian(t time.Time) float64 {
	return 2440587.5 + float64(t.UnixNano())/86400e9
}

// gmst is the Greenwich mean sidereal time in radians (IAU 1982), which
// turns TEME into an Earth-fixed frame.
func gmst(t time.Time) float64 {
	tut1 := (julian(t) - 2451545) / 36525
	sec := -6.2e-6*tut1*tut1*tut1 + 0.093104*tut1*tut1 +
		(876600*3600+8640184.812866)*tut1 + 67310.54841
	g := math.Mod(sec*deg/240, 2*math.Pi)
	if g < 0 {
		g += 2 * math.Pi
	}
	return g
}

// temeToECEF rotates a TEME position and velocity into the Earth-fixed
// frame, ignoring polar motion (a few metres).
func temeToECEF(pos, vel Vec, t time.Time) (Vec, Vec) {
	g := gmst(t)
	c, s := math.Cos(g), math.Sin(g)
	p := Vec{c*pos.X + s*pos.Y, -s*pos.X + c*pos.Y, pos.Z}
	v := Vec{c*vel.X + s*vel.Y, -s*vel.X + c*vel.Y, vel.Z}
	// Subtract the frame's rotation: v − ω × r.
	v.X += earthOmega * p.Y
	v.Y -= earthOmega * p.X
	return p, v
}

// Geo is a geodetic position: degrees north and east, km above the
// WGS-84 ellipsoid.
type Geo struct{ Lat, Lon, Alt float64 }

// Observer is a station: degrees north and east, metres above the
// ellipsoid.
type Observer struct{ Lat, Lon, AltM float64 }

func (o Observer) ecef() Vec {
	return geoToECEF(Geo{o.Lat, o.Lon, o.AltM / 1000})
}

func geoToECEF(g Geo) Vec {
	e2 := wgs84F * (2 - wgs84F)
	lat, lon := g.Lat*deg, g.Lon*deg
	n := wgs84A / math.Sqrt(1-e2*math.Sin(lat)*math.Sin(lat))
	return Vec{
		(n + g.Alt) * math.Cos(lat) * math.Cos(lon),
		(n + g.Alt) * math.Cos(lat) * math.Sin(lon),
		(n*(1-e2) + g.Alt) * math.Sin(lat),
	}
}

// ecefToGeo converts an Earth-fixed position to geodetic, iterating on
// latitude (converges to well under a metre in a few rounds).
func ecefToGeo(p Vec) Geo {
	e2 := wgs84F * (2 - wgs84F)
	r := math.Hypot(p.X, p.Y)
	lon := math.Atan2(p.Y, p.X)
	lat := math.Atan2(p.Z, r*(1-e2))
	var n, h float64
	for i := 0; i < 6; i++ {
		sl := math.Sin(lat)
		n = wgs84A / math.Sqrt(1-e2*sl*sl)
		h = r/math.Cos(lat) - n
		lat = math.Atan2(p.Z, r*(1-e2*n/(n+h)))
	}
	return Geo{lat / deg, lon / deg, h}
}

// Look is how a satellite appears from a station.
type Look struct {
	Az, El    float64 // degrees: azimuth from true north, elevation
	Range     float64 // km
	RangeRate float64 // km/s, positive when moving away
}

// lookFrom works out az/el/range/range rate from Earth-fixed vectors.
func lookFrom(o Observer, p, v Vec) Look {
	rho := p.Sub(o.ecef())
	lat, lon := o.Lat*deg, o.Lon*deg
	sLat, cLat, sLon, cLon := math.Sin(lat), math.Cos(lat), math.Sin(lon), math.Cos(lon)
	east := -sLon*rho.X + cLon*rho.Y
	north := -sLat*cLon*rho.X - sLat*sLon*rho.Y + cLat*rho.Z
	up := cLat*cLon*rho.X + cLat*sLon*rho.Y + sLat*rho.Z
	r := rho.Norm()
	az := math.Atan2(east, north) / deg
	if az < 0 {
		az += 360
	}
	return Look{Az: az, El: math.Asin(up/r) / deg, Range: r, RangeRate: rho.Dot(v) / r}
}

// Look returns how the satellite appears from o at t.
func (s *Satellite) Look(o Observer, t time.Time) (Look, error) {
	pos, vel, err := s.Propagate(t)
	if err != nil {
		return Look{}, err
	}
	p, v := temeToECEF(pos, vel, t)
	return lookFrom(o, p, v), nil
}

// SubPoint returns the point below the satellite and its height.
func (s *Satellite) SubPoint(t time.Time) (Geo, error) {
	pos, vel, err := s.Propagate(t)
	if err != nil {
		return Geo{}, err
	}
	p, _ := temeToECEF(pos, vel, t)
	return ecefToGeo(p), nil
}

// Doppler returns the frequency heard on the ground from a transmitter on
// freqHz with the given range rate (km/s, positive receding).
func Doppler(freqHz, rangeRate float64) float64 {
	return freqHz * (1 - rangeRate/speedOfKmS)
}
