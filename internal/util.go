package internal

import "math/rand/v2"

const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomSuffix returns a 5-character random alphanumeric string.
func RandomSuffix() string {
	b := make([]byte, 5)
	for i := range b {
		b[i] = suffixChars[rand.IntN(len(suffixChars))]
	}
	return string(b)
}
