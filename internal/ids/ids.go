// Package ids generates the identifier strings used across leather. TimestampHex
// produces the "<prefix>_<yyyymmdd>_<HHMM>_<8hex>" form shared by artifact,
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
// "<prefix>_<yyyymmdd>_<HHMM>_<8hex>". The hex suffix provides intra-minute
// uniqueness and is not cryptographically random. 32 suffix bits keep
// birthday-collision odds for hundreds of IDs per (prefix, minute) bucket
// around one in a million; the previous 16-bit suffix collided in practice
// under burst load (~1% per bucket at ~40 IDs/minute), cross-wiring fan-in
// groups that reference hides by ID.
func TimestampHex(prefix string) string {
	suffix := mathrand.Uint32() //nolint:gosec // uniqueness, not security
	return fmt.Sprintf("%s_%s_%08x", prefix, time.Now().Format("20060102_1504"), suffix)
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
