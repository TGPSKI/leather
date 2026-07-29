package cli

import "testing"

func TestResolveBuildVersion_LdflagsPassThrough(t *testing.T) {
	v, c := ResolveBuildVersion("v9.9.9", "abc1234")
	if v != "v9.9.9" || c != "abc1234" {
		t.Errorf("ResolveBuildVersion stamped values = %q/%q, want passthrough", v, c)
	}
}

func TestResolveBuildVersion_DevFallbackIsSane(t *testing.T) {
	// In a test binary the module version is "(devel)" and vcs settings may be
	// absent; the function must return usable, non-empty values either way.
	v, c := ResolveBuildVersion("dev", "none")
	if v == "" || c == "" {
		t.Errorf("ResolveBuildVersion(dev, none) = %q/%q, want non-empty", v, c)
	}
}
