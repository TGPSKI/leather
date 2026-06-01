package safepath

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAnchor(t *testing.T) {
	root := "/var/leather/hides"

	tests := []struct {
		name    string
		input   string
		wantErr error
		wantOut string
	}{
		{
			name:    "valid simple id",
			input:   "abc123",
			wantOut: filepath.Join(root, "abc123"),
		},
		{
			name:    "valid nested path",
			input:   "prefix/abc123",
			wantOut: filepath.Join(root, "prefix/abc123"),
		},
		{
			name:    "dotdot traversal",
			input:   "../../etc/passwd",
			wantErr: ErrEscapesRoot,
		},
		{
			name:    "dotdot in middle",
			input:   "a/../../etc/passwd",
			wantErr: ErrEscapesRoot,
		},
		{
			name:    "absolute path",
			input:   "/etc/passwd",
			wantErr: ErrAbsolute,
		},
		{
			name:    "empty name",
			input:   "",
			wantErr: ErrEmpty,
		},
		{
			name:    "NUL byte",
			input:   "abc\x00def",
			wantErr: ErrNUL,
		},
		{
			name:    "just dotdot",
			input:   "..",
			wantErr: ErrEscapesRoot,
		},
		{
			name:    "trailing slash normalised",
			input:   "abc/",
			wantOut: filepath.Join(root, "abc"),
		},
		{
			name:    "dot segment",
			input:   "abc/./def",
			wantOut: filepath.Join(root, "abc/def"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Anchor(root, tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Anchor(%q, %q): want error %v, got %v", root, tc.input, tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Anchor(%q, %q): unexpected error: %v", root, tc.input, err)
			}
			if got != tc.wantOut {
				t.Errorf("Anchor(%q, %q): got %q, want %q", root, tc.input, got, tc.wantOut)
			}
		})
	}
}

func TestAnchorRootWithTrailingSlash(t *testing.T) {
	// Root with trailing slash should behave the same as without.
	root := "/var/leather/hides/"
	got, err := Anchor(root, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/var/leather/hides/abc123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
