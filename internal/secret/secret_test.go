package secret

import (
	"context"
	"testing"
)

func TestResolve_Inline(t *testing.T) {
	got, err := Resolve(context.Background(), Ref{Value: "sk-abc"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "sk-abc" {
		t.Fatalf("got %q want %q", got, "sk-abc")
	}
}

func TestResolve_EnvFallback(t *testing.T) {
	t.Setenv("LEATHER_TEST_SECRET", "from-env")
	got, err := Resolve(context.Background(), Ref{Env: "LEATHER_TEST_SECRET"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("got %q want %q", got, "from-env")
	}
}

func TestResolve_Empty(t *testing.T) {
	got, err := Resolve(context.Background(), Ref{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestResolve_MissingEnv(t *testing.T) {
	// Variable name that should not exist in the test environment.
	t.Setenv("LEATHER_DEFINITELY_UNSET_XYZ", "")
	_, err := Resolve(context.Background(), Ref{Env: "LEATHER_DEFINITELY_UNSET_XYZ"})
	if err == nil {
		t.Fatal("expected error for unresolved Ref, got nil")
	}
}

func TestResolve_PassFallsBackToEnv(t *testing.T) {
	// Pass path that almost certainly does not exist; Env should win.
	t.Setenv("LEATHER_TEST_FALLBACK", "fallback-value")
	got, err := Resolve(context.Background(), Ref{
		Pass: "leather/test/__definitely_missing__",
		Env:  "LEATHER_TEST_FALLBACK",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "fallback-value" {
		t.Fatalf("got %q want %q", got, "fallback-value")
	}
}

func TestRef_IsZero(t *testing.T) {
	if !(Ref{}).IsZero() {
		t.Error("empty Ref should be zero")
	}
	if (Ref{Env: "X"}).IsZero() {
		t.Error("Ref with Env should not be zero")
	}
}
