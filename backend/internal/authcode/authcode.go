// Package authcode generates human-typeable, high-entropy invite/login codes.
package authcode

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// Crockford base32 alphabet (no I, L, O, U — avoids ambiguity for non-technical users).
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// groups x size = number of significant characters. 12 chars of base32 ≈ 60 bits of entropy.
const (
	groups    = 3
	groupSize = 4
)

// New returns a fresh code formatted as XXXX-XXXX-XXXX using crypto/rand.
func New() string {
	var sb strings.Builder
	sb.Grow(groups*groupSize + (groups - 1))
	for g := 0; g < groups; g++ {
		if g > 0 {
			sb.WriteByte('-')
		}
		for i := 0; i < groupSize; i++ {
			sb.WriteByte(alphabet[randIndex(len(alphabet))])
		}
	}
	return sb.String()
}

// Normalize upper-cases, strips separators/whitespace, and maps Crockford's
// interchangeable characters (I/L→1, O→0) so user-entered codes match storage.
func Normalize(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'I', 'L':
			sb.WriteRune('1')
		case 'O':
			sb.WriteRune('0')
		case 'U':
			sb.WriteRune('V')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func randIndex(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand should never fail; if it does, fail loudly rather than emit a weak code.
		panic("authcode: crypto/rand failure: " + err.Error())
	}
	return int(v.Int64())
}
