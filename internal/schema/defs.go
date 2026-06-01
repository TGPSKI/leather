package schema

// AgentFrontmatterSchema validates the YAML front matter of *.agent.md files.
// Only the flat scalar and list fields are covered; body text is not validated.
var AgentFrontmatterSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":     {Type: TypeString},
	"name":        {Type: TypeString, Required: true},
	"schedule":    {Type: TypeCron},
	"model":       {Type: TypeString},
	"tool_rounds": {Type: TypeInteger, HasMin: true, IntMin: 1, HasMax: true, IntMax: 20},
	"max_tokens":  {Type: TypeInteger, HasMin: true, IntMin: 1},
	"timeout":     {Type: TypeDuration},
	"temperature": {Type: TypeNumber},
	"enabled":     {Type: TypeBoolean},
	"queue_input": {Type: TypeString},
	"skills":      {IsList: true},
	"tags":        {IsList: true},
}

// LifecycleSchema validates the flat scalar and list fields of *.lifecycle.yaml files.
// Nested blocks (cache:, output:, hooks:, instances:, parameters:) are handled by lifecycle.go
// and are intentionally outside the flat-schema scope.
var LifecycleSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":            {Type: TypeString},
	"agent":              {Type: TypeString, Required: true},
	"name":               {Type: TypeString},
	"schedule":           {Type: TypeCron},
	"model":              {Type: TypeString},
	"prompt":             {Type: TypeString},
	"enabled":            {Type: TypeBoolean},
	"disable":            {Type: TypeBoolean},
	"max_tokens":         {Type: TypeInteger, HasMin: true, IntMin: 1},
	"timeout":            {Type: TypeDuration},
	"temperature":        {Type: TypeNumber},
	"tool_rounds":        {Type: TypeInteger, HasMin: true, IntMin: 1},
	"queue_input":        {Type: TypeString},
	"queue_batch_size":   {Type: TypeInteger, HasMin: true, IntMin: 1},
	"queue_max_attempts": {Type: TypeInteger, HasMin: true, IntMin: 0},
	"skills":             {IsList: true},
	"tags":               {IsList: true},
	"prompts":            {IsList: true},
	"toolsets":           {IsList: true},
}

// SkillSchema validates the top-level flat fields of *.skill.yaml files.
// The tools: array of nested objects is validated by the tool loader; only the
// presence of the list itself is checked here. The parameters: nested map block
// is parsed by the tool loader and is outside the flat-schema scope.
var SkillSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":              {Type: TypeString},
	"name":                 {Type: TypeString, Required: true},
	"system_prompt_append": {Type: TypeString},
	"tools":                {IsList: true, Required: true},
}

// ToolsetSchema validates the top-level flat fields of *.toolset.yaml files.
// A toolset is a named grouping of tool names; the "tools:" list references
// tool names defined elsewhere.
var ToolsetSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":     {Type: TypeString},
	"name":        {Type: TypeString, Required: true},
	"description": {Type: TypeString},
	"tools":       {IsList: true, Required: true},
}

// WorkerSchema validates the flat fields of *.worker.yaml files.
var WorkerSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":  {Type: TypeString},
	"name":     {Type: TypeString, Required: true},
	"type":     {Type: TypeEnum, Required: true, AllowedValues: []string{"http_poll"}},
	"interval": {Type: TypeDuration, Required: true},
	"url":      {Type: TypeString, Required: true},
}

