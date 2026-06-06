// Package model defines the shared domain types for leather.
// It has no intra-project imports and carries no business logic.
package model

import "time"

// LogLevel controls the verbosity of structured logging output.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// JobStatus represents the execution state of a scheduled job.
type JobStatus string

const (
	JobStatusPending JobStatus = "pending"
	JobStatusRunning JobStatus = "running"
	JobStatusSuccess JobStatus = "success"
	JobStatusError   JobStatus = "error"
	JobStatusSkipped JobStatus = "skipped"
)

// ToolRetryConfig controls per-tool retry behaviour for transient failures.
// The zero value disables the configured retry policy; the executor falls back
// to the legacy single-retry-on-rate-limit behaviour.
type ToolRetryConfig struct {
	// MaxAttempts is the total number of attempts (initial + retries).
	// 0 means use the default (3). Set to 1 to disable retries entirely.
	MaxAttempts int `json:"max_attempts,omitempty"`
	// BaseDelay is the initial backoff delay. 0 means use the default (1s).
	BaseDelay time.Duration `json:"base_delay,omitempty"`
	// MaxDelay caps the backoff delay. 0 means use the default (30s).
	MaxDelay time.Duration `json:"max_delay,omitempty"`
	// HonorRetryAfter, when true, uses the Retry-After response header to
	// override the computed backoff delay. Defaults to true when MaxAttempts > 0.
	HonorRetryAfter bool `json:"honor_retry_after,omitempty"`
}

// ToolDefinition describes a callable tool available to an agent.
type ToolDefinition struct {
	// Name is the unique identifier of the tool, used by the model to invoke it.
	Name string `json:"name"`
	// Description is a human-readable explanation of what the tool does.
	Description string `json:"description,omitempty"`
	// Type is the tool executor type: "http", "mcp", or "hide" (built-in hide nav).
	Type string `json:"type"`
	// Parameters is the JSON Schema object describing the tool's input parameters.
	// When non-nil it is sent verbatim to the LLM API so the model knows what
	// arguments to pass. Populated at runtime for MCP tools via tools/list.
	Parameters map[string]any `json:"parameters,omitempty"`
	// HTTP holds the configuration for HTTP-type tools.
	HTTP HTTPToolConfig `json:"http,omitempty"`
	// MCP holds the configuration for mcp-type tools.
	MCP MCPToolConfig `json:"mcp,omitempty"`
	// OutputFile, when non-empty, is a file path where the raw tool result is
	// written after each successful execution. The file is created or truncated.
	OutputFile string `json:"output_file,omitempty"`
	// Buffer, when true, stores the tool result in the HideBuffer and delivers
	// a paged cut to the agent instead of the full content.
	Buffer bool `json:"buffer,omitempty"`
	// AllowedEnv restricts which environment variables may be expanded via
	// {{env:VAR}} in the tool's URL, headers, and body templates. When nil
	// (the default) all env vars are permitted for backwards compatibility;
	// set to an explicit list in new skill definitions.
	AllowedEnv []string `json:"allowed_env,omitempty"`
	// Retry configures the per-tool retry policy for transient failures.
	// The zero value preserves the legacy single-retry-on-rate-limit behaviour.
	Retry ToolRetryConfig `json:"retry,omitempty"`
}

// MCPToolConfig holds the configuration for an mcp-type tool.
type MCPToolConfig struct {
	// Server is the name of the MCP server from mcp-servers.yaml.
	Server string `json:"server"`
	// Tool is the tool name exposed by the MCP server.
	Tool string `json:"tool"`
}

// MCPEnvVar describes one environment variable to inject into an MCP server
// process. The value is resolved from a Unix pass-store path (preferred) or a
// literal environment variable name (fallback), matching the SecretRef pattern.
type MCPEnvVar struct {
	// Name is the environment variable name, e.g. "GITHUB_PERSONAL_ACCESS_TOKEN".
	Name string
	// Pass is the pass-store path to resolve, e.g. "github-pat".
	// Resolved by running `pass show <path>` at server startup.
	Pass string
	// Env is the environment variable name to read as a fallback when Pass is
	// empty or resolution fails.
	Env string
}

