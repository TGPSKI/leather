// Package sources maps runtime signals to DevTools bus events.
package sources

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/devtools/bus"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/runner"
)

// Deps are optional dependencies for wiring.
type Deps struct {
	Now func() time.Time
}

// Wiring exposes callbacks that publish source events onto the bus.
type Wiring struct {
	bus *bus.Bus
	now func() time.Time
}

// Wire binds source callbacks to the given bus.
func Wire(b *bus.Bus, deps Deps) *Wiring {
	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Wiring{bus: b, now: nowFn}
}

// PublishHTTP emits an HTTP-related event kind with payload.
func (w *Wiring) PublishHTTP(kind string, payload map[string]any) uint64 {
	if w == nil || w.bus == nil || kind == "" {
		return 0
	}
	return w.bus.Publish(bus.RedactEvent(bus.Event{
		At:         w.now().Unix(),
		Kind:       kind,
		Source:     "http",
		EntityKind: "event",
		Payload:    toRaw(payload),
	}))
}

// PublishRunner emits a runner progress event for a specific agent.
func (w *Wiring) PublishRunner(curingName, agentName string, ev runner.ProgressEvent) uint64 {
	if w == nil || w.bus == nil {
		return 0
	}
	payload := map[string]any{
		"curing":         curingName,
		"agent":          agentName,
		"progress_kind":  ev.Kind,
		"round":          ev.Round,
		"tool":           ev.Tool,
		"tool_type":      ev.ToolType,
		"result_bytes":   ev.ResultBytes,
		"result_preview": ev.ResultPreview,
		"error":          ev.Error,
		"skill":          ev.Skill,
		"var_key":        ev.VarKey,
		"var_val":        ev.VarVal,
		"hide_id":        ev.HideID,
		"total_pages":    ev.TotalPages,
	}
	if ev.Args != "" {
		payload["args"] = ev.Args
	}
	if ev.Prompt != "" {
		payload["prompt"] = ev.Prompt
	}
	if ev.Response != "" {
		payload["response"] = ev.Response
	}
	if ev.PromptTokens != 0 || ev.CompletionTokens != 0 || ev.TotalTokens != 0 {
		payload["prompt_tokens"] = ev.PromptTokens
		payload["completion_tokens"] = ev.CompletionTokens
		payload["total_tokens"] = ev.TotalTokens
	}
	if ev.Context != nil {
		payload["context"] = map[string]any{
			"agent_name":  ev.Context.AgentName,
			"turn":        ev.Context.Turn,
			"round":       ev.Context.Round,
			"tool_names":  ev.Context.ToolNames,
			"max_tokens":  ev.Context.MaxTokens,
			"temperature": ev.Context.Temperature,
		}
	}

	kind := "agent.turn.event"
	switch ev.Kind {
	case "call":
		kind = "tool.call"
	case "result":
		kind = "tool.result"
	case "extract":
		kind = "agent.turn.extract"
	case "hide":
		kind = "hide.cut"
	case "context":
		kind = "agent.turn.context"
	case "skill_start":
		kind = "agent.turn.skill"
	case "agent":
		kind = "agent.response"
	}

	return w.bus.Publish(bus.RedactEvent(bus.Event{
		At:         w.now().Unix(),
		Kind:       kind,
		Source:     "runner",
		EntityKind: "agent",
		EntityID:   agentName,
		Payload:    toRaw(payload),
		Err:        ev.Error,
	}))
}

// PublishScheduleFire emits a schedule.fire event when a cron-scheduled agent
// run begins. Call it once per tick, before the runner executes the agent.
func (w *Wiring) PublishScheduleFire(agentName, scheduleExpr string) uint64 {
	if w == nil || w.bus == nil {
		return 0
	}
	return w.bus.Publish(bus.RedactEvent(bus.Event{
		At:         w.now().Unix(),
		Kind:       "schedule.fire",
		Source:     "scheduler",
		EntityKind: "agent",
		EntityID:   agentName,
		Payload: toRaw(map[string]any{
			"agent":    agentName,
			"schedule": scheduleExpr,
		}),
	}))
}

// PublishTannery emits a curing worker pipeline event.
func (w *Wiring) PublishTannery(ev curing.TanneryEvent) uint64 {
	if w == nil || w.bus == nil {
		return 0
	}
	kind := "queue.event"
	switch ev.Kind {
	case "webhook":
		kind = "webhook.received"
	case "enqueue":
		kind = "queue.enqueue"
	case "dequeue":
		kind = "queue.dequeue"
	case "retry":
		kind = "queue.retry"
	case "dlq":
		kind = "queue.dlq"
	}

	payload := map[string]any{
		"curing":       ev.Curing,
		"queue":        ev.Queue,
		"dest_queue":   ev.DestQueue,
		"hide_id":      ev.HideID,
		"hide_kind":    ev.HideKind,
		"item_id":      ev.ItemID,
		"attempt":      ev.Attempt,
		"error":        ev.Err,
		"source":       ev.Source,
		"webhook_name": ev.WebhookName,
	}
	return w.bus.Publish(bus.RedactEvent(bus.Event{
		At:         w.now().Unix(),
		Kind:       kind,
		Source:     "curing",
		EntityKind: "queue_item",
		EntityID:   ev.ItemID,
		Payload:    toRaw(payload),
		Err:        ev.Err,
	}))
}

// PublishQueueRun emits a queue.run event when the scheduler dequeues an item
// and begins an agent run. It carries queue context without exposing payload values.
func (w *Wiring) PublishQueueRun(agentName, queueName string, item model.QueueItem) uint64 {
	if w == nil || w.bus == nil {
		return 0
	}
	keys := make([]string, 0, len(item.Payload))
	for k := range item.Payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return w.bus.Publish(bus.RedactEvent(bus.Event{
		At:         w.now().Unix(),
		Kind:       "queue.run",
		Source:     "scheduler",
		EntityKind: "queue_item",
		EntityID:   item.ID,
		Payload: toRaw(map[string]any{
			"agent":        agentName,
			"queue":        queueName,
			"item_id":      item.ID,
			"hide_id":      item.HideID,
			"hide_kind":    item.HideKind,
			"attempt":      item.AttemptCount,
			"payload_keys": keys,
		}),
	}))
}

func toRaw(v any) json.RawMessage {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return encoded
}
