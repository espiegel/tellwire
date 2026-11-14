package telnet

// AppendEscaped appends data to dst, doubling every IAC byte.
func AppendEscaped(dst, data []byte) []byte {
	for _, b := range data {
		if b == IAC {
			dst = append(dst, IAC, IAC)
		} else {
			dst = append(dst, b)
		}
	}
	return dst
}

// Negotiation returns IAC <verb> <option>.
func Negotiation(verb, option byte) []byte { return []byte{IAC, verb, option} }

// Subnegotiation returns IAC SB <option> <escaped payload> IAC SE.
func Subnegotiation(option byte, payload []byte) []byte {
	out := make([]byte, 0, len(payload)+6)
	out = append(out, IAC, SB, option)
	out = AppendEscaped(out, payload)
	return append(out, IAC, SE)
}

// NAWS returns the NAWS subnegotiation for the given window size.
func NAWS(cols, rows uint16) []byte {
	return Subnegotiation(OptNAWS, []byte{byte(cols >> 8), byte(cols), byte(rows >> 8), byte(rows)})
}

// TTypeIsReply returns the TTYPE IS <name> subnegotiation.
func TTypeIsReply(name string) []byte {
	return Subnegotiation(OptTType, append([]byte{TTypeIs}, name...))
}

// EncodeLine converts user text to NVT bytes: every line ending (\n, \r, \r\n)
// becomes CR LF, NULs are dropped and IAC is escaped.
func EncodeLine(s string) []byte {
	out := make([]byte, 0, len(s)+2)
	prevCR := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case 0:
		case '\r':
			out = append(out, '\r', '\n')
			prevCR = true
			continue
		case '\n':
			if !prevCR {
				out = append(out, '\r', '\n')
			}
		case IAC:
			out = append(out, IAC, IAC)
		default:
			out = append(out, b)
		}
		prevCR = false
	}
	return out
}