// MCPServerConfig describes one MCP server entry in mcp-servers.yaml.
type MCPServerConfig struct {
	// Name is the unique server identifier, referenced by MCPToolConfig.Server.
	Name string
	// Command is the shell command to start the server process (e.g. "skeptic mcp").
	Command string
	// Transport is the communication transport. Only "stdio" is supported.
	Transport string
	// Env holds environment variables to inject into the server process.
	// Each entry is resolved from a pass-store path or a fallback env var.
	Env []MCPEnvVar
}

// HTTPToolConfig holds the configuration for an HTTP-type tool.
// URL and header values may contain {{env:VAR}} and {{.argName}} templates.
type HTTPToolConfig struct {
	// Method is the HTTP method (GET, POST, PUT, PATCH, DELETE).
	Method string `json:"method"`
	// URL is the endpoint, which may contain template variables.
	URL string `json:"url"`
	// Headers are added verbatim (after template expansion) to the request.
	Headers map[string]string `json:"headers,omitempty"`
	// Query parameters are appended (after template expansion) to the URL.
	Query map[string]string `json:"query,omitempty"`
	// Body key/value pairs are serialized to a JSON object request body.
	Body map[string]string `json:"body,omitempty"`
}

// ToolCall represents a function invocation requested by the model.
type ToolCall struct {
	// ID is the model-assigned call identifier used when submitting tool results.
	ID string `json:"id"`
	// Name is the tool name the model is invoking.
	Name string `json:"name"`
	// Arguments are the parsed key/value parameters the model supplied.
	Arguments map[string]any `json:"arguments"`
}

// ToolResult is the output of executing a single ToolCall.
type ToolResult struct {
	// ToolCallID links this result to its originating ToolCall.
	ToolCallID string `json:"tool_call_id"`
	// Name is the tool that was executed.
	Name string `json:"name"`
	// Content is the plain-text result returned to the model.
	Content string `json:"content"`
	// Error is non-empty when execution failed; content may be empty.
	Error string `json:"error,omitempty"`
}

// SkillExtract describes a single post-tool-result extraction pattern. After a
// tool call whose name matches Tool succeeds, Pattern is applied to the output.
// If capture group 1 matches, the captured text is stored in turn-level variables
// under Store, where it becomes accessible as {{Store}} in subsequent turn prompts.
// NOTE: extracted values are injected into prompts; tool output is already in the
// LLM context window, so this does not widen the prompt-injection attack surface.
type SkillExtract struct {
	// Tool is the name of the tool whose output to scan.
	Tool string `json:"tool"`
	// Pattern is a Go regexp. The text of capture group 1 is stored.
	Pattern string `json:"pattern"`
	// Store is the variable key; accessible as {{Store}} in later turn prompts.
	Store string `json:"store"`
}

// Skill is a named bundle of ToolDefinitions with optional system-prompt augmentation.
type Skill struct {
	// Name is the unique skill identifier, referenced by agent definitions.
	Name string `json:"name"`
	// Description is a human-readable summary of what the skill provides.
	Description string `json:"description,omitempty"`
	// SystemPromptAppend is appended to the agent system prompt when this skill is active.
	SystemPromptAppend string `json:"system_prompt_append,omitempty"`
	// Parameters declares named template variables that this skill requires.
	// Keys appear as {{key}} placeholders in the skill's system_prompt_append
	// and in the agent body. Values are the defaults; empty string means no default
	// (leather run will prompt the user to supply a value at invocation time).
	Parameters map[string]string `json:"parameters,omitempty"`
	// Extract is an ordered list of post-tool-result extraction patterns. Applied
	// after each successful tool call; matched values flow into subsequent turn prompts.
	Extract []SkillExtract `json:"extract,omitempty"`
	// RequiredEnv is the set of environment variable names that this skill's tools
	// are permitted to expand via {{env:VAR}} templates. When non-empty it is
	// propagated to AllowedEnv on each ToolDefinition at load time. An empty
	// RequiredEnv leaves per-tool AllowedEnv unchanged (nil = unrestricted).
	RequiredEnv []string `json:"required_env,omitempty"`
	// Tools is the ordered list of tool definitions bundled with this skill.
	Tools []ToolDefinition `json:"tools"`
}

// Toolset is a named bundle of tool names used for tool-exposure policy.
// Unlike Skill, a Toolset does not append prompt text; it only groups tools.
type Toolset struct {
	// Name is the unique toolset identifier, referenced by config and agent declarations.
	Name string `json:"name"`
	// Description is a human-readable summary of what the toolset exposes.
	Description string `json:"description,omitempty"`
	// Tools is the ordered list of tool names included in the toolset.
	Tools []string `json:"tools"`
}

