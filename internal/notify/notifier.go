// Package notify provides a Notifier interface and concrete backends for
// delivering agent output to messaging platforms (Telegram, Signal).
// Secret resolution is handled by SecretRef in secret.go.
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// Message is the payload sent to a messaging backend after each successful agent run.
type Message struct {
	// AgentName is the name of the agent that produced the content.
	AgentName string
	// Content is the agent response text.
	Content string
	// Tags are the agent's metadata labels.
	Tags []string
	// Timestamp is the wall-clock time when the run completed.
	Timestamp time.Time
}

// Notifier sends agent output to a messaging backend.
type Notifier interface {
	// Send delivers msg to the configured backend.
	// Errors are non-fatal from the caller's perspective; the runner logs them
	// as warnings and continues processing remaining output routes.
	Send(ctx context.Context, msg Message) error
	// Name returns the backend config name used in log context.
	Name() string
}

// New constructs a Notifier from a NotifyBackendConfig.
// Returns an error if the backend type is unknown or if secret resolution fails
// at construction time (fail-closed — no partially-configured backends).
func New(cfg model.NotifyBackendConfig) (Notifier, error) {
	switch cfg.Type {
	case "telegram":
		return newTelegramNotifier(cfg)
	case "signal":
		return newSignalNotifier(cfg)
	default:
		return nil, fmt.Errorf("notify/New: unknown backend type %q (want: telegram, signal)", cfg.Type)
	}
}

// BuildMap constructs a name→Notifier map from a slice of NotifyBackendConfigs.
// Backends that fail to initialise are skipped with an error logged; the map
// contains only successfully initialised notifiers.
func BuildMap(cfgs []model.NotifyBackendConfig) (map[string]Notifier, []error) {
	out := make(map[string]Notifier, len(cfgs))
	var errs []error
	for _, cfg := range cfgs {
		n, err := New(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("notify/BuildMap %q: %w", cfg.Name, err))
			continue
		}
		out[cfg.Name] = n
	}
	return out, errs
}
