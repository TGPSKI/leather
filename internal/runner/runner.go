// Package runner executes a single agent turn, including multi-round tool calling.
// It is the canonical execution path used by both the scheduler and the run command.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/tgpski/leather/internal/cache"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// DefaultToolRounds is the tool call cycle limit when neither the agent nor
// Config.MaxToolRounds specifies a value.
const DefaultToolRounds = 5

var hidePageHeaderRE = regexp.MustCompile(`\[HIDE id=([^\s]+)\s+[^\]]*page=([0-9]+)/([0-9]+)`) //nolint:gochecknoglobals // compile-time regexp is immutable and safe

// Runner executes agents with optional tool calling support.
type Runner struct {
	// Client is the LLM backend used for completions.
	Client session.LLMClient
	// Registry provides tool definitions looked up from agent skill lists.
	// May be nil; nil means no tool calling is available.
	Registry *tool.Registry
	// Log is the structured logger for this runner instance.
	Log *logging.Logger
	// MaxToolRounds is the global default tool call cycle limit.
	// Agents may override with Agent.ToolRounds.
	MaxToolRounds int
	// Cache is the sha256-keyed file cache for agent responses.
	// May be nil; nil means caching is disabled globally.
	Cache *cache.FileCache
	// QueueMgr is used for output routing to named queues.
	// May be nil; nil means queue output routing is unavailable.
	QueueMgr *queue.Manager
	// Notifiers maps backend name to a ready Notifier for type=notify output routes.
	// May be nil or empty; missing backend names are logged as warnings.
	Notifiers map[string]notify.Notifier
	// MCPRegistry is the registry of running MCP server clients.
	// May be nil; nil means mcp-type tools are unavailable.
	MCPRegistry *mcp.Registry
	// HideBuffer is the in-process store for large tool outputs.
	// When non-nil, tool results exceeding a threshold are intercepted and
	// paged via the hide/cut API instead of being delivered in full.
	// May be nil; nil means large-output buffering is disabled.
	HideBuffer *hide.HideBuffer
	// ProgressFn, when non-nil, is called for each tool call and result event.
	// It is called synchronously in the run goroutine; implementations must not block.
	ProgressFn func(ProgressEvent)
	// DebugContextFn, when non-nil, receives the exact message window and tool
	// exposure immediately before each LLM completion call. Intended for
	// operator-visible debugging of "what did the model actually see?"
	DebugContextFn func(ContextSnapshot)
	// ForceTextAfterHide, when true, strips all tools from the next LLM call
	// whenever a hide navigation tool (hide_next, hide_jump, hide_search) executes
	// within a round. This prevents the model from chaining additional tool calls
	// immediately after paging — the only valid next action is a text reflection.
	ForceTextAfterHide bool
	// NoToolsForFirstTurn, when true, suppresses all tools for the very first
	// user turn (index 0). Use with ForceTextAfterHide in reflection mode so
	// the model is forced to reflect on the first page content rather than
	// immediately calling hide_next before being asked to.
	NoToolsForFirstTurn bool
	// NoToolsForLastTurn, when true, suppresses all tools for the final user
	// turn. Use in reflection mode so the final structured output is plain text
	// after all pages have been read.
	NoToolsForLastTurn bool
	// Vars holds named values that replace {{key}} placeholders in the agent's
	// system prompt and user prompt before the first LLM call. Populated by
	// leather run when skills declare parameters.
	Vars      map[string]string
	hidePages map[string]int
}

// ContextSnapshot is a point-in-time view of the exact input sent to one LLM
// completion call.
type ContextSnapshot struct {
	AgentName   string
	Turn        int
	Round       int
	Messages    []model.Message
	ToolNames   []string
	ExtraBody   map[string]any
	MaxTokens   int
	Temperature float64
}