// SecretRef resolves a secret value from either a Unix pass-store path
// (preferred) or an environment variable name (fallback). The resolution order
// is pass → env → error. Secret values are never stored on the struct after
// resolution; this type is used only in configuration.
type SecretRef struct {
	// Pass is the pass-store path, e.g. "leather/telegram/bot-token".
	// Resolved by running `pass show <path>` via os/exec.
	Pass string
	// Env is the environment variable name, e.g. "LEATHER_TELEGRAM_BOT_TOKEN".
	Env string
}

// NotifyBackendConfig describes one messaging backend loaded from the notify:
// block in config.yaml.
type NotifyBackendConfig struct {
	// Name is the unique backend identifier referenced by output route "backend:" fields.
	Name string
	// Type is the backend implementation: "telegram" or "signal".
	Type string
	// ChatID is the Telegram chat_id (numeric string). Required for type=telegram.
	ChatID string
	// From is the E.164 sender number for type=signal.
	From string
	// To is the E.164 recipient number for type=signal.
	To string
	// GroupID is the Signal group ID. Mutually exclusive with To.
	GroupID string
	// APIURL is the signal-cli REST API base URL for type=signal.
	// Defaults to http://127.0.0.1:8080 when empty.
	APIURL string
	// Token holds the secret reference for the bot token or API key.
	Token SecretRef
}

// CacheConfig controls response caching behaviour for an agent.
type CacheConfig struct {
	// Enabled activates the sha256-keyed file cache for this agent.
	Enabled bool
	// TTL is the time-to-live for each cache entry. Zero means entries never expire.
	TTL time.Duration
}

// OutputRoute describes one destination for an agent's response content.
type OutputRoute struct {
	// Type is the routing backend: "file", "queue", "http", or "notify".
	Type string
	// FilePath is the destination path for type=file.
	// May contain {{.date}} or {{.agent}} template variables.
	FilePath string
	// Queue is the queue name for type=queue.
	Queue string
	// URL is the endpoint for type=http.
	URL string
	// Method is the HTTP method for type=http (default "POST").
	Method string
	// Headers are added verbatim to HTTP requests for type=http.
	// Values may contain {{env:VAR}} template expressions.
	Headers map[string]string
	// NotifyBackend is the backend name for type=notify.
	// Must match a NotifyBackendConfig.Name in Config.NotifyBackends.
	NotifyBackend string
}

// WorkerOutput configures where a polling worker delivers collected items.
type WorkerOutput struct {
	// Queue is the name of the queue to push new items into.
	Queue string `json:"queue"`
	// DedupKey is the JSON field name used to identify unique items (e.g. "number", "id").
	// A leading dot is stripped; only top-level fields are supported.
	DedupKey string `json:"dedup_key"`
}

// WorkerDefinition describes a polling worker loaded from a *.worker.yaml file.
type WorkerDefinition struct {
	// Name is the unique worker identifier.
	Name string `json:"name"`
	// Type is the worker implementation to use; currently only "http_poll" is supported.
	Type string `json:"type"`
	// Interval is how often the worker polls its source.
	Interval time.Duration `json:"interval"`
	// URL is the endpoint to poll; may contain {{env:VAR}} template expressions.
	URL string `json:"url"`
	// Headers are added to every HTTP request; values may contain {{env:VAR}} templates.
	Headers map[string]string `json:"headers,omitempty"`
	// Output describes where new items are sent.
	Output WorkerOutput `json:"output"`
}

