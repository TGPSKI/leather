package ids

import (
	"regexp"
	"testing"
)

var timestampHexRE = regexp.MustCompile(`^pre_\d{8}_\d{4}_[0-9a-f]{4}$`)

func TestTimestampHexFormat(t *testing.T) {
	id := TimestampHex("pre")
	if !timestampHexRE.MatchString(id) {
		t.Errorf("TimestampHex(%q) = %q, does not match %s", "pre", id, timestampHexRE)
	}
}

func TestTimestampHexPrefixPreserved(t *testing.T) {
	// Prefixes may contain underscores (e.g. "hide_<sanitized>"); the prefix
	// must appear verbatim at the start.
	id := TimestampHex("hide_my_source")
	re := regexp.MustCompile(`^hide_my_source_\d{8}_\d{4}_[0-9a-f]{4}$`)
	if !re.MatchString(id) {
		t.Errorf("TimestampHex preserved prefix incorrectly: %q", id)
	}
}

func TestTimestampHexUniqueness(t *testing.T) {
	seen := make(map[string]struct{})
	collisions := 0
	for i := 0; i < 1000; i++ {
		id := TimestampHex("x")
		if _, ok := seen[id]; ok {
			collisions++
		}
		seen[id] = struct{}{}
	}
	// Within the same minute only the 16-bit hex suffix varies, so a few
	// collisions over 1000 draws are expected; a high rate signals a bug.
	if collisions > 100 {
		t.Errorf("excessive collisions: %d/1000", collisions)
	}
}

func TestRandHex(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		wantLen int
	}{
		{name: "zero", n: 0, wantLen: 0},
		{name: "small", n: 4, wantLen: 8},
		{name: "token", n: 32, wantLen: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RandHex(tt.n)
			if err != nil {
				t.Fatalf("RandHex(%d): %v", tt.n, err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestRandHexDistinct(t *testing.T) {
	a, _ := RandHex(16)
	b, _ := RandHex(16)
	if a == b {
		t.Errorf("two RandHex(16) calls returned identical value %q", a)
	}
}
