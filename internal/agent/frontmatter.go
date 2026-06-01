package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// frontMatter holds the raw parsed values from the YAML header of a *.agent.md file.
type frontMatter struct {
	Name        string
	Schedule    string
	Model       string
	MaxTokens   int
	Timeout     time.Duration
	Temperature float64
	Enabled     bool
	Tags        []string
	Skills      []string
	Toolsets    []string
	ToolRounds  int
}

// parseFrontMatter extracts and parses the YAML front matter from src.
// Front matter is delimited by --- lines at the very start of the document.
// Returns the parsed front matter, the remaining body text, and any error.
func parseFrontMatter(src string) (frontMatter, string, error) {
	// Sensible defaults match the field descriptions in AGENTS-CORE.md.
	fm := frontMatter{
		Enabled:     true,
		Temperature: 0.7,
	}

	src = strings.ReplaceAll(src, "\r\n", "\n")

	const open = "---\n"
	if !strings.HasPrefix(src, open) {
		// No front matter — treat the entire file as the body.
		return fm, strings.TrimSpace(src), nil
	}

	rest := src[len(open):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontMatter{}, "", fmt.Errorf("parseFrontMatter: missing closing ---")
	}

	yamlBlock := rest[:end]
	// Body starts after the closing \n--- line. Trim any leading newline.
	body := rest[end+4:] // len("\n---") == 4
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimSpace(body)

	if err := applyFrontMatterFields(yamlBlock, &fm); err != nil {
		return frontMatter{}, "", fmt.Errorf("parseFrontMatter: %w", err)
	}
	return fm, body, nil
}

// applyFrontMatterFields parses key:value lines from yamlBlock into fm.
func applyFrontMatterFields(yamlBlock string, fm *frontMatter) error {
	// activeList tracks which list field is currently being accumulated in
	// multi-line block style (e.g., "skills:\n  - repo").
	activeList := ""

	appendItem := func(field, item string) {
		item = fmStripQuotes(strings.TrimSpace(item))
		if item == "" {
			return
		}
		switch field {
		case "skills":
			fm.Skills = append(fm.Skills, item)
		case "toolsets":
			fm.Toolsets = append(fm.Toolsets, item)
		case "tags":
			fm.Tags = append(fm.Tags, item)
		}
	}

	for _, line := range strings.Split(yamlBlock, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Handle block-style list items (e.g., "  - repo" under "skills:").
		if activeList != "" && strings.HasPrefix(trimmed, "- ") {
			appendItem(activeList, strings.TrimPrefix(trimmed, "- "))
			continue
		}

		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			continue
		}
		// A new key resets the active list.
		activeList = ""

		key := strings.TrimSpace(trimmed[:idx])
		raw := strings.TrimSpace(trimmed[idx+1:])

		// Strip trailing inline comment.
		if ci := strings.Index(raw, " #"); ci >= 0 {
			raw = strings.TrimSpace(raw[:ci])
		}

		switch key {
		case "name":
			fm.Name = fmStripQuotes(raw)
		case "schedule":
			fm.Schedule = fmStripQuotes(raw)
		case "model":
			fm.Model = fmStripQuotes(raw)
		case "max_tokens":
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("invalid max_tokens %q: %w", raw, err)
			}
			fm.MaxTokens = n
		case "timeout":
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("invalid timeout %q: %w", raw, err)
			}
			fm.Timeout = d
		case "temperature":
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return fmt.Errorf("invalid temperature %q: %w", raw, err)
			}
			fm.Temperature = f
		case "enabled":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("invalid enabled %q: %w", raw, err)
			}
			fm.Enabled = b
		case "tags":
			if raw == "" {
				activeList = "tags"
				continue
			}
			raw = strings.TrimPrefix(raw, "[")
			raw = strings.TrimSuffix(raw, "]")
			for _, item := range strings.Split(raw, ",") {
				appendItem("tags", item)
			}
		case "skills":
			if raw == "" {
				activeList = "skills"
				continue
			}
			raw = strings.TrimPrefix(raw, "[")
			raw = strings.TrimSuffix(raw, "]")
			for _, item := range strings.Split(raw, ",") {
				appendItem("skills", item)
			}
		case "toolsets":
			if raw == "" {
				activeList = "toolsets"
				continue
			}
			raw = strings.TrimPrefix(raw, "[")
			raw = strings.TrimSuffix(raw, "]")
			for _, item := range strings.Split(raw, ",") {
				appendItem("toolsets", item)
			}
		case "tool_rounds":
			n, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("invalid tool_rounds %q: %w", raw, err)
			}
			fm.ToolRounds = n
			// Unknown keys are silently ignored for forward compatibility.
		}
	}
	return nil
}

// fmStripQuotes removes surrounding single or double quotes from s.
func fmStripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