// ProgressEvent describes a single tool-call activity during an agent run.
type ProgressEvent struct {
	// Kind is "call" when a tool is invoked, "result" when it returns,
	// "system" when the system prompt is added, "user" when a user prompt
	// is added, "skill_start" when a skill is loaded, "context" when the
	// exact LLM input window is captured, or "extract" when a
	// value is captured from a tool result into the turn-variable map.
	Kind string
	// Round is the tool-call cycle index (0-based).
	Round int
	// Tool is the tool name.
	Tool string
	// ToolType is the executor type: "http", "mcp", etc.
	ToolType string
	// ResultBytes is the byte length of the tool result content (Kind=="result" only).
	ResultBytes int
	// ResultPreview is a truncated, human-readable snapshot of the tool result
	// content as it enters the model's context (Kind=="result" only). Capped to
	// keep pretty output tractable; never used for behaviour, only display.
	ResultPreview string
	// Error is non-empty when tool execution failed (Kind=="result" only).
	Error string
	// Args is the JSON-encoded tool arguments (Kind=="call" only).
	Args string
	// Prompt holds the prompt text (Kind=="system" or Kind=="user" only).
	Prompt string
	// Skill is the skill name (Kind=="skill_start" only).
	Skill string
	// VarKey and VarVal are the extracted key and value (Kind=="extract" only).
	VarKey string
	VarVal string
	// Context holds the exact pre-completion message snapshot (Kind=="context" only).
	Context *ContextSnapshot
	// HideID is the buffer ID of the created hide (Kind=="hide" only).
	HideID string
	// TotalPages is the total page count for the hide (Kind=="hide" only).
	TotalPages int
	// Response holds the assistant text reply (Kind=="agent" only).
	Response string
	// PromptTokens, CompletionTokens, TotalTokens record usage for the turn
	// that produced this response (Kind=="agent" only).
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Run executes a single agent turn. It:
//  1. Collects tool definitions from the agent's skill list.
//  2. Builds the session with system and user prompts (augmented by skill prompts).
//  3. Calls the model in a loop, executing any tool calls and feeding results back.
//  4. Returns a RunRecord with turn content, token usage, and timing.
func (r *Runner) Run(ctx context.Context, a model.Agent, budget model.TokenBudget) (rec model.RunRecord, runErr error) {
	startTs := time.Now()

	r.Log.Info("executing agent", "agent", a.Name)
	r.Log.Debug("agent config", "agent", a.Name, "model", a.Model,
		"timeout", a.Timeout, "temperature", a.Temperature,
		"max_tokens", budget.MaxTokens, "completion_reserve", budget.CompletionReserve)

	// Establish per-run timeout once, outside the turn/round loops. The same
	// deadline applies to every LLM call AND every tool call in this run, so
	// `timeout: 30s` bounds the total run wall-clock at 30s, not 30s per round.
	// runHook receives the parent ctx so post-run hooks survive a run timeout.
	parentCtx := ctx
	if a.Timeout > 0 {
		var runCancel context.CancelFunc
		ctx, runCancel = context.WithTimeout(ctx, a.Timeout)
		defer runCancel()
	}

	// Fire pre-run hook before any LLM work.
	r.runHook(parentCtx, a.Hooks.PreRun, a.Name, "pre_run")

	// Fire post hooks when the function returns.
	defer func() {
		if runErr != nil {
			r.runHook(parentCtx, a.Hooks.PostError, a.Name, "post_error")
		} else {
			r.runHook(parentCtx, a.Hooks.PostSuccess, a.Name, "post_success")
		}
	}()

	baseSkills := append([]string(nil), a.Skills...)
	baseToolsets := append([]string(nil), a.Toolsets...)
	baseTools := r.resolveScopeTools(baseSkills, baseToolsets, nil)
	if r.HideBuffer == nil && toolsNeedBuffer(baseTools) {
		r.HideBuffer = hide.NewHideBuffer(0)
	}
	if r.HideBuffer != nil && r.HideBuffer.NeedsPaging() {
		baseTools = append(baseTools, hide.ToolDefs()...)
	}
	sysPrompt := joinPromptBlocks(a.SystemPrompt, r.skillPromptAppend(baseSkills))

	// Build turn-level variable map from Vars. Starts as a copy of the static
	// lifecycle parameters; skill extract rules extend it as tool results arrive.
	// UserPrompts are substituted lazily per turn so extracted vars are visible.
	turnVars := make(map[string]string, len(r.Vars))
	for k, v := range r.Vars {
		turnVars[k] = v
	}
	if len(turnVars) > 0 {
		sysPrompt = applyVars(sysPrompt, turnVars)
		a.UserPrompt = applyVars(a.UserPrompt, turnVars)
	}

	// Pre-run cache check (after skill prompt appends are resolved).
	var cacheKey string
	if r.Cache != nil && a.Cache.Enabled {
		// Multi-turn agents (a.UserPrompts) must include every turn in the
		// cache key; otherwise two agents that share a.UserPrompt but differ
		// in later turns would collide.
		userPromptForKey := a.UserPrompt
		if len(a.UserPrompts) > 0 {
			userPromptForKey = strings.Join(a.UserPrompts, "\x01")
		}
		cacheKey = cache.AgentRunKey(a.Name, sysPrompt, userPromptForKey, a.Model)
		if cached, ok := r.Cache.Get(cacheKey); ok {
			r.Log.Info("cache hit", "agent", a.Name)
			return model.RunRecord{
				AgentName:    a.Name,
				Time:         model.RunTime{StartTs: startTs.Unix(), DurationMs: 0},
				Status:       model.JobStatusSuccess,
				SystemPrompt: a.SystemPrompt,
				Turns:        []model.Turn{{Prompt: a.UserPrompt, Response: cached}},
			}, nil
		}
	}

	sess := session.New(budget, a.Model, r.Client)

	if sysPrompt != "" {
		r.Log.Debug("adding system prompt", "agent", a.Name, "chars", len(sysPrompt))
		if err := sess.Add(ctx, model.Message{Role: "system", Content: sysPrompt}); err != nil {
			return r.errorRecord(a, startTs, err), err
		}
		if r.ProgressFn != nil {
			r.ProgressFn(ProgressEvent{Kind: "system", Prompt: sysPrompt})
		}
	}

	rounds := a.ToolRounds
	if rounds <= 0 {
		rounds = r.MaxToolRounds
	}
	if rounds <= 0 {
		rounds = DefaultToolRounds
	}
	// When there are no tools, cap at 1 round (no tool loop needed).
	if !r.agentHasAnyTools(a, baseTools) {
		rounds = 1
	}

	// Build the ordered list of user prompts for this run.
	// UserPrompts (from prompts: list) takes precedence over the single UserPrompt.
	userPrompts := a.UserPrompts
	if len(userPrompts) == 0 && a.UserPrompt != "" {
		userPrompts = []string{a.UserPrompt}
	}
	// Guarantee at least one LLM call even when no user prompt is configured,
	// preserving the original behaviour (system-prompt-only agents still run).
	if len(userPrompts) == 0 {
		userPrompts = []string{""}
	}

	var totalTokens model.RunTokens
	var turns []model.Turn
	var lastResp model.LLMResponse

	for i, userPrompt := range userPrompts {
		// Apply turn-level vars (may include values extracted from previous tool calls).
		userPrompt = applyVars(userPrompt, turnVars)

		turnSkills, turnToolsets, turnToolNames, turnDeclared := turnScopeFor(a, i)
		turnTools := baseTools
		if turnDeclared {
			turnTools = r.resolveScopeTools(turnSkills, turnToolsets, turnToolNames)
			if r.HideBuffer == nil && toolsNeedBuffer(turnTools) {
				r.HideBuffer = hide.NewHideBuffer(0)
			}
			if r.HideBuffer != nil {
				turnTools = append(turnTools, hide.ToolDefs()...)
			}
		}
		if turnPrompt := r.skillPromptAppend(turnSkills); turnPrompt != "" {
			if err := sess.Add(ctx, model.Message{Role: "system", Content: turnPrompt}); err != nil {
				return r.errorRecord(a, startTs, err), err
			}
			if r.ProgressFn != nil {
				r.ProgressFn(ProgressEvent{Kind: "system", Prompt: turnPrompt})
			}
		}

		// In NoToolsForFirstTurn mode suppress tools for turn 0 so the model
		// must reflect on the delivered page rather than calling hide_next.
		if r.NoToolsForFirstTurn && i == 0 {
			turnTools = nil
		}
		if r.NoToolsForLastTurn && i == len(userPrompts)-1 {
			turnTools = nil
		}

		toolByName := make(map[string]model.ToolDefinition, len(turnTools))
		for _, t := range turnTools {
			toolByName[t.Name] = t
		}

		opts := session.CompletionOptions{
			MaxTokens:   budget.CompletionReserve,
			Temperature: a.Temperature,
			Tools:       turnTools,
		}
		if len(turnTools) > 0 {
			opts.ExtraBody = map[string]any{"parallel_tool_calls": false}
		}

		if userPrompt != "" {
			r.Log.Debug("adding user prompt", "agent", a.Name, "chars", len(userPrompt))
			if r.ProgressFn != nil {
				r.ProgressFn(ProgressEvent{Kind: "user", Prompt: userPrompt})
			}
			r.recordHidePages(userPrompt)
			if err := sess.Add(ctx, model.Message{Role: "user", Content: userPrompt}); err != nil {
				return r.errorRecord(a, startTs, err), err
			}
		}

		for round := 0; round < rounds; round++ {
			reflectionTextTurn := r.ForceTextAfterHide && len(opts.Tools) == 0
			// The run-level deadline (set at function entry) bounds this call.
			callCtx := ctx
			if r.DebugContextFn != nil {
				r.DebugContextFn(ContextSnapshot{
					AgentName:   a.Name,
					Turn:        i,
					Round:       round,
					Messages:    cloneMessages(sess.Messages()),
					ToolNames:   toolNames(opts.Tools),
					ExtraBody:   cloneMap(opts.ExtraBody),
					MaxTokens:   opts.MaxTokens,
					Temperature: opts.Temperature,
				})
			}
			r.Log.Debug("calling LLM", "agent", a.Name, "model", a.Model,
				"messages", len(sess.Messages()), "round", round)

			resp, err := r.Client.Complete(callCtx, a.Model, sess.Messages(), opts)
			if err != nil {
				wErr := fmt.Errorf("runner/Run %s round %d: %w", a.Name, round, err)
				return r.errorRecord(a, startTs, wErr), wErr
			}

			totalTokens.Prompt += resp.PromptTokens
			totalTokens.Response += resp.CompletionTokens
			totalTokens.Total += resp.TotalTokens
			lastResp = resp

			if len(resp.ToolCalls) == 0 {
				// Final text response — record the turn and continue to next prompt.
				r.Log.Info("agent completed", "agent", a.Name,
					"tokens", resp.TotalTokens, "finish_reason", resp.FinishReason)
				r.Log.Info("agent response content", "agent", a.Name, "content", resp.Content)
				if r.ProgressFn != nil {
					r.ProgressFn(ProgressEvent{
						Kind:             "agent",
						Round:            round,
						Response:         resp.Content,
						PromptTokens:     resp.PromptTokens,
						CompletionTokens: resp.CompletionTokens,
						TotalTokens:      resp.TotalTokens,
					})
				}
				if userPrompt != "" || resp.Content != "" {
					turns = append(turns, model.Turn{
						Prompt:           userPrompt,
						Response:         resp.Content,
						PromptTokens:     resp.PromptTokens,
						CompletionTokens: resp.CompletionTokens,
						TotalTokens:      resp.TotalTokens,
					})
				}
				// Add the assistant response to session so subsequent prompts see it.
				if err := sess.Add(ctx, model.Message{Role: "assistant", Content: resp.Content}); err != nil {
					return r.errorRecord(a, startTs, err), fmt.Errorf("runner/Run %s: session add assistant: %w", a.Name, err)
				}
				if reflectionTextTurn {
					if _, err := sess.CompactLatestHidePage(ctx, resp.Content); err != nil {
						return r.errorRecord(a, startTs, err), fmt.Errorf("runner/Run %s: compact hide page: %w", a.Name, err)
					}
				}
				break
			}

			// The model requested tool calls — validate, execute, and feed results back.
			r.Log.Info("tool calls requested", "agent", a.Name, "count", len(resp.ToolCalls))

			// Record the assistant message with its tool call requests.
			if err := sess.Add(ctx, model.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			}); err != nil {
				return r.errorRecord(a, startTs, err), fmt.Errorf("runner/Run %s: session add assistant tool calls: %w", a.Name, err)
			}

			hideToolSucceeded := false
			for _, tc := range resp.ToolCalls {
				def, ok := toolByName[tc.Name]
				if !ok {
					// Tool not in the current turn scope — reject to prevent execution of
					// globally registered tools not declared by this agent/turn, and to guard
					// against prompt injection naming arbitrary registered tools.
					wErr := fmt.Errorf("runner/Run %s: tool %q not in current tool scope (possible prompt injection)", a.Name, tc.Name)
					r.Log.Error("out-of-scope tool call rejected", "agent", a.Name, "tool", tc.Name)
					return r.errorRecord(a, startTs, wErr), wErr
				}
				r.Log.Info("executing tool", "agent", a.Name, "tool", tc.Name)
				if r.ProgressFn != nil {
					r.ProgressFn(ProgressEvent{Kind: "call", Round: round, Tool: tc.Name, ToolType: def.Type, Args: marshalArgs(tc.Arguments)})
				}
				var result model.ToolResult
				if def.Type == "hide" {
					result = r.executeHideTool(def.Name, tc.ID, tc.Arguments)
					if result.Error == "" {
						hideToolSucceeded = true
					}
				} else {
					result = (&tool.Executor{MCP: r.MCPRegistry}).Execute(ctx, def, tc.Arguments)
				}
				if result.Error == "" && def.Buffer {
					if r.HideBuffer == nil {
						result.Error = fmt.Sprintf("tool %s requested buffering but no hide buffer is configured", tc.Name)
					} else {
						stored := r.HideBuffer.Store(tc.Name, result.Content)
						cut, cutErr := r.HideBuffer.Cut(stored.ID, 1)
						if cutErr != nil {
							result.Error = cutErr.Error()
						} else {
							result.Content = cut.Format()
							r.recordHidePage(cut.HideID, cut.PageNumber)
							if r.ProgressFn != nil {
								r.ProgressFn(ProgressEvent{Kind: "hide", Round: round, Tool: tc.Name, ToolType: def.Type, HideID: stored.ID, TotalPages: cut.TotalPages})
							}
							if cut.TotalPages > 1 {
								for _, hideDef := range hide.ToolDefs() {
									if _, exists := toolByName[hideDef.Name]; exists {
										continue
									}
									opts.Tools = append(opts.Tools, hideDef)
									toolByName[hideDef.Name] = hideDef
								}
								if opts.ExtraBody == nil {
									opts.ExtraBody = map[string]any{"parallel_tool_calls": false}
								}
							}
						}
					}
				}
				if result.Error != "" {
					r.Log.Error("tool execution failed", "agent", a.Name, "tool", tc.Name, "error", result.Error)
					if r.ProgressFn != nil {
						r.ProgressFn(ProgressEvent{Kind: "result", Round: round, Tool: tc.Name, ToolType: def.Type, Error: result.Error})
					}
					if def.Type == "hide" || def.Buffer {
						wErr := fmt.Errorf("runner/Run %s: hide tool %s failed: %s", a.Name, tc.Name, result.Error)
						if def.Buffer {
							wErr = fmt.Errorf("runner/Run %s: buffered tool %s failed: %s", a.Name, tc.Name, result.Error)
						}
						return r.errorRecord(a, startTs, wErr), wErr
					}
				} else {
					r.Log.Debug("tool result", "agent", a.Name, "tool", tc.Name, "bytes", len(result.Content))
					if r.ProgressFn != nil {
						r.ProgressFn(ProgressEvent{Kind: "result", Round: round, Tool: tc.Name, ToolType: def.Type, ResultBytes: len(result.Content), ResultPreview: previewToolResult(result.Content)})
					}
				}
				content := result.Content
				if result.Error != "" {
					content = "error: " + result.Error
				} else {
					r.recordHidePages(content)
				}
				if err := sess.Add(ctx, model.Message{
					Role:       "tool",
					Content:    content,
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
				}); err != nil {
					return r.errorRecord(a, startTs, err), fmt.Errorf("runner/Run %s: session add tool result %s: %w", a.Name, tc.Name, err)
				}
				// Run skill extraction patterns against successful tool results.
				// Matched values extend turnVars so subsequent turn prompts can
				// reference them via {{key}} substitution.
				if result.Error == "" {
					if r.ProgressFn != nil {
						// Snapshot before so we can report only newly extracted values.
						before := make(map[string]string, len(turnVars))
						for k, v := range turnVars {
							before[k] = v
						}
						r.Registry.ApplyExtractors(tc.Name, result.Content, turnVars)
						for k, v := range turnVars {
							if before[k] != v {
								r.ProgressFn(ProgressEvent{Kind: "extract", Tool: tc.Name, VarKey: k, VarVal: v})
							}
						}
					} else {
						r.Registry.ApplyExtractors(tc.Name, result.Content, turnVars)
					}
				}
			}

			// When ForceTextAfterHide is set and a hide navigation tool ran this
			// round, clear tools so the follow-up call is forced to produce text.
			// The reflection instruction comes from the Cut header (ReflectionHint)
			// and the system preamble — no separate user message is injected here,
			// avoiding mid-round user events that break the timeline renderer.
			if r.ForceTextAfterHide && hideToolSucceeded {
				opts.Tools = nil
				opts.ExtraBody = nil
				toolByName = map[string]model.ToolDefinition{}
			}

			if round == rounds-1 {
				// Max rounds reached without a text response.
				wErr := fmt.Errorf("runner/Run %s: max tool rounds (%d) reached without text response", a.Name, rounds)
				return r.errorRecord(a, startTs, wErr), wErr
			}
		}
	}

	rec = model.RunRecord{
		AgentName:    a.Name,
		Time:         model.RunTime{StartTs: startTs.Unix(), DurationMs: time.Since(startTs).Milliseconds()},
		Status:       model.JobStatusSuccess,
		SystemPrompt: a.SystemPrompt,
		Tokens:       totalTokens,
		Turns:        turns,
	}
	if len(rec.Turns) > 0 {
		rec.LastResponse = rec.Turns[len(rec.Turns)-1].Response
	}
	_ = lastResp // used via turns above

	// Post-run cache write.
	if r.Cache != nil && a.Cache.Enabled && cacheKey != "" && lastResp.Content != "" {
		if err := r.Cache.Set(cacheKey, lastResp.Content, a.Cache.TTL); err != nil {
			r.Log.Warn("cache write failed", "agent", a.Name, "error", err)
		}
	}

	// Output routing: send response content to configured destinations.
	if len(a.OutputRoutes) > 0 {
		r.routeOutput(ctx, a, lastResp.Content, startTs)
	}

	return rec, nil
}

