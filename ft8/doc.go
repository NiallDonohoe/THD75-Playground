// Package ft8 encodes and decodes FT8, the 15-second weak-signal mode of
// WSJT-X. It is pure Go with no I/O, written from the published protocol
// (Franke, Somerville and Taylor, "The FT4 and FT8 Communication
// Protocols", QEX July/August 2020).
//
// The chain, transmit side: a message of up to 13 characters packs into 77
// bits; a 14-bit CRC makes 91; a (174,91) LDPC code adds 83 parity bits;
// the 174 bits become 58 Gray-coded 8-FSK symbols; three 7×7 Costas arrays
// bring the total to 79 symbols of 0.16 s, 6.25 Hz apart, sent with
// Gaussian smoothing (BT 2.0). A transmission starts 0.5 s into its slot
// and lasts 12.64 s.
//
// Receive runs the chain backwards: a coarse search for Costas patterns in
// a spectrogram, then for each candidate a narrow baseband copy, fine time
// and frequency sync, soft bit metrics, belief-propagation decoding and the
// CRC. Each decoded signal is subtracted from the audio before the next
// pass, so weaker signals underneath it can decode.
package ft8
