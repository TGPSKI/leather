// Package yamlx is leather's stdlib-only parser for the small, flat subset of
// YAML used by config files, agent front-matter, and lifecycle definitions. It
// handles scalar key/value pairs, flow-style lists (key: [a, b]), and
// block-style lists (key:\n  - a). Nested maps are not supported. Inline "#"
// comments and surrounding quotes are stripped from scalar values.
package yamlx

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseBlock parses a flat YAML document from a string and returns scalar and
// list values. Both flow-style lists (key: [a, b]) and block-style lists
// (key:\n  - a\n  - b) are supported. Indentation is not required for
// block-style list items, allowing use on pre-trimmed content.
//
// Keys with empty values are not added to the scalar map; if "- item" lines
// follow, they begin a block-style list entry for that key.
func ParseBlock(src string) (map[string]string, map[string][]string) {
	vals := make(map[string]string)
	lists := make(map[string][]string)
	inListKey := "" // non-empty while accumulating block-style list items

	for _, rawLine := range strings.Split(src, "\n") {
		if inListKey != "" {
			trimmed := strings.TrimSpace(rawLine)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "- ") {
				item := StripQuotes(strings.TrimSpace(trimmed[2:]))
				if item != "" {
					lists[inListKey] = append(lists[inListKey], item)
				}
				continue
			}
			// Non-list-item line ends block-style mode; re-process below.
			inListKey = ""
		}

		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		raw := strings.TrimSpace(line[idx+1:])
		if ci := strings.Index(raw, " #"); ci >= 0 {
			raw = strings.TrimSpace(raw[:ci])
		}
		switch {
		case strings.HasPrefix(raw, "["):
			// Flow-style list: key: [a, b, c]
			raw = strings.TrimPrefix(raw, "[")
			raw = strings.TrimSuffix(raw, "]")
			for _, item := range strings.Split(raw, ",") {
				item = StripQuotes(strings.TrimSpace(item))
				if item != "" {
					lists[key] = append(lists[key], item)
				}
			}
		case raw == "":
			// Possibly a block-style list; accumulate "- item" lines that follow.
			inListKey = key
		default:
			vals[key] = StripQuotes(raw)
		}
	}
	return vals, lists
}

// ParseFlat reads a flat YAML document from r into scalar and list maps.
// It handles scalar values, quoted strings, and inline flow lists ([a, b, c]).
// Comments (#) and blank lines are skipped. Nested maps are not supported.
func ParseFlat(r io.Reader) (vals map[string]string, lists map[string][]string, err error) {
	vals = make(map[string]string)
	lists = make(map[string][]string)

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}

		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		raw := strings.TrimSpace(line[idx+1:])

		// Strip trailing inline comment.
		if ci := strings.Index(raw, " #"); ci >= 0 {
			raw = strings.TrimSpace(raw[:ci])
		}

		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			inner := raw[1 : len(raw)-1]
			var items []string
			for _, item := range strings.Split(inner, ",") {
				item = strings.TrimSpace(item)
				item = StripQuotes(item)
				if item != "" {
					items = append(items, item)
				}
			}
			lists[key] = items
		} else {
			vals[key] = StripQuotes(raw)
		}
	}

	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("yamlx.ParseFlat: %w", err)
	}
	return vals, lists, nil
}

// ParseFlatLines is like ParseFlat but also returns a lines map that records
// the 1-indexed source line number at which each scalar key was parsed.
// List keys are not tracked. A key absent from lines has line 0 (unknown).
func ParseFlatLines(r io.Reader) (vals map[string]string, lists map[string][]string, lines map[string]int, err error) {
	vals = make(map[string]string)
	lists = make(map[string][]string)
	lines = make(map[string]int)

	sc := bufio.NewScanner(r)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}

		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		raw := strings.TrimSpace(line[idx+1:])

		if ci := strings.Index(raw, " #"); ci >= 0 {
			raw = strings.TrimSpace(raw[:ci])
		}

		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			inner := raw[1 : len(raw)-1]
			var items []string
			for _, item := range strings.Split(inner, ",") {
				item = strings.TrimSpace(item)
				item = StripQuotes(item)
				if item != "" {
					items = append(items, item)
				}
			}
			lists[key] = items
		} else {
			vals[key] = StripQuotes(raw)
			lines[key] = lineNum
		}
	}

	if err := sc.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("yamlx.ParseFlatLines: %w", err)
	}
	return vals, lists, lines, nil
}

// SplitKV splits a "key: value" line into its key and value, returning
// ok=false when the line has no colon or an empty key. The value has any
// inline "#" comment removed and surrounding single or double quotes stripped.
func SplitKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	if ci := strings.Index(value, " #"); ci >= 0 {
		value = strings.TrimSpace(value[:ci])
	}
	value = StripQuotes(value)
	return key, value, key != ""
}

// StripQuotes removes a single pair of surrounding single or double quotes
// from s, if present. Strings without matching surrounding quotes are returned
// unchanged.
func StripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