func (r *Runner) resolveScopeTools(skillNames, toolsetNames, toolNames []string) []model.ToolDefinition {
	if r.Registry == nil {
		return nil
	}
	tools := r.Registry.ResolveTools(skillNames, toolsetNames, toolNames)
	// Enrich MCP-type tools with parameter schemas fetched from the server.
	if r.MCPRegistry != nil {
		for i, t := range tools {
			if t.Type == "mcp" && t.Parameters == nil {
				if schema := r.MCPRegistry.ToolSchema(t.MCP.Server, t.MCP.Tool); schema != nil {
					tools[i].Parameters = schema
				}
			}
		}
	}
	return tools
}

func (r *Runner) skillPromptAppend(skillNames []string) string {
	if r.Registry == nil || len(skillNames) == 0 {
		return ""
	}
	var parts []string
	for _, sk := range r.Registry.GetSkills(skillNames) {
		if r.ProgressFn != nil {
			r.ProgressFn(ProgressEvent{Kind: "skill_start", Skill: sk.Name})
		}
		if sk.SystemPromptAppend != "" {
			parts = append(parts, sk.SystemPromptAppend)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (r *Runner) agentHasAnyTools(a model.Agent, baseTools []model.ToolDefinition) bool {
	if len(baseTools) > 0 {
		return true
	}
	turnCount := maxTurnDecls(a)
	for i := 0; i < turnCount; i++ {
		skills, toolsets, toolNames, declared := turnScopeFor(a, i)
		if !declared {
			continue
		}
		if len(r.resolveScopeTools(skills, toolsets, toolNames)) > 0 {
			return true
		}
	}
	return false
}

func turnScopeFor(a model.Agent, i int) (skills []string, toolsets []string, tools []string, declared bool) {
	if len(a.TurnSkills) > i && a.TurnSkills[i] != nil {
		skills = a.TurnSkills[i]
		declared = true
	}
	if len(a.TurnToolsets) > i && a.TurnToolsets[i] != nil {
		toolsets = a.TurnToolsets[i]
		declared = true
	}
	if len(a.TurnTools) > i && a.TurnTools[i] != nil {
		tools = a.TurnTools[i]
		declared = true
	}
	return skills, toolsets, tools, declared
}

// executeHideTool dispatches a hide navigation tool call to the HideBuffer.
// Arguments are type-asserted from JSON-decoded map values (numbers arrive as float64).
func (r *Runner) executeHideTool(name, callID string, args map[string]any) model.ToolResult {
	res := model.ToolResult{Name: name, ToolCallID: callID}
	if r.HideBuffer == nil {
		res.Error = "hide tool called but no hide buffer is configured"
		return res
	}
	rawHideID, _ := args["hide_id"].(string)
	hideID, ok := r.HideBuffer.ResolveID(rawHideID)
	if !ok {
		if rawHideID == "" {
			res.Error = "hide tool requires hide_id"
		} else {
			res.Error = fmt.Sprintf("hide tool unknown hide id %q", rawHideID)
		}
		return res
	}
	if rawHideID != "" && rawHideID != hideID && r.Log != nil {
		r.Log.Warn("hide tool resolved unknown id to active hide", "tool", name, "requested_hide", rawHideID, "active_hide", hideID)
	}
	switch name {
	case "hide_next":
		currentPage, ok := intArg(args, "current_page")
		if !ok || currentPage < 1 {
			res.Error = "hide_next requires current_page >= 1"
			return res
		}
		fromPage := currentPage
		if trackedPage, ok := r.currentHidePage(hideID); ok {
			fromPage = trackedPage
		}
		cut, err := r.HideBuffer.Cut(hideID, fromPage+1)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Content = cut.Format()
			r.recordHidePage(hideID, cut.PageNumber)
		}
	case "hide_jump":
		page, ok := intArg(args, "page")
		if !ok || page < 1 {
			res.Error = "hide_jump requires page >= 1"
			return res
		}
		cut, err := r.HideBuffer.Cut(hideID, page)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Content = cut.Format()
			r.recordHidePage(hideID, cut.PageNumber)
		}
	case "hide_search":
		query, _ := args["query"].(string)
		cut, _, err := r.HideBuffer.Search(hideID, query)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Content = cut.Format()
			r.recordHidePage(cut.HideID, cut.PageNumber)
		}
	default:
		res.Error = fmt.Sprintf("unknown hide tool %q", name)
	}
	return res
}

