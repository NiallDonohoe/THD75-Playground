package ft8

// crcPoly is FT8's 14-bit CRC polynomial, x^14 implied.
const crcPoly = 0x2757

// crc14 runs the CRC over bits (one bit per byte, MSB first), starting from
// zero and without augmentation.
func crc14(bits []uint8) uint16 {
	var r uint16
	for _, b := range bits {
		top := (r>>13)&1 ^ uint16(b&1)
		r = (r << 1) & 0x3fff
		if top != 0 {
			r ^= crcPoly
		}
	}
	return r
}

// payloadCRC is the CRC FT8 sends: over the 77 payload bits followed by
// five zero bits (82 in all), as WSJT-X computes it.
func payloadCRC(p *[payloadBits]uint8) uint16 {
	var buf [payloadBits + 5]uint8
	copy(buf[:], p[:])
	return crc14(buf[:])
}

// addCRC appends the CRC to a payload, making the 91 message bits the LDPC
// code protects.
func addCRC(p *[payloadBits]uint8) (a [ldpcK]uint8) {
	copy(a[:], p[:])
	c := payloadCRC(p)
	for i := 0; i < crcBits; i++ {
		a[payloadBits+i] = uint8(c>>(crcBits-1-i)) & 1
	}
	return a
}

// checkCRC reports whether 91 decoded bits carry a matching CRC, and
// returns the payload.
func checkCRC(a *[ldpcK]uint8) (p [payloadBits]uint8, ok bool) {
	copy(p[:], a[:payloadBits])
	c := payloadCRC(&p)
	for i := 0; i < crcBits; i++ {
		if a[payloadBits+i] != uint8(c>>(crcBits-1-i))&1 {
			return p, false
		}
	}
	return p, true
}
