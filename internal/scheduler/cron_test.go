package scheduler

import (
	"testing"
	"time"
)

func TestParseSchedule_Once(t *testing.T) {
	s, err := ParseSchedule("once")
	if err != nil {
		t.Fatalf("ParseSchedule(once): %v", err)
	}
	if !s.Once() {
		t.Error("expected Once() = true")
	}
}

func TestParseSchedule_InvalidFieldCount(t *testing.T) {
	cases := []string{"* * * *", "* * * * * * *", "", "bad"}
	for _, expr := range cases {
		if _, err := ParseSchedule(expr); err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}

func TestParseSchedule_Wildcard(t *testing.T) {
	s, err := ParseSchedule("* * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	// Every minute/hour/day/month/weekday should match.
	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	next := s.Next(now)
	if next.Sub(now) != time.Minute {
		t.Errorf("Next() advanced %v, want 1m", next.Sub(now))
	}
}

func TestParseSchedule_HourlyOnTheHour(t *testing.T) {
	s, err := ParseSchedule("0 * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Minute() != 0 {
		t.Errorf("Next().Minute() = %d, want 0", next.Minute())
	}
	if next.Hour() != 10 {
		t.Errorf("Next().Hour() = %d, want 10", next.Hour())
	}
}

func TestParseSchedule_DailyAtMidnight(t *testing.T) {
	s, err := ParseSchedule("0 0 * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Hour() != 0 || next.Minute() != 0 {
		t.Errorf("Next() = %v, want next midnight", next)
	}
	if next.Day() != 2 {
		t.Errorf("Next().Day() = %d, want 2", next.Day())
	}
}

func TestParseSchedule_Step(t *testing.T) {
	s, err := ParseSchedule("*/15 * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	next := s.Next(from)
	// From 09:00, next should be 09:15.
	if next.Minute() != 15 {
		t.Errorf("Next().Minute() = %d, want 15", next.Minute())
	}
}

func TestParseSchedule_Range(t *testing.T) {
	s, err := ParseSchedule("0 9-17 * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("Next() = %v, want 09:00", next)
	}
}

func TestParseSchedule_List(t *testing.T) {
	s, err := ParseSchedule("0,30 * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 1, 1, 9, 15, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Minute() != 30 {
		t.Errorf("Next().Minute() = %d, want 30", next.Minute())
	}
}

func TestParseSchedule_OutOfRange(t *testing.T) {
	cases := []string{
		"60 * * * *", // minute 60 invalid
		"* 24 * * *", // hour 24 invalid
		"* * 0 * *",  // day 0 invalid (1-indexed)
		"* * * 13 *", // month 13 invalid
		"* * * * 7",  // weekday 7 invalid
	}
	for _, expr := range cases {
		if _, err := ParseSchedule(expr); err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}

func TestNext_AdvancesPastFrom(t *testing.T) {
	s, err := ParseSchedule("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	next := s.Next(now)
	if !next.After(now) {
		t.Errorf("Next() %v should be after from %v", next, now)
	}
}

func TestParseSchedule_SixField_Wildcard(t *testing.T) {
	s, err := ParseSchedule("* * * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	// Every second should match.
	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	next := s.Next(now)
	if next.Sub(now) != time.Second {
		t.Errorf("Next() advanced %v, want 1s", next.Sub(now))
	}
	if next.Second() != 1 {
		t.Errorf("Next().Second() = %d, want 1", next.Second())
	}
}

func TestParseSchedule_SixField_Every30s(t *testing.T) {
	s, err := ParseSchedule("*/30 * * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	// From :00, next should be :30.
	from := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Second() != 30 {
		t.Errorf("Next().Second() = %d, want 30", next.Second())
	}
	if next.Sub(from) != 30*time.Second {
		t.Errorf("gap = %v, want 30s", next.Sub(from))
	}
	// From :30, next should be :00 of the following minute.
	next2 := s.Next(next)
	if next2.Second() != 0 || next2.Minute() != 1 {
		t.Errorf("second next = %v, want 09:01:00", next2)
	}
	if next2.Sub(next) != 30*time.Second {
		t.Errorf("second gap = %v, want 30s", next2.Sub(next))
	}
}

func TestParseSchedule_SixField_Offset15s(t *testing.T) {
	// 15/30 fires at seconds 15 and 45.
	s, err := ParseSchedule("15/30 * * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	from := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	next := s.Next(from)
	if next.Second() != 15 {
		t.Errorf("Next().Second() = %d, want 15", next.Second())
	}
	next2 := s.Next(next)
	if next2.Second() != 45 {
		t.Errorf("second Next().Second() = %d, want 45", next2.Second())
	}
	if next2.Sub(next) != 30*time.Second {
		t.Errorf("gap = %v, want 30s", next2.Sub(next))
	}
}

func TestParseSchedule_SixField_HasSecondsFlag(t *testing.T) {
	s5, _ := ParseSchedule("* * * * *")
	if s5.HasSeconds() {
		t.Error("5-field: HasSeconds() should be false")
	}
	s6, _ := ParseSchedule("* * * * * *")
	if !s6.HasSeconds() {
		t.Error("6-field: HasSeconds() should be true")
	}
}

func TestParseSchedule_SixField_NextIsAfterFrom(t *testing.T) {
	s, err := ParseSchedule("*/30 * * * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	next := s.Next(now)
	if !next.After(now) {
		t.Errorf("Next() %v should be after from %v", next, now)
	}
}

func TestParseSchedule_SixField_OutOfRange(t *testing.T) {
	cases := []string{
		"60 * * * * *", // second 60 invalid
		"* 60 * * * *", // minute 60 invalid
		"* * 24 * * *", // hour 24 invalid
	}
	for _, expr := range cases {
		if _, err := ParseSchedule(expr); err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}