// QueueItem is a single entry in a named file queue.
type QueueItem struct {
	// ID is a unique identifier assigned when the item is enqueued.
	ID string `json:"id"`
	// AgentName is optionally set when the item targets a specific agent.
	AgentName string `json:"agent_name,omitempty"`
	// Payload holds the raw data fields available for prompt variable substitution.
	Payload map[string]any `json:"payload"`
	// EnqueuedAt is the Unix timestamp (seconds) when the item was enqueued.
	EnqueuedAt int64 `json:"enqueued_at"`
	// AttemptCount is incremented each time the item is dequeued for processing.
	AttemptCount int `json:"attempt_count"`
	// CuringName, when non-empty, marks this as a curing work item.
	// CuringWorker processes items where CuringName != "".
	// The scheduler agent runner processes items where CuringName == "" (unchanged).
	CuringName string `json:"curing_name,omitempty"`
	// HideID references the hide entry containing the raw input for this curing run.
	HideID string `json:"hide_id,omitempty"`
	// HideKind is the hide type string (e.g. "github.pr_review_thread").
	HideKind string `json:"hide_kind,omitempty"`
	// CorrelationID, when set, ties this item to a fan-out group.
	// All items produced by a single fan-out webhook event share the same
	// CorrelationID (= the original webhook hide_id). Downstream curings
	// with collect_by: correlation_id group by this field for correlated joins.
	CorrelationID string `json:"correlation_id,omitempty"`
	// ToolName, when non-empty, identifies this as an outbound-DLQ item produced
	// by a failed tool execution. Set to the tool name that failed.
	ToolName string `json:"tool_name,omitempty"`
	// ToolTarget is the URL (for HTTP tools) or "<server>/<tool>" (for MCP tools)
	// that the failed tool was targeting. Used for DLQ inspection.
	ToolTarget string `json:"tool_target,omitempty"`
}

// AgentHooks describes shell commands executed at agent lifecycle events.
// Commands are run via sh -c with a 10 s hard timeout; failures are non-fatal.
type AgentHooks struct {
	// PreRun is executed before the agent's first LLM call.
	PreRun string
	// PostSuccess is executed after a successful agent run.
	PostSuccess string
	// PostError is executed after a failed agent run.
	PostError string
}

// Agent holds the parsed definition of a *.agent.md file.
type Agent struct {
	// Name is the unique agent identifier, used as the scheduler job name.
	Name string
	// Schedule is a cron expression or "once" for a one-shot agent.
	Schedule string
	// Model is the model name passed verbatim to the LLM endpoint.
	Model string
	// SystemPrompt is the Markdown body of the agent file, trimmed of whitespace.
	SystemPrompt string
	// UserPrompt is the per-instantiation user message sent at each execution.
	// When non-empty, it is added as the first user turn before the model is called.
	UserPrompt string
	// UserPrompts, when non-empty, replaces UserPrompt with an ordered sequence of
	// user messages. The model responds to each in turn within the same session so
	// subsequent prompts see the full prior conversation as context.
	// Declared via the prompts: list field in lifecycle YAML.
	UserPrompts []string
	// MaxTokens overrides the global token budget for this agent. Zero means use the global default.
	MaxTokens int
	// Timeout overrides the global LLM request timeout. Zero means use the global default.
	Timeout time.Duration
	// Temperature is the sampling temperature sent to the model.
	Temperature float64
	// Enabled controls whether the agent is registered with the scheduler.
	Enabled bool
	// Tags are metadata labels for filtering in leather status.
	Tags []string
	// Skills is the list of skill names to load from the tool registry.
	Skills []string
	// Toolsets is the list of named toolsets to load from the tool registry.
	// Config.DefaultToolsets are applied first; agent Toolsets append after that.
	Toolsets []string
	// TurnTools, when non-empty, restricts which tools are available on each
	// user-prompt turn. Element i corresponds to UserPrompts[i]; a nil slice
	// means "all agent tools" (no restriction). Populated by LoadFile when the
	// agent body uses per-turn --- sections with optional "tools: [...]" lines.
	TurnTools [][]string
	// TurnSkills, when non-empty, declares skill bundles that become available on
	// each user-prompt turn. Element i corresponds to UserPrompts[i]. When any of
	// TurnSkills[i], TurnToolsets[i], or TurnTools[i] are non-empty, that turn's
	// tool exposure is resolved from the declared turn scope instead of the base scope.
	TurnSkills [][]string
	// TurnToolsets, when non-empty, declares named toolsets that become available on
	// each user-prompt turn. Element i corresponds to UserPrompts[i].
	TurnToolsets [][]string
	// Parameters holds named template variables declared in the lifecycle file.
	// Values are substituted for {{key}} placeholders in prompts before the first LLM call.
	// An empty string value causes leather run to prompt the user interactively.
	Parameters map[string]string
	// ToolRounds is the maximum tool-call/result cycles per run.
	// Zero means use Config.MaxToolRounds.
	ToolRounds int
	// QueueInput, when non-empty, names a queue whose items are dequeued one per tick.
	// Each item's Payload is used for prompt variable substitution in SystemPrompt and UserPrompt.
	QueueInput string
	// QueueBatchSize is the maximum number of queue items processed per scheduler tick.
	// Zero or 1 means one item per tick (default behaviour).
	QueueBatchSize int
	// QueueMaxAttempts is the maximum times a queue item is retried before being moved
	// to the dead-letter queue (<queue>-dlq). Zero disables DLQ promotion (items are dropped on failure).
	QueueMaxAttempts int
	// Cache configures optional response caching for this agent.
	Cache CacheConfig
	// OutputRoutes lists the destinations where the agent's response is sent after each run.
	OutputRoutes []OutputRoute
	// Hooks are optional shell commands run at lifecycle events.
	Hooks AgentHooks
	// SourcePath is the absolute path of the *.agent.md file.
	SourcePath string
	// LifecycleSourcePath is the absolute path of the *.lifecycle.yaml file
	// that supplied the operational configuration for this agent.
	// Empty when scheduling/model fields came from the agent file's front matter.
	LifecycleSourcePath string
}

