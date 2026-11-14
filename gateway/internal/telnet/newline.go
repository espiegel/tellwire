package telnet

// Normalizer rewrites the line endings found in MUD output to CR LF, which is
// what a terminal emulator expects. It copes with every convention seen in
// the wild: LF, CR LF, LF CR (common in LP/Diku MUDs) and the NVT bare-CR
// encoding CR NUL. It is stateful so a pair may straddle two reads, and it
// never holds bytes back, so a prompt without a trailing newline shows
// immediately.
type Normalizer struct{ st nlState }

type nlState uint8

const (
	nlNormal  nlState = iota
	nlAfterCR         // last byte emitted was a CR from the input
	nlAfterLF         // a bare LF was just expanded to CR LF
)

// Append appends the normalised form of p to dst and returns the result.
func (n *Normalizer) Append(dst, p []byte) []byte {
	for _, b := range p {
		switch b {
		case '\r':
			if n.st == nlAfterLF {
				n.st = nlNormal // LF CR: the CR is redundant
				continue
			}
			dst = append(dst, '\r')
			n.st = nlAfterCR
		case '\n':
			if n.st == nlAfterCR {
				dst = append(dst, '\n') // completes CR LF
				n.st = nlNormal
			} else {
				dst = append(dst, '\r', '\n')
				n.st = nlAfterLF
			}
		case 0:
			// CR NUL is a bare CR (already emitted). Other NULs are noise.
			n.st = nlNormal
		default:
			dst = append(dst, b)
			n.st = nlNormal
		}
	}
	return dst
}
