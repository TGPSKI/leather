package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// envString returns the value of LEATHER_{KEY}, or def if unset or empty.
func envString(key, def string) string {
	if v := os.Getenv("LEATHER_" + key); v != "" {
		return v
	}
	return def
}

// envInt returns the integer value of LEATHER_{KEY}, or def if unset or unparseable.
func envInt(key string, def int) int {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envFloat returns the float64 value of LEATHER_{KEY}, or def if unset or unparseable.
func envFloat(key string, def float64) float64 {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// envBool returns the boolean value of LEATHER_{KEY}, or def if unset or unparseable.
func envBool(key string, def bool) bool {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// envInt64 returns the int64 value of LEATHER_{KEY}, or def if unset or unparseable.
func envInt64(key string, def int64) int64 {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// envDuration returns the duration value of LEATHER_{KEY}, or def if unset or unparseable.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// envCSV returns a comma-separated LEATHER_{KEY} value as a trimmed string slice.
// Empty items are dropped. Unset or empty returns nil.
func envCSV(key string) []string {
	v := os.Getenv("LEATHER_" + key)
	if v == "" {
		return nil
	}
	return splitCSV(v)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
