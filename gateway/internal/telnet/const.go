// Package telnet implements the parts of the Telnet protocol a MUD client
// gateway needs: an incremental parser, an encoder, option negotiation and
// NVT line-ending normalisation.
//
// It is deliberately small and dependency-free. The parser never blocks and
// never allocates per byte, so it can run on a hot read loop; every buffer it
// owns has a hard upper bound.
package telnet

// Telnet command bytes (RFC 854).
const (
	SE   byte = 240
	NOP  byte = 241
	DM   byte = 242
	BRK  byte = 243
	IP   byte = 244
	AO   byte = 245
	AYT  byte = 246
	EC   byte = 247
	EL   byte = 248
	GA   byte = 249
	SB   byte = 250
	WILL byte = 251
	WONT byte = 252
	DO   byte = 253
	DONT byte = 254
	IAC  byte = 255
)

// Telnet option codes used by this gateway.
const (
	OptBinary  byte = 0
	OptEcho    byte = 1
	OptSGA     byte = 3
	OptTType   byte = 24
	OptEOR     byte = 25
	OptNAWS    byte = 31
	OptCharset byte = 42
	OptMCCP2   byte = 86
	OptMCCP3   byte = 87
	OptGMCP    byte = 201
)

// TTYPE subnegotiation commands (RFC 1091).
const (
	TTypeIs   byte = 0
	TTypeSend byte = 1
)

// EOR is the end-of-record command (RFC 885), sent as IAC EOR.
const EOR byte = 239