// Job is a scheduler record for a single agent's execution slot.
type Job struct {
	// AgentName identifies which agent this job belongs to.
	AgentName string `json:"agent_name"`
	// Status is the last known execution state.
	Status JobStatus `json:"status"`
	// LastRun is the Unix timestamp of the most recent execution start.
	LastRun int64 `json:"last_run"`
	// NextRun is the Unix timestamp of the next scheduled execution.
	NextRun int64 `json:"next_run"`
	// LastError holds the error message from the most recent failed run.
	LastError string `json:"last_error,omitempty"`
	// RunCount is the total number of times this job has executed.
	RunCount int `json:"run_count"`
}

// Message is a single conversational turn in a session context window.
type Message struct {
	// Role is "system", "user", "assistant", or "tool".
	Role string
	// Content is the text of the message.
	Content string
	// Tokens is the estimated token count for this message.
	Tokens int
	// Summarized indicates this message is a collapsed summary of earlier turns.
	Summarized bool
	// ToolCalls is non-nil for assistant messages that request tool invocations.
	ToolCalls []ToolCall
	// ToolCallID links a tool-role message to its originating ToolCall.
	ToolCallID string
	// ToolName is the tool name for tool-role result messages.
	ToolName string
}

// TokenBudget defines the token-limit parameters for a session.
type TokenBudget struct {
	// MaxTokens is the total context window size for the model.
	MaxTokens int
	// CompletionReserve is the number of tokens held back for the model's response.
	CompletionReserve int
	// SummarizeThreshold is the fraction of MaxTokens at which summarization is triggered.
	SummarizeThreshold float64
}

// LLMResponse is the parsed output from a model completion call.
type LLMResponse struct {
	// Content is the model's generated text.
	Content string
	// FinishReason is the stop condition reported by the model (e.g. "stop", "length", "tool_calls").
	FinishReason string
	// PromptTokens is the number of tokens in the input.
	PromptTokens int
	// CompletionTokens is the number of tokens generated.
	CompletionTokens int
	// TotalTokens is PromptTokens + CompletionTokens.
	TotalTokens int
	// ToolCalls contains function invocations requested by the model.
	// Non-empty when FinishReason is "tool_calls".
	ToolCalls []ToolCall
}

