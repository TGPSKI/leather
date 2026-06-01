package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentRunKey_Deterministic(t *testing.T) {
	k1 := AgentRunKey("agent", "sys", "usr", "llama3")
	k2 := AgentRunKey("agent", "sys", "usr", "llama3")
	if k1 != k2 {
		t.Errorf("key not deterministic: %q != %q", k1, k2)
	}
}

func TestAgentRunKey_DistinctInputs(t *testing.T) {
	cases := [][4]string{
		{"a", "s", "u", "m"},
		{"b", "s", "u", "m"}, // different agent
		{"a", "X", "u", "m"}, // different system prompt
		{"a", "s", "Y", "m"}, // different user prompt
		{"a", "s", "u", "Z"}, // different model
	}
	seen := map[string]bool{}
	for _, c := range cases {
		k := AgentRunKey(c[0], c[1], c[2], c[3])
		if seen[k] {
			t.Errorf("collision for inputs %v", c)
		}
		seen[k] = true
	}
}

func TestAgentRunKey_NoBoundaryCollision(t *testing.T) {
	// "ab" + "" must differ from "a" + "b" (NUL separator prevents this).
	k1 := AgentRunKey("ab", "", "u", "m")
	k2 := AgentRunKey("a", "b", "u", "m")
	if k1 == k2 {
		t.Error("boundary collision: NUL separator not working")
	}
}

func TestFileCache_Miss(t *testing.T) {
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("Get on missing key returned ok=true")
	}
}

func TestFileCache_SetGet(t *testing.T) {
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	key := AgentRunKey("agent", "sys", "usr", "m")
	if err := c.Set(key, "hello world", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, ok := c.Get(key)
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if val != "hello world" {
		t.Errorf("value: got %q, want %q", val, "hello world")
	}
}

func TestFileCache_TTLExpiry(t *testing.T) {
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	key := AgentRunKey("agent", "sys", "usr", "m")
	// Use a TTL that's already expired: Set with a 1ms duration, then wait for it.
	if err := c.Set(key, "stale", 1*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, ok := c.Get(key)
	if ok {
		t.Error("Get returned ok=true for expired entry")
	}
	// Confirm the expired file was deleted lazily.
	path := filepath.Join(c.dir, key+".json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expired cache file was not deleted on lazy expiry")
	}
}

func TestFileCache_NoExpiry(t *testing.T) {
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	key := AgentRunKey("a", "b", "c", "d")
	if err := c.Set(key, "permanent", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, ok := c.Get(key)
	if !ok {
		t.Fatal("Get returned ok=false for no-expiry entry")
	}
	if val != "permanent" {
		t.Errorf("value: got %q", val)
	}
}

func TestFileCache_Persistence(t *testing.T) {
	dir := t.TempDir()
	c1, _ := NewFileCache(dir)
	key := AgentRunKey("persist", "sys", "usr", "m")
	if err := c1.Set(key, "cached response", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Open a second FileCache pointing at the same dir.
	c2, _ := NewFileCache(dir)
	val, ok := c2.Get(key)
	if !ok {
		t.Fatal("Get from second cache instance returned ok=false")
	}
	if val != "cached response" {
		t.Errorf("value: got %q", val)
	}
}