// ConfigSchema validates the flat scalar fields of config.yaml.
// The notify: nested block is parsed separately and is outside the flat scope.
var ConfigSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":             {Type: TypeString},
	"agent_dir":           {Type: TypeString},
	"model":               {Type: TypeString},
	"log_level":           {Type: TypeEnum, AllowedValues: []string{"debug", "info", "warn", "error"}},
	"log_format":          {Type: TypeEnum, AllowedValues: []string{"text", "json"}},
	"llm_endpoint":        {Type: TypeString},
	"llm_api_key":         {Type: TypeString},
	"api_addr":            {Type: TypeString},
	"state_dir":           {Type: TypeString},
	"max_tokens":          {Type: TypeInteger, HasMin: true, IntMin: 1},
	"completion_reserve":  {Type: TypeInteger, HasMin: true, IntMin: 1},
	"max_concurrent_jobs": {Type: TypeInteger, HasMin: true, IntMin: 1},
	"summarize_threshold": {Type: TypeNumber},
	"temperature":         {Type: TypeNumber},
	"llm_timeout":         {Type: TypeDuration},
	"scheduler_tick":      {Type: TypeDuration},
	"run_duration":        {Type: TypeDuration},
	"max_jobs":            {Type: TypeInteger},
	"api":                 {Type: TypeBoolean},
	"log_file":            {Type: TypeString},
	"pretty":              {Type: TypeBoolean},
	"pretty_mode":         {Type: TypeEnum, AllowedValues: []string{"messages", "all"}},
	"stats":               {Type: TypeBoolean},
	"persist_runs":        {Type: TypeBoolean},
	"run_history_dir":     {Type: TypeString},
	"run_max_bytes":       {Type: TypeInteger, HasMin: true, IntMin: 1},
	"tool_dir":            {Type: TypeString},
	"default_toolsets":    {IsList: true},
	"max_tool_rounds":     {Type: TypeInteger, HasMin: true, IntMin: 1},
	"worker_dir":          {Type: TypeString},
	"cache_dir":           {Type: TypeString},
	"mcp_servers_file":    {Type: TypeString},
	"tannery":             {Type: TypeString},
	"hide_enabled":        {Type: TypeBoolean},
	"hide_page_size":      {Type: TypeInteger, HasMin: true, IntMin: 1},
	"tokens_per_turn":     {Type: TypeBoolean},
	"show_vars":           {Type: TypeBoolean},
	"loop":                {Type: TypeInteger, HasMin: true, IntMin: 0},
	"replay_file":         {Type: TypeString},
	"replay_live_dir":     {Type: TypeString},
	"replay_speed":        {Type: TypeNumber},
}

// TanneryConfigSchema validates the flat scalar fields of a tannery.yaml file.
// Nested blocks (routes:, queues:, webhooks:) are validated separately via
// ValidateTanneryYAML which walks each list/map item.
var TanneryConfigSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":      {Type: TypeString},
	"hide_dir":     {Type: TypeString},
	"curing_dir":   {Type: TypeString},
	"artifact_dir": {Type: TypeString},
}

// TanneryRouteSchema validates one item in the tannery routes: list.
var TanneryRouteSchema = Schema{
	"name":      {Type: TypeString, Required: true},
	"hide_kind": {Type: TypeString},
	"curing":    {Type: TypeString, Required: true},
	// queue and queue_pattern are mutually exclusive; one is required.
	// This is enforced in walkTanneryRoutes, not via field-level Required.
	"queue":         {Type: TypeString},
	"queue_pattern": {Type: TypeString},
}

// TanneryQueueSchema validates one entry in the tannery queues: map.
var TanneryQueueSchema = Schema{
	"concurrency":  {Type: TypeInteger, HasMin: true, IntMin: 1},
	"max_attempts": {Type: TypeInteger, HasMin: true, IntMin: 0},
	"max_depth":    {Type: TypeInteger, HasMin: true, IntMin: 0},
}

// TanneryWebhookSchema validates one item in the tannery webhooks: list.
var TanneryWebhookSchema = Schema{
	"name":           {Type: TypeString, Required: true},
	"path":           {Type: TypeString, Required: true},
	"source":         {Type: TypeString, Required: true},
	"secret":         {Type: TypeString},
	"max_body_bytes": {Type: TypeInteger, HasMin: true, IntMin: 0},
}

// CuringSchema validates the flat scalar and list fields of a *.curing.yaml file.
// The nested output: block is validated by walkCuringOutput.
var CuringSchema = Schema{
	// version is a reserved forward-compatibility field; default "1". (T5.10)
	"version":     {Type: TypeString},
	"name":        {Type: TypeString, Required: true},
	"description": {Type: TypeString},
	"agent":       {Type: TypeString, Required: true},
	// queue and queue_prefix are mutually exclusive; one is required.
	"queue":           {Type: TypeString},
	"queue_prefix":    {Type: TypeString},
	"hide_types":      {IsList: true},
	"page_size_bytes": {Type: TypeInteger, HasMin: true, IntMin: 1},
	"max_attempts":    {Type: TypeInteger, HasMin: true, IntMin: 0},
	"timeout_seconds": {Type: TypeInteger, HasMin: true, IntMin: 0},
	"collect_size":    {Type: TypeInteger, HasMin: true, IntMin: 1},
	"collect_by":      {Type: TypeString},
}

// CuringOutputSchema validates the nested output: block of a *.curing.yaml file.
var CuringOutputSchema = Schema{
	"notify": {Type: TypeString},
	"queue":  {Type: TypeString},
}

// MCPServersItemSchema validates a single item in the mcp-servers.yaml servers list.
var MCPServersItemSchema = Schema{
	"name":      {Type: TypeString, Required: true},
	"command":   {Type: TypeString, Required: true},
	"transport": {Type: TypeEnum, AllowedValues: []string{"stdio"}},
}