func (r *Runner) recordHidePages(content string) {
	for _, match := range hidePageHeaderRE.FindAllStringSubmatch(content, -1) {
		if len(match) < 3 {
			continue
		}
		page, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		r.recordHidePage(match[1], page)
	}
}

func (r *Runner) recordHidePage(hideID string, page int) {
	if hideID == "" || page < 1 {
		return
	}
	if r.hidePages == nil {
		r.hidePages = make(map[string]int)
	}
	if page > r.hidePages[hideID] {
		r.hidePages[hideID] = page
	}
}

func (r *Runner) currentHidePage(hideID string) (int, bool) {
	if r.hidePages == nil {
		return 0, false
	}
	page, ok := r.hidePages[hideID]
	return page, ok
}

func intArg(args map[string]any, name string) (int, bool) {
	value, ok := args[name]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		i := int(v)
		return i, float64(i) == v
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func maxTurnDecls(a model.Agent) int {
	max := len(a.TurnTools)
	if len(a.TurnSkills) > max {
		max = len(a.TurnSkills)
	}
	if len(a.TurnToolsets) > max {
		max = len(a.TurnToolsets)
	}
	return max
}

func joinPromptBlocks(parts ...string) string {
	var out []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n\n")
}

func cloneMessages(in []model.Message) []model.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Message, len(in))
	for i, msg := range in {
		out[i] = msg
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = make([]model.ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				out[i].ToolCalls[j] = tc
				if len(tc.Arguments) > 0 {
					out[i].ToolCalls[j].Arguments = cloneMap(tc.Arguments)
				}
			}
		}
	}
	return out
}

