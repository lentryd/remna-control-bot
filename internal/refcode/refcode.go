// Package refcode turns a customer id into a short referral code and back.
// code = base62(id XOR mask) + base62(HMAC(secret, id)) padded to 4 chars;
// nothing is stored, and a forged code has to guess the 4-char signature.
package refcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"math"
	"strings"
)

const (
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sigLen   = 4
	sigSpace = 62 * 62 * 62 * 62
	maxLen   = 6 + sigLen // a 32-bit head is at most 6 base62 chars
)

type Codec struct {
	secret []byte
	mask   uint64
}

func New(secret string) *Codec {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("mask"))
	return &Codec{secret: []byte(secret), mask: uint64(binary.BigEndian.Uint32(m.Sum(nil)[:4]))}
}

// Encode supports ids in [1, MaxInt32].
func (c *Codec) Encode(id int) string { return encode(uint64(id)^c.mask, 1) + c.sig(id) }

func (c *Codec) Decode(code string) (int, bool) {
	if len(code) <= sigLen || len(code) > maxLen {
		return 0, false
	}
	head, sig := code[:len(code)-sigLen], code[len(code)-sigLen:]
	if len(head) > 1 && head[0] == '0' { // only the canonical form is valid
		return 0, false
	}
	v, ok := decode(head)
	if !ok {
		return 0, false
	}
	id := v ^ c.mask
	if id == 0 || id > math.MaxInt32 {
		return 0, false
	}
	if subtle.ConstantTimeCompare([]byte(c.sig(int(id))), []byte(sig)) != 1 {
		return 0, false
	}
	return int(id), true
}

func (c *Codec) sig(id int) string {
	m := hmac.New(sha256.New, c.secret)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(id))
	m.Write(b[:])
	return encode(binary.BigEndian.Uint64(m.Sum(nil)[:8])%sigSpace, sigLen)
}

func encode(v uint64, width int) string {
	var out []byte
	for v > 0 {
		out = append(out, alphabet[v%62])
		v /= 62
	}
	for len(out) < width {
		out = append(out, '0')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func decode(s string) (uint64, bool) {
	var v uint64
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(alphabet, s[i])
		if d < 0 || v > (math.MaxUint64-uint64(d))/62 {
			return 0, false
		}
		v = v*62 + uint64(d)
	}
	return v, true
}