// Config is the fully resolved runtime configuration for leather,
// merged from CLI flags, environment variables, config file, and built-in defaults.
type Config struct {
	AgentDir           string
	ConfigFile         string
	LogLevel           LogLevel
	LogFormat          string  // "text" or "json"
	Model              string  // global default model name; applied to agents that don't specify one
	Temperature        float64 // global default sampling temperature
	MaxTokens          int
	CompletionReserve  int
	SummarizeThreshold float64
	LLMEndpoint        string
	LLMTimeout         time.Duration
	SchedulerTick      time.Duration // how often the scheduler wakes to check for due jobs
	MaxConcurrentJobs  int
	// RunDuration caps the total wall-clock life of leather serve. 0 = unlimited.
	RunDuration time.Duration
	// MaxJobs caps the total number of completed jobs before a clean shutdown. 0 = unlimited.
	MaxJobs  int
	StateDir string
	API      bool
	APIAddr  string
	// LogFile is the path to write full structured logs. Empty = stderr only.
	LogFile string
	// Pretty suppresses structured log output from the console and renders
	// agent turns (user prompt / model response) in a human-readable format instead.
	Pretty bool
	// PrettyMode selects the pretty console layout. "messages" shows only the
	// user/assistant transcript; "all" also shows live activity lines.
	PrettyMode string
	// Stats prints per-turn token counts (in pretty mode) and a totals summary at shutdown.
	Stats bool
	// TokensPerTurn, when true in pretty mode, prints token usage after each individual turn response.
	TokensPerTurn bool
	// ShowVars, when true in pretty mode, prints each key–value pair extracted from tool results
	// into the turn-variable map as an inline event in the timeline.
	ShowVars bool
	// ShowContext, when true, prints the exact message window and tool exposure
	// immediately before each LLM call. Intended for debugging prompt/context flow.
	ShowContext bool
	// HideEnabled enables the hide buffer; tool results marked buffer:true are paged.
	HideEnabled bool
	// HidePageSizeBytes is the byte size of each hide cut (default 3800).
	HidePageSizeBytes int
	// PersistRuns enables writing RunRecords to JSONL files in RunHistoryDir.
	PersistRuns bool
	// RunHistoryDir is the directory for per-agent *.jsonl run log files.
	// Defaults to <StateDir>/runs when empty.
	RunHistoryDir string
	// RunMaxBytes is the maximum JSONL file size before rotation. Defaults to 10 MB.
	RunMaxBytes int64
	// ReplayFile, when non-empty, starts leather in read-only replay mode.
	// The serve command loads the snapshot at this path and serves it from the API.
	ReplayFile string
	// ReplayLiveDir, when non-empty, starts leather in live JSONL replay mode.
	// The serve command loads all *.jsonl files from this directory and serves
	// them incrementally based on the replay clock.
	ReplayLiveDir string
	// ReplaySpeed is the playback speed multiplier for replay-live mode. Defaults to 1.0.
	ReplaySpeed float64
	// ToolDir is the directory containing *.skill.yaml tool definition files.
	// It also hosts *.toolset.yaml files. Defaults to ~/.leather/tools/ when empty.
	ToolDir string
	// DefaultToolsets is the global baseline set of named toolsets applied to every
	// agent before the agent's own Skills/Toolsets declarations. Turn declarations
	// override this base scope for the specific turn.
	DefaultToolsets []string
	// MaxToolRounds is the global default maximum tool call cycles per agent run.
	// Individual agents may override via tool_rounds in their lifecycle file.
	MaxToolRounds int
	// WorkerDir is the directory containing *.worker.yaml worker definition files.
	// Defaults to ~/.leather/workers/ when empty.
	WorkerDir string
	// CacheDir is the directory for the sha256-keyed response cache files.
	// Defaults to <StateDir>/cache when empty.
	CacheDir string
	// NotifyBackends is the list of messaging backend configurations loaded
	// from the notify.backends block in config.yaml.
	NotifyBackends []NotifyBackendConfig
	// MCPServersFile is the path to mcp-servers.yaml.
	// Defaults to ~/.leather/mcp-servers.yaml when empty.
	MCPServersFile string
	// Loop is the number of times to repeat the run command. 0 and 1 both mean run once.
	Loop int
	// TanneryFile is the path to tannery.yaml. When empty, tannery mode is disabled.
	TanneryFile string
	// LLMAPIKey is the resolved bearer token sent as `Authorization: Bearer <key>`
	// to LLMEndpoint. Empty disables auth (suitable for local Ollama / vLLM).
	// The raw key is never written to structured logs.
	LLMAPIKey string
	// ToolRateLimits maps a hostname (e.g. "api.github.com") to a rate spec
	// expressed as "N/s", "N/m", or "N/h". An empty map means no limits.
	// Populated from the tools.rate_limits block in config.yaml.
	ToolRateLimits map[string]string
}

// SessionContext is a point-in-time snapshot of a session's conversation window.
// It is returned by Session.Snapshot and used for state inspection, logging, and tests.
type SessionContext struct {
	// Messages is the ordered list of messages in the current context window.
	Messages []Message
	// UsedTokens is the total estimated token count for all messages.
	UsedTokens int
	// Metadata holds arbitrary key-value labels for this snapshot (e.g. agent name, job ID).
	Metadata map[string]string
}

