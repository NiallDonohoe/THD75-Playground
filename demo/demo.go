// Package demo makes up APRS traffic for d75aprs --demo: stations around a
// centre heard on a fake RF channel and a fake APRS-IS feed, covering every
// case the source tags distinguish, and a fake aprs.fi. Every callsign
// starts with Q, a prefix no country is issued, so none is a real station.
//
// RF packets go through AX.25 encoding and decoding, as KISS frames would.
package demo

import (
	"fmt"
	"math"
	"sync"
	"time"

	"d75scope/aprs"
	"d75scope/aprsapp"
)

// Scenario generates the traffic, one second per Step.
type Scenario struct {
	MyCall   string
	Lat, Lon float64

	mu      sync.Mutex
	tick    int
	msgNo   int
	pending []pending
	last    map[string][2]float64 // latest position sent per station
}

type pending struct {
	due int
	a   aprsapp.Arrival
}

// New returns a scenario centred on lat, lon, sending messages to myCall.
func New(myCall string, lat, lon float64) *Scenario {
	return &Scenario{MyCall: myCall, Lat: lat, Lon: lon, last: map[string][2]float64{}}
}

// Run calls deliver with each arrival, one step a second, until stop closes.
func (s *Scenario) Run(deliver func(aprsapp.Arrival), stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		for _, a := range s.Step(time.Now()) {
			deliver(a)
		}
		select {
		case <-t.C:
		case <-stop:
			return
		}
	}
}

