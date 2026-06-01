package curing

import (
	"testing"

	"github.com/tgpski/leather/internal/model"
)

func route(name, source, eventType, curing string) model.TanneryRoute {
	return model.TanneryRoute{
		Name:     name,
		Match:    model.RouteMatch{Source: source, EventType: eventType},
		HideKind: source + "." + eventType,
		Curing:   curing,
		Queue:    "test-queue",
	}
}

func TestRouter_Match_Exact(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pr_review", "pr-review-curing"),
	})
	got, ok := r.Match("github", "pr_review")
	if !ok {
		t.Fatal("expected match")
	}
	if got.Name != "r1" {
		t.Errorf("Name: got %q", got.Name)
	}
}

func TestRouter_Match_WildcardEventType(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "", "any-curing"),
	})
	got, ok := r.Match("github", "push")
	if !ok {
		t.Fatal("expected wildcard match")
	}
	if got.Name != "r1" {
		t.Errorf("Name: got %q", got.Name)
	}
}

func TestRouter_Match_WrongSource(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pr_review", "curing"),
	})
	_, ok := r.Match("gitlab", "pr_review")
	if ok {
		t.Error("expected no match for wrong source")
	}
}

func TestRouter_Match_WrongEventType(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pr_review", "curing"),
	})
	_, ok := r.Match("github", "push")
	if ok {
		t.Error("expected no match for wrong event type")
	}
}

func TestRouter_Match_FirstMatchWins(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("first", "github", "pr_review", "curing-a"),
		route("second", "github", "pr_review", "curing-b"),
	})
	got, ok := r.Match("github", "pr_review")
	if !ok {
		t.Fatal("expected match")
	}
	if got.Name != "first" {
		t.Errorf("expected first route to win, got %q", got.Name)
	}
}

func TestRouter_Match_WildcardBeforeSpecific(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("wildcard", "github", "", "wildcard-curing"),
		route("specific", "github", "pr_review", "specific-curing"),
	})
	got, ok := r.Match("github", "pr_review")
	if !ok {
		t.Fatal("expected match")
	}
	// Wildcard is first, so it wins.
	if got.Name != "wildcard" {
		t.Errorf("expected wildcard to win, got %q", got.Name)
	}
}

func TestRouter_Match_NoRoutes(t *testing.T) {
	r := NewRouter(nil)
	_, ok := r.Match("github", "pr_review")
	if ok {
		t.Error("expected no match from empty router")
	}
}

func TestRouter_Routes_ReturnsCopy(t *testing.T) {
	original := []model.TanneryRoute{
		route("r1", "github", "pr_review", "curing"),
	}
	r := NewRouter(original)
	got := r.Routes()
	if len(got) != 1 {
		t.Fatalf("expected 1 route, got %d", len(got))
	}
	// Mutating the returned slice must not affect the router.
	got[0].Name = "mutated"
	again := r.Routes()
	if again[0].Name != "r1" {
		t.Error("Routes() returned a reference, not a copy")
	}
}

func TestRouter_MatchAll_ReturnsAllMatches(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pull_request", "pr-metadata"),
		route("r2", "github", "pull_request", "pr-diff"),
		route("r3", "github", "pull_request", "pr-history"),
	})
	got := r.MatchAll("github", "pull_request")
	if len(got) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(got))
	}
	if got[0].Name != "r1" || got[1].Name != "r2" || got[2].Name != "r3" {
		t.Errorf("unexpected route order: %v", got)
	}
}

func TestRouter_MatchAll_NoMatch(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pull_request", "curing"),
	})
	got := r.MatchAll("gitlab", "pull_request")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestRouter_MatchAll_WildcardAndSpecificBothMatch(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("wildcard", "github", "", "any-curing"),
		route("specific", "github", "pull_request", "pr-curing"),
	})
	got := r.MatchAll("github", "pull_request")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(got))
	}
	if got[0].Name != "wildcard" || got[1].Name != "specific" {
		t.Errorf("unexpected names: %v", got)
	}
}

func TestRouter_MatchAll_PartialMatch(t *testing.T) {
	r := NewRouter([]model.TanneryRoute{
		route("r1", "github", "pull_request", "curing-a"),
		route("r2", "github", "push", "curing-b"),
		route("r3", "github", "pull_request", "curing-c"),
	})
	got := r.MatchAll("github", "pull_request")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(got))
	}
	if got[0].Name != "r1" || got[1].Name != "r3" {
		t.Errorf("unexpected routes: %v", got)
	}
}

func TestRouter_MatchAll_EmptyRouter(t *testing.T) {
	r := NewRouter(nil)
	got := r.MatchAll("github", "pull_request")
	if got != nil {
		t.Errorf("expected nil from empty router, got %v", got)
	}
}
