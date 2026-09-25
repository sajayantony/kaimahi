package kmx

import (
	"crypto/sha256"
	"encoding/hex"
)

type Digest struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

func NewDigest(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest{Algorithm: "sha256", Value: hex.EncodeToString(sum[:])}
}

func (d Digest) IsZero() bool {
	return d.Algorithm == "" && d.Value == ""
}
