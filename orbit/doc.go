// Package orbit predicts where amateur satellites are and how they look
// from a station: element sets (TLE and CCSDS OMM), the SGP4 propagator,
// Earth-fixed and geodetic positions, look angles, pass prediction,
// Doppler shift, ground tracks and coverage footprints. It is pure Go
// with no I/O.
//
// SGP4 follows Vallado, Crawford, Hujsak and Kelso, "Revisiting Spacetrack
// Report #3" (AIAA 2006-6753), near-Earth branch only: orbits with a period
// under 225 minutes, which covers every low-orbit amateur satellite. Deep-
// space element sets are refused with ErrDeepSpace (decision 0010).
package orbit
