package frontier

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func OutcomeHash(outcomes []bool) string {
	bits := make([]byte, (len(outcomes)+7)/8)
	for i, outcome := range outcomes {
		if outcome {
			bits[i/8] |= 1 << uint(i%8)
		}
	}
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("runs:%d\n", len(outcomes))), bits...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func countSuccess(outcomes []bool) int {
	count := 0
	for _, outcome := range outcomes {
		if outcome {
			count++
		}
	}
	return count
}