func toolNames(defs []model.ToolDefinition) []string {
	if len(defs) == 0 {
		return nil
	}
	out := make([]string, len(defs))
	for i, def := range defs {
		out[i] = def.Name
	}
	return out
}

func toolsNeedBuffer(defs []model.ToolDefinition) bool {
	for _, def := range defs {
		if def.Buffer {
			return true
		}
	}
	return false
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// runHook executes a shell hook command via sh -c with a 10 s hard timeout.
// Hook failures are non-fatal: logged as warnings only. Empty commands are a no-op.
// Hook command strings are never logged (they may contain user-defined values).
func (r *Runner) runHook(ctx context.Context, hookCmd, agentName, hookName string) {
	if hookCmd == "" {
		return
	}
	hookCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(hookCtx, "sh", "-c", hookCmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		r.Log.Warn("hook failed", "agent", agentName, "hook", hookName, "error", err)
	} else if out.Len() > 0 {
		r.Log.Debug("hook output", "agent", agentName, "hook", hookName, "bytes", out.Len())
	}
}

// errorRecord builds a failed RunRecord for the given error.
func (r *Runner) errorRecord(a model.Agent, startTs time.Time, err error) model.RunRecord {
	return model.RunRecord{
		AgentName: a.Name,
		Time:      model.RunTime{StartTs: startTs.Unix(), DurationMs: time.Since(startTs).Milliseconds()},
		Status:    model.JobStatusError,
		Error:     err.Error(),
	}
}

// marshalArgs encodes tool arguments as compact JSON for display. Returns ""
// if args is nil, empty, or cannot be marshalled.
func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

// previewToolResult truncates a tool result for display in progress events.
// It keeps the first ~24 lines and 1200 chars and appends a truncation marker
// when the content is larger so the operator can see what entered the model's
// context without flooding the terminal.
func previewToolResult(s string) string {
	const maxChars = 1200
	const maxLines = 24
	lines := strings.SplitN(s, "\n", maxLines+1)
	truncatedLines := len(lines) > maxLines
	if truncatedLines {
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxChars {
		out = out[:maxChars]
		return out + "\n… [truncated, full size " + fmt.Sprintf("%d bytes", len(s)) + "]"
	}
	if truncatedLines {
		return out + "\n… [truncated, full size " + fmt.Sprintf("%d bytes", len(s)) + "]"
	}
	return out
}

// applyVars replaces every {{key}} and {{.key}} occurrence in s with the
// corresponding value from vars. Replacements are applied in map iteration order.
func applyVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		s = strings.ReplaceAll(s, "{{."+k+"}}", v)
	}
	return s
}