// Turn is a single prompt/response exchange within an agent execution.
type Turn struct {
	// Prompt is the user-turn message sent to the model.
	Prompt string `json:"prompt"`
	// Response is the model's generated text.
	Response string `json:"response"`
	// PromptTokens is the number of input tokens used for this turn.
	PromptTokens int `json:"prompt_tokens,omitempty"`
	// CompletionTokens is the number of output tokens generated for this turn.
	CompletionTokens int `json:"completion_tokens,omitempty"`
	// TotalTokens is PromptTokens + CompletionTokens for this turn.
	TotalTokens int `json:"total_tokens,omitempty"`
}

// RunTokens holds the token usage breakdown for a single agent execution.
type RunTokens struct {
	// Prompt is the number of tokens in the model input.
	Prompt int `json:"prompt"`
	// Response is the number of tokens generated by the model.
	Response int `json:"response"`
	// Total is Prompt + Response.
	Total int `json:"total"`
}

// RunTime holds timing information for a single agent execution.
type RunTime struct {
	// StartTs is the Unix timestamp (seconds) when execution began.
	StartTs int64 `json:"start_ts"`
	// DurationMs is the wall-clock duration of the execution in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// RunRecord is a single completed agent execution record, stored in the
// in-process run history ring buffer and served by the /metrics and /history endpoints.
type RunRecord struct {
	// AgentName is the name of the agent that produced this record.
	AgentName string `json:"agent_name"`
	// Time holds the start timestamp and duration of the execution.
	Time RunTime `json:"time"`
	// Status is the final execution state.
	Status JobStatus `json:"status"`
	// Tokens holds the token usage breakdown for this execution.
	Tokens RunTokens `json:"tokens"`
	// Error holds the error message for failed runs. Empty on success.
	Error string `json:"error,omitempty"`
	// SystemPrompt is the agent's system prompt at the time of this execution.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Turns holds the ordered prompt/response exchanges for this execution.
	Turns []Turn `json:"turns,omitempty"`
	// LastResponse is the final text content returned by the LLM in the last
	// successful turn. Empty on error runs or zero-turn runs.
	LastResponse string `json:"last_response,omitempty"`
}

// CuringOutput configures where completed curing results are delivered.
type CuringOutput struct {
	// Notify is the notifier backend name to send the artifact content to.
	Notify string `json:"notify,omitempty"`
	// Queue, when non-empty, forwards artifact content to this queue.
	Queue string `json:"queue,omitempty"`
}

// CuringDefinition describes a named curing workflow loaded from a *.curing.yaml file.
type CuringDefinition struct {
	// Name is the unique curing identifier.
	Name string `json:"name"`
	// Description is a human-readable summary of what this curing does.
	Description string `json:"description,omitempty"`
	// Agent is the agent name to run for each hide; must exist in the agent directory.
	Agent string `json:"agent"`
	// HideTypes is the list of accepted hide kinds; empty means accept all kinds.
	HideTypes []string `json:"hide_types"`
	// Queue is the queue name this curing pulls work from.
	Queue string `json:"queue"`
	// PageSizeBytes is the byte size of each hide cut delivered to the agent (default 3800).
	PageSizeBytes int `json:"page_size_bytes"`
	// MaxAttempts is the per-item retry cap; 0 = unlimited; default 3.
	MaxAttempts int `json:"max_attempts"`
	// TimeoutSeconds is the per-run time limit in seconds; 0 = no timeout; default 900.
	TimeoutSeconds int `json:"timeout_seconds"`
	// Output configures where completed curing results are delivered.
	Output CuringOutput `json:"output,omitempty"`
	// CollectSize, when > 0, requires this many items with the same CollectBy key
	// to be present in the queue before any are dequeued. All CollectSize items are
	// dequeued atomically and their hide contents combined into one agent invocation.
	// Use this for fan-in joins where N parallel analysis agents feed one decision.
	// Zero means process items individually as they arrive (default behaviour).
	CollectSize int `json:"collect_size,omitempty"`
	// CollectBy is the QueueItem field used to group items for CollectSize.
	// Supported values: "hide_id" (default when CollectSize > 0), "curing_name",
	// "correlation_id". Ignored when QueuePrefix is set (each single-use queue
	// holds items for exactly one event, so no grouping key is needed).
	CollectBy string `json:"collect_by,omitempty"`
	// QueuePrefix, when non-empty, enables single-use queue mode: the worker
	// scans the queue directory for queues whose names begin with this prefix
	// and processes them individually. Each single-use queue holds items for
	// exactly one event. Mutually exclusive with Queue.
	QueuePrefix string `json:"queue_prefix,omitempty"`
	// SourcePath is set by the loader to the absolute path of the *.curing.yaml file.
	SourcePath string `json:"source_path,omitempty"`
}

// Artifact is the durable output of a completed curing run.
type Artifact struct {
	// ID is the unique artifact identifier.
	ID string `json:"id"`
	// HideID is the hide that was processed to produce this artifact.
	HideID string `json:"hide_id"`
	// HideKind is the kind label of the source hide.
	HideKind string `json:"hide_kind"`
	// CuringName is the curing workflow that produced this artifact.
	CuringName string `json:"curing_name"`
	// AgentName is the agent that ran the curing.
	AgentName string `json:"agent_name"`
	// Queue is the source queue name from which the hide item was dequeued.
	Queue string `json:"queue,omitempty"`
	// Content is the final agent response (LastResponse from the RunRecord).
	Content string `json:"content"`
	// CreatedAt is the Unix timestamp when the artifact was written.
	CreatedAt int64 `json:"created_at"`
	// Metadata holds arbitrary key-value labels for this artifact.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RouteMatch is the predicate for a TanneryRoute.
// An empty EventType matches all events from the given Source.
type RouteMatch struct {
	// Source is the source label used for route matching (e.g. "github").
	Source string `json:"source"`
	// EventType, when non-empty, further restricts the match to a specific event type.
	EventType string `json:"event_type,omitempty"`
}

// TanneryRoute maps an intake event to a curing workflow and queue.
type TanneryRoute struct {
	// Name is the unique route identifier.
	Name string `json:"name"`
	// Match is the predicate used to select this route.
	Match RouteMatch `json:"match"`
	// HideKind is the kind label assigned to the hide created from the matched event.
	HideKind string `json:"hide_kind"`
	// Curing is the name of the CuringDefinition to run.
	Curing string `json:"curing"`
	// Queue is the queue name to enqueue the curing work item on.
	// Mutually exclusive with QueuePattern.
	Queue string `json:"queue"`
	// QueuePattern, when non-empty, is a per-event queue name template.
	// {{hide_id}} is expanded at fan-out time using the shared hide ID, creating
	// an isolated single-use queue for each event. Mutually exclusive with Queue.
	QueuePattern string `json:"queue_pattern,omitempty"`
}

// QueueConcurrencyConfig controls the curing worker pool for one queue.
type QueueConcurrencyConfig struct {
	// Concurrency is the number of goroutines pulling from this queue (default 1).
	Concurrency int `json:"concurrency"`
	// MaxAttempts is the per-item retry cap; 0 = unlimited; default 3.
	MaxAttempts int `json:"max_attempts"`
	// MaxDepth is the backpressure ceiling; when Depth >= MaxDepth and MaxDepth > 0,
	// webhook/intake handlers return HTTP 503 with Retry-After: 30. Default 1000; 0 = unlimited.
	MaxDepth int `json:"max_depth"`
	// PollInterval is the worker's queue-poll frequency. Default 1s. Use shorter
	// values (e.g. 100ms) for low-latency workflows where queue items arrive in bursts.
	PollInterval time.Duration `json:"poll_interval"`
}

// WebhookConfig describes one registered webhook endpoint.
type WebhookConfig struct {
	// Name is the unique webhook identifier.
	Name string `json:"name"`
	// Path is the HTTP path suffix (e.g. "/webhooks/github").
	Path string `json:"path"`
	// Source is the source label added to every hide created by this webhook.
	Source string `json:"source"`
	// Secret is the optional HMAC secret; supports {{env:VAR}} expansion.
	Secret string `json:"secret,omitempty"`
	// MaxBodyBytes caps the request body size; 0 = no limit.
	MaxBodyBytes int64 `json:"max_body_bytes,omitempty"`
}

// RunOptions carries per-invocation settings for a single leather command.
type RunOptions struct {
	// AgentName restricts execution to a single named agent (used by leather run).
	AgentName string
	// DryRun reports what would execute without making LLM calls.
	DryRun bool
}
