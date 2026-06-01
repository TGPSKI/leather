package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// yamlValues maps YAML scalar keys to their raw string values.
type yamlValues map[string]string

// yamlLists maps YAML list keys to their raw string-item slices.
type yamlLists map[string][]string

// parseYAML reads a flat YAML document into string maps.
// It handles scalar values, quoted strings, and inline lists ([a, b, c]).
// Comments (#) and blank lines are skipped. Nested maps are not supported.
func parseYAML(r io.Reader) (yamlValues, yamlLists, error) {
	vals := make(yamlValues)
	lists := make(yamlLists)

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
				item = yamlStripQuotes(item)
				if item != "" {
					items = append(items, item)
				}
			}
			lists[key] = items
		} else {
			vals[key] = yamlStripQuotes(raw)
		}
	}

	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("parseYAML: %w", err)
	}
	return vals, lists, nil
}

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
				item := yamlStripQuotes(strings.TrimSpace(trimmed[2:]))
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
				item = yamlStripQuotes(strings.TrimSpace(item))
				if item != "" {
					lists[key] = append(lists[key], item)
				}
			}
		case raw == "":
			// Possibly a block-style list; accumulate "- item" lines that follow.
			inListKey = key
		default:
			vals[key] = yamlStripQuotes(raw)
		}
	}
	return vals, lists
}

// yamlStripQuotes removes surrounding single or double quotes from s.
func yamlStripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