// routeOutput delivers the agent response content to each configured OutputRoute.
// Errors are logged as warnings; routing failures do not fail the run.
func (r *Runner) routeOutput(ctx context.Context, a model.Agent, content string, startTs time.Time) {
	tmplData := map[string]any{
		"date":  startTs.Format("2006-01-02"),
		"agent": a.Name,
	}
	for _, route := range a.OutputRoutes {
		switch route.Type {
		case "file":
			path, err := expandTmpl(route.FilePath, tmplData)
			if err != nil {
				r.Log.Warn("output route: path template failed", "agent", a.Name, "path", route.FilePath, "error", err)
				continue
			}
			if dir := filepath.Dir(path); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					r.Log.Warn("output route: mkdir failed", "agent", a.Name, "path", path, "error", err)
					continue
				}
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				r.Log.Warn("output route: file write failed", "agent", a.Name, "path", path, "error", err)
			}
		case "queue":
			if r.QueueMgr == nil {
				r.Log.Warn("output route: queue manager not configured", "agent", a.Name, "queue", route.Queue)
				continue
			}
			item := model.QueueItem{
				ID:         fmt.Sprintf("%s-%d", a.Name, time.Now().UnixNano()),
				AgentName:  a.Name,
				Payload:    map[string]any{"content": content, "agent": a.Name},
				EnqueuedAt: time.Now().Unix(),
			}
			if err := r.QueueMgr.Enqueue(route.Queue, item); err != nil {
				r.Log.Warn("output route: enqueue failed", "agent", a.Name, "queue", route.Queue, "error", err)
			}
		case "http":
			r.routeHTTP(a.Name, route, content)
		case "notify":
			r.routeNotify(ctx, a, route, content)
		default:
			r.Log.Warn("output route: unknown type", "agent", a.Name, "type", route.Type)
		}
	}
}

