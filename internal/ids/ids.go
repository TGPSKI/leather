// Package ids generates the identifier strings used across leather. TimestampHex
// produces the "<prefix>_<yyyymmdd>_<HHMM>_<4hex>" form shared by artifact,
// queue-item, and hide IDs; RandHex produces cryptographically random hex tokens
// for bearer secrets. The TimestampHex suffix is for uniqueness, not security.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"time"
)

// TimestampHex returns an identifier of the form
// "<prefix>_<yyyymmdd>_<HHMM>_<4hex>". The hex suffix provides intra-minute
// uniqueness and is not cryptographically random.
func TimestampHex(prefix string) string {
	suffix := mathrand.Int31n(0x10000) //nolint:gosec // uniqueness, not security
	return fmt.Sprintf("%s_%s_%04x", prefix, time.Now().Format("20060102_1504"), suffix)
}

// RandHex returns n cryptographically random bytes hex-encoded as a 2n-character
// string. Suitable for bearer tokens and other secrets.
func RandHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ids: rand read: %w", err)
	}
	return hex.EncodeToString(b), nil
}
