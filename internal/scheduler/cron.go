// Package scheduler implements cron expression parsing and schedule computation.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule represents a parsed cron expression that can compute future trigger times.
type Schedule struct {
	once       bool
	hasSeconds bool   // true for six-field (second-granularity) expressions
	seconds    []bool // index 0–59; only populated when hasSeconds is true
	minutes    []bool // index 0–59
	hours      []bool // index 0–23
	days       []bool // index 1–31 (index 0 unused)
	months     []bool // index 1–12 (index 0 unused)
	weekdays   []bool // index 0–6 (Sunday = 0)
}

// ParseSchedule parses a cron expression or the special value "once".
//
// Five-field format:  minute hour day-of-month month day-of-week
// Six-field format:   second minute hour day-of-month month day-of-week
//
// Each field supports: * N N-M */N N-M/S A,B,C and combinations thereof.
func ParseSchedule(expr string) (*Schedule, error) {
	if expr == "once" {
		return &Schedule{once: true}, nil
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 && len(fields) != 6 {
		return nil, fmt.Errorf("cron: expected 5 or 6 fields, got %d in %q", len(fields), expr)
	}

	s := &Schedule{
		hasSeconds: len(fields) == 6,
		seconds:    make([]bool, 60),
		minutes:    make([]bool, 60),
		hours:      make([]bool, 24),
		days:       make([]bool, 32), // 1-indexed; [0] unused
		months:     make([]bool, 13), // 1-indexed; [0] unused
		weekdays:   make([]bool, 7),
	}

	type spec struct {
		dst      []bool
		min, max int
	}
	var specs []spec
	if s.hasSeconds {
		specs = []spec{
			{s.seconds, 0, 59},
			{s.minutes, 0, 59},
			{s.hours, 0, 23},
			{s.days, 1, 31},
			{s.months, 1, 12},
			{s.weekdays, 0, 6},
		}
	} else {
		specs = []spec{
			{s.minutes, 0, 59},
			{s.hours, 0, 23},
			{s.days, 1, 31},
			{s.months, 1, 12},
			{s.weekdays, 0, 6},
		}
	}

	for i, sp := range specs {
		if err := parseCronField(fields[i], sp.min, sp.max, sp.dst); err != nil {
			return nil, fmt.Errorf("cron: field %d (%q): %w", i+1, fields[i], err)
		}
	}
	return s, nil
}

// Once reports whether this schedule fires only once (the "once" schedule).
func (s *Schedule) Once() bool { return s.once }

// HasSeconds reports whether this is a six-field (second-granularity) schedule.
func (s *Schedule) HasSeconds() bool { return s.hasSeconds }

// Next returns the next time strictly after from that matches the schedule.
//
// For five-field expressions the search advances in one-minute increments (truncated
// to the minute). For six-field expressions it advances in one-second increments
// (truncated to the second). Returns the zero time if no match is found within the
// search bound (~8 years for minute schedules, ~2 years for second schedules).
func (s *Schedule) Next(from time.Time) time.Time {
	if s.hasSeconds {
		t := from.Add(time.Second).Truncate(time.Second)
		// 365.25 days/year × 2 years × 86400 seconds/day.
		for i := 0; i < 63_115_200; i++ {
			if s.matchesSec(t) {
				return t
			}
			t = t.Add(time.Second)
		}
		return time.Time{}
	}
	t := from.Add(time.Minute).Truncate(time.Minute)
	// 525960 minutes/year × 8 years as a generous upper bound.
	for i := 0; i < 525960*8; i++ {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s *Schedule) matches(t time.Time) bool {
	return s.minutes[t.Minute()] &&
		s.hours[t.Hour()] &&
		s.days[t.Day()] &&
		s.months[int(t.Month())] &&
		s.weekdays[int(t.Weekday())]
}

func (s *Schedule) matchesSec(t time.Time) bool {
	return s.seconds[t.Second()] &&
		s.minutes[t.Minute()] &&
		s.hours[t.Hour()] &&
		s.days[t.Day()] &&
		s.months[int(t.Month())] &&
		s.weekdays[int(t.Weekday())]
}

// parseCronField fills dst according to the cron field expression.
// Comma-separated parts are evaluated as a union.
func parseCronField(field string, min, max int, dst []bool) error {
	for _, part := range strings.Split(field, ",") {
		if err := parseCronPart(part, min, max, dst); err != nil {
			return err
		}
	}
	return nil
}

// parseCronPart handles a single cron field segment: *, N, N-M, */N, N-M/S.
func parseCronPart(part string, min, max int, dst []bool) error {
	step := 1

	// Extract step (e.g. */5 or 1-30/2).
	if idx := strings.Index(part, "/"); idx >= 0 {
		var err error
		step, err = strconv.Atoi(part[idx+1:])
		if err != nil || step < 1 {
			return fmt.Errorf("invalid step %q", part[idx+1:])
		}
		part = part[:idx]
	}

	var lo, hi int
	switch {
	case part == "*":
		lo, hi = min, max
	case strings.Contains(part, "-"):
		idx := strings.Index(part, "-")
		var err error
		lo, err = strconv.Atoi(part[:idx])
		if err != nil {
			return fmt.Errorf("invalid range start %q", part[:idx])
		}
		hi, err = strconv.Atoi(part[idx+1:])
		if err != nil {
			return fmt.Errorf("invalid range end %q", part[idx+1:])
		}
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value %q", part)
		}
		lo = n
		// N/step means "from N to max, step by step"; plain N means exactly N.
		if step > 1 {
			hi = max
		} else {
			hi = n
		}
	}

	if lo < min || hi > max || lo > hi {
		return fmt.Errorf("value %d-%d out of range [%d, %d]", lo, hi, min, max)
	}
	for v := lo; v <= hi; v += step {
		dst[v] = true
	}
	return nil
}