// routeNotify delivers content to a named messaging backend.
func (r *Runner) routeNotify(ctx context.Context, a model.Agent, route model.OutputRoute, content string) {
	if len(r.Notifiers) == 0 {
		r.Log.Warn("output route: no notifiers configured", "agent", a.Name, "backend", route.NotifyBackend)
		return
	}
	n, ok := r.Notifiers[route.NotifyBackend]
	if !ok {
		r.Log.Warn("output route: unknown notify backend", "agent", a.Name, "backend", route.NotifyBackend)
		return
	}
	msg := notify.Message{
		AgentName: a.Name,
		Content:   content,
		Tags:      a.Tags,
		Timestamp: time.Now(),
	}
	if err := n.Send(ctx, msg); err != nil {
		r.Log.Warn("output route: notify failed", "agent", a.Name, "backend", route.NotifyBackend, "error", err)
	}
}

// routeHTTP sends content to an HTTP endpoint configured in the OutputRoute.
func (r *Runner) routeHTTP(agentName string, route model.OutputRoute, content string) {
	method := route.Method
	if method == "" {
		method = "POST"
	}
	req, err := http.NewRequest(method, route.URL, strings.NewReader(content))
	if err != nil {
		r.Log.Warn("output route: http request build failed", "agent", agentName, "url", route.URL, "error", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	for k, v := range route.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		r.Log.Warn("output route: http request failed", "agent", agentName, "url", route.URL, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		r.Log.Warn("output route: http non-success response", "agent", agentName, "url", route.URL, "status", resp.StatusCode)
	}
}

// BuildRunData returns the standard built-in template variables for an agent run.
// The returned map is suitable for merging with QueueItem.Payload and passing to
// ExpandPromptPayload. Built-in vars: agent_name, schedule, now, tags.
// Lifecycle parameters (a.Parameters) are included so scheduled agents can use
// {{.param_name}} substitution; queue payloads merged by the caller win on collision.
func BuildRunData(a model.Agent) map[string]any {
	data := map[string]any{
		"agent_name": a.Name,
		"schedule":   a.Schedule,
		"now":        time.Now().Format("2006-01-02 15:04:05"),
		"tags":       strings.Join(a.Tags, ", "),
	}
	for k, v := range a.Parameters {
		data[k] = v
	}
	return data
}

// ExpandPromptPayload applies text/template substitution to the agent's
// SystemPrompt and UserPrompt using payload as the template data.
// Template variables use {{.fieldName}} syntax (standard Go text/template).
// Returns an error (fail-closed) if any template fails to parse or execute.
// Returns a unchanged copy of the agent when payload is nil or empty.
func ExpandPromptPayload(a model.Agent, payload map[string]any) (model.Agent, error) {
	if len(payload) == 0 {
		return a, nil
	}
	sys, err := expandTmpl(a.SystemPrompt, payload)
	if err != nil {
		return model.Agent{}, fmt.Errorf("runner/ExpandPromptPayload: system prompt: %w", err)
	}
	usr, err := expandTmpl(a.UserPrompt, payload)
	if err != nil {
		return model.Agent{}, fmt.Errorf("runner/ExpandPromptPayload: user prompt: %w", err)
	}
	a.SystemPrompt = sys
	a.UserPrompt = usr
	return a, nil
}

// expandTmpl renders text using Go's text/template with data as the dot value.
// Both {{key}} and {{.key}} forms are accepted: bare {{key}} references are
// normalised to {{.key}} before parsing so authors don't need the dot prefix.
// Unknown keys produce an empty string (missingkey=zero behaviour).
func expandTmpl(text string, data map[string]any) (string, error) {
	// Pre-normalise {{key}} → {{.key}} for every known data key so both forms
	// work in agent files, matching the behaviour of applyVars.
	for k := range data {
		text = strings.ReplaceAll(text, "{{"+k+"}}", "{{."+k+"}}")
	}
	tmpl, err := template.New("prompt").Option("missingkey=zero").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}
	return buf.String(), nil
}