// Step advances one second and returns what arrives in it.
func (s *Scenario) Step(now time.Time) []aprsapp.Arrival {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tick
	s.tick++

	every := func(period, offset int) bool { return t >= offset && (t-offset)%period == 0 }

	// Fixed stations on RF, also gated to APRS-IS by Q3GATE.
	if every(180, 0) {
		s.rfAndIS(now, 1, "Q1HOME", "APK005", nil, s.plain("Q1HOME", 1.2, -0.8, '/', '-', "Home station, monitoring 145.500", true))
	}
	if every(300, 1) {
		s.rfAndIS(now, 2, "Q2DIGI", "APN391", nil, s.plain("Q2DIGI", -2.5, 3.0, '/', '#', "PHG5360 Hilltop digipeater", false))
	}
	if every(300, 2) {
		// The IGate's own beacon: on RF, and posted to APRS-IS directly.
		info := s.plain("Q3GATE", 0.6, 2.2, 'I', '&', "RX IGate for the demo area", false)
		s.rf(now, 0, "Q3GATE", "APDW16", nil, info)
		s.is(now, 1, "Q3GATE", "APDW16", "TCPIP*,qAC,T2DEMO", info)
	}
	if every(300, 4) {
		s.rfAndIS(now, 1, "Q4WX", "APRS", []aprs.Hop{{Call: "Q2DIGI", Used: true}, {Call: "WIDE2-1"}},
			s.plain("Q4WX", 3.5, -2.0, '/', '_', "220/004g009t054r000p012P004h87b10132", false))
	}
	if every(600, 5) {
		s.rf(now, 0, "Q4WX", "APRS", []aprs.Hop{{Call: "Q2DIGI", Used: true}}, "T#042,199,000,255,073,123,01101001")
	}

	// Mobiles sending Mic-E.
	if every(30, 3) {
		lat, lon, course := s.circle(t, 3.0, 1200)
		dst, info := micE(lat, lon, 30, course, '/', '>', 6, ">", 45, true)
		s.last["Q5MOB-9"] = [2]float64{lat, lon}
		path := []aprs.Hop{{Call: "WIDE1-1"}, {Call: "WIDE2-1"}}
		if (t/30)%2 == 1 {
			path = []aprs.Hop{{Call: "Q2DIGI", Used: true}, {Call: "WIDE2-1"}}
		}
		s.rfAndIS(now, 2, "Q5MOB-9", dst, path, info)
	}
	if every(45, 6) {
		lat, lon, course := s.line(t, 6.0, 900)
		dst, info := micE(lat, lon, 22, course, '/', 'v', 7, "]", 60, true)
		s.last["Q6VAN-7"] = [2]float64{lat, lon}
		s.rf(now, 0, "Q6VAN-7", dst, nil, info+"Van on the coast road")
	}
	// A walker with a plain position, course and speed: RF only, never gated.
	if every(60, 7) {
		lat, lon, course := s.circle(t+400, 1.0, 3600)
		p := aprs.Position{Lat: lat, Lon: lon, Symbol: aprs.Symbol{Table: '/', Code: '['}, HasCourseSpeed: true, Course: course, Speed: 3}
		info, _ := aprs.PositionInfo(p, "On foot", true)
		s.last["Q7HIKE-7"] = [2]float64{lat, lon}
		s.rf(now, 0, "Q7HIKE-7", "APK005", []aprs.Hop{{Call: "WIDE1-1"}}, info)
	}

	// APRS-IS only: an internet station (also gated to RF as third party),
	// and stations igated far away.
	if every(120, 8) {
		info := s.plain("Q8FAR", 35, 20, '/', '-', "Internet-connected station", true)
		s.is(now, 0, "Q8FAR", "APRS", "TCPIP*,qAC,T2DEMO", info)
		if every(240, 8) {
			s.thirdParty(now, 2, "Q8FAR", "APRS", info)
		}
	}
	if every(60, 9) {
		lat, lon, course := s.circle(t, 8.0, 2400)
		lat, lon = lat+0.45, lon-0.6
		s.last["Q9FAR-9"] = [2]float64{lat, lon}
		s.is(now, 0, "Q9FAR-9", "APRS", "WIDE1-1,qAR,Q9GATE", compressed(lat, lon, '/', 'k', course, 40)+"Truck, far away")
	}
	if every(90, 10) {
		lat, lon := s.offset(-30, -45)
		s.last["Q0BOAT"] = [2]float64{lat, lon}
		s.is(now, 0, "Q0BOAT", "APRS", "WIDE2-1,qAo,Q0GATE", compressed(lat, lon, '/', 'Y', 270, 6)+"Sailing")
	}

	// An object, an item, a status, a bulletin.
	if every(300, 11) {
		lat, lon := s.offset(0.3, 0.4)
		p := aprs.Position{Lat: lat, Lon: lon, Symbol: aprs.Symbol{Table: '\\', Code: 'n'}}
		info, _ := aprs.PositionInfo(p, "Club net 145.500 Thu 20:00", false)
		obj := ";NETCTRL  *" + now.UTC().Format("021504") + "z" + info[1:]
		s.rfAndIS(now, 1, "Q1HOME", "APK005", nil, obj)
	}
	if every(900, 12) {
		lat, lon := s.offset(0.9, -1.1)
		p := aprs.Position{Lat: lat, Lon: lon, Symbol: aprs.Symbol{Table: '/', Code: 'w'}}
		info, _ := aprs.PositionInfo(p, "Water tap", false)
		s.rf(now, 0, "Q7HIKE-7", "APK005", nil, ")WATER!"+info[1:])
	}
	if every(600, 13) {
		s.rfAndIS(now, 1, "Q1HOME", "APK005", nil, ">Monitoring 145.500")
	}
	if every(600, 14) {
		bln, _ := aprs.MessageInfo("BLN1", "Club net tonight 20:00 on 145.500", "")
		s.rfAndIS(now, 1, "Q1HOME", "APK005", nil, bln)
	}

	// Messages to us: on RF (and gated to IP), and over APRS-IS only.
	if every(150, 20) {
		s.msgNo++
		m, _ := aprs.MessageInfo(s.MyCall, fmt.Sprintf("Hello from the demo, are you on 145.500? %d", s.msgNo), fmt.Sprint(s.msgNo))
		s.rfAndIS(now, 2, "Q5MOB-9", "APK005", []aprs.Hop{{Call: "WIDE1-1"}}, m)
	}
	if every(150, 95) {
		s.msgNo++
		m, _ := aprs.MessageInfo(s.MyCall, "Testing over APRS-IS, reply if you see this", fmt.Sprint(s.msgNo))
		s.is(now, 0, "Q8FAR", "APRS", "TCPIP*,qAC,T2DEMO", m)
	}

	// Now and then, a packet that breaks the format.
	if every(600, 30) {
		s.rf(now, 0, "Q2DIGI", "APRS", nil, "!4903.5")
	}

	var out []aprsapp.Arrival
	keep := s.pending[:0]
	for _, p := range s.pending {
		if p.due <= t {
			out = append(out, p.a)
		} else {
			keep = append(keep, p)
		}
	}
	s.pending = keep
	return out
}

