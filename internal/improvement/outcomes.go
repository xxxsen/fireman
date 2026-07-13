package improvement

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrOutcomeBitsInvalid = errors.New("improvement outcome bits invalid")

func EncodeOutcomeBits(outcomes []bool) (string, string) {
	bits := make([]byte, (len(outcomes)+7)/8)
	for i, outcome := range outcomes {
		if outcome {
			bits[i/8] |= 1 << uint(i%8)
		}
	}
	encoded := base64.StdEncoding.EncodeToString(bits)
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("runs:%d\n", len(outcomes))), bits...))
	return encoded, "sha256:" + hex.EncodeToString(sum[:])
}

func DecodeOutcomeBits(encoded, expectedHash string, runs int) ([]bool, error) {
	if runs < 0 {
		return nil, ErrOutcomeBitsInvalid
	}
	bits, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(bits) != (runs+7)/8 {
		return nil, ErrOutcomeBitsInvalid
	}
	if runs%8 != 0 && len(bits) > 0 {
		unusedMask := byte(0xff << uint(runs%8))
		if bits[len(bits)-1]&unusedMask != 0 {
			return nil, ErrOutcomeBitsInvalid
		}
	}
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("runs:%d\n", runs)), bits...))
	if expectedHash != "sha256:"+hex.EncodeToString(sum[:]) {
		return nil, ErrOutcomeBitsInvalid
	}
	out := make([]bool, runs)
	for i := range out {
		out[i] = bits[i/8]&(1<<uint(i%8)) != 0
	}
	return out, nil
}