// offset returns the point north and east of the centre by the given km.
func (s *Scenario) offset(northKm, eastKm float64) (lat, lon float64) {
	lat = s.Lat + northKm/111.32
	lon = s.Lon + eastKm/(111.32*math.Cos(s.Lat*math.Pi/180))
	return lat, lon
}

// circle moves round a circle of radius km once every period seconds.
func (s *Scenario) circle(t int, km float64, period int) (lat, lon float64, course int) {
	a := 2 * math.Pi * float64(t%period) / float64(period)
	lat, lon = s.offset(km*math.Cos(a), km*math.Sin(a))
	course = int(math.Mod(a*180/math.Pi+90, 360))
	if course == 0 {
		course = 360
	}
	return lat, lon, course
}

// line drives east and back west over km, there and back once a period.
func (s *Scenario) line(t int, km float64, period int) (lat, lon float64, course int) {
	f := float64(t%period) / float64(period)
	course = 90
	if f > 0.5 {
		f, course = 1-f, 270
	}
	lat, lon = s.offset(-1.5, -km/2+2*f*km)
	return lat, lon, course
}

func (s *Scenario) plain(call string, northKm, eastKm float64, table, code byte, comment string, messaging bool) string {
	lat, lon := s.offset(northKm, eastKm)
	s.last[call] = [2]float64{lat, lon}
	info, _ := aprs.PositionInfo(aprs.Position{Lat: lat, Lon: lon, Symbol: aprs.Symbol{Table: table, Code: code}}, comment, messaging)
	return info
}

func (s *Scenario) later(delay int, a aprsapp.Arrival) {
	s.pending = append(s.pending, pending{due: s.tick - 1 + delay, a: a})
}

// rf sends a packet on the fake RF channel, through AX.25 as KISS would.
func (s *Scenario) rf(now time.Time, delay int, src, dst string, path []aprs.Hop, info string) {
	p := aprs.Packet{Src: src, Dst: dst, Path: path, Info: info}
	frame, err := p.AX25()
	if err != nil {
		panic(fmt.Sprintf("demo packet %v: %v", p, err))
	}
	back, err := aprs.DecodeAX25(frame)
	if err != nil {
		panic(fmt.Sprintf("demo frame %v: %v", p, err))
	}
	s.later(delay, aprsapp.Arrival{Source: aprsapp.SourceRF, Packet: back, At: now.Add(time.Duration(delay) * time.Second)})
}

// is sends a packet on the fake APRS-IS feed; path is the TNC2 path text.
func (s *Scenario) is(now time.Time, delay int, src, dst, path, info string) {
	p, err := aprs.ParseTNC2(src + ">" + dst + "," + path + ":" + info)
	if err != nil {
		panic(fmt.Sprintf("demo IS packet: %v", err))
	}
	s.later(delay, aprsapp.Arrival{Source: aprsapp.SourceIS, Packet: p, At: now.Add(time.Duration(delay) * time.Second)})
}

// rfAndIS sends a packet on RF and, isDelay seconds later, the copy Q3GATE
// gates to APRS-IS.
func (s *Scenario) rfAndIS(now time.Time, isDelay int, src, dst string, path []aprs.Hop, info string) {
	s.rf(now, 0, src, dst, path, info)
	tnc2 := ""
	for _, h := range path {
		tnc2 += h.Call
		if h.Used {
			tnc2 += "*"
		}
		tnc2 += ","
	}
	s.is(now, isDelay, src, dst, tnc2+"qAR,Q3GATE", info)
}

// thirdParty sends an APRS-IS packet that Q3GATE relays to RF.
func (s *Scenario) thirdParty(now time.Time, delay int, src, dst, info string) {
	s.rf(now, delay, "Q3GATE", "APDW16", []aprs.Hop{{Call: "WIDE1-1"}}, "}"+src+">"+dst+",TCPIP,Q3GATE*:"+info)
}

// LastPosition returns where the scenario last put a station.
func (s *Scenario) LastPosition(call string) (lat, lon float64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.last[call]
	return p[0], p[1], ok
}
