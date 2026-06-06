package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/config"
)

// RunAttach connects to a running serve instance and streams pretty-printed
// DevTools events to stdout.
func RunAttach(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("attach", stderr)
	config.BindFlags(fs)
	filter := fs.String("filter", "", "comma-separated event kinds or sources to include")
	noReconnect := fs.Bool("no-reconnect", false, "exit instead of reconnecting on stream close")
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather attach: %v\n", err)
		return 1
	}

	// Read token from state directory.
	token, err := readDevtoolsToken(cfg.StateDir)
	if err != nil {
		fmt.Fprintf(stderr, "leather attach: cannot read devtools token: %v\n"+
			"  Is leather serve running? Token file expected at: %s\n",
			err, filepath.Join(cfg.StateDir, "devtools.token"))
		return 1
	}

	// Build filter set.
	var filterSet map[string]bool
	if *filter != "" {
		filterSet = make(map[string]bool)
		for _, f := range strings.Split(*filter, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				filterSet[f] = true
			}
		}
	}

	baseURL := "http://" + cfg.APIAddr + "/api/devtools/events"

	fmt.Fprintf(stdout, "%s  connecting to %s\n",
		dim(time.Now().Format("15:04:05")),
		bold(cfg.APIAddr))

	var lastEventID uint64
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		err := streamEvents(stdout, stderr, baseURL, token, lastEventID, filterSet, func(seq uint64) {
			lastEventID = seq
		})
		if err == nil || *noReconnect {
			return 0
		}
		// Serve closed the stream or we got a connection error. Reconnect with backoff.
		fmt.Fprintf(stdout, "%s  %s reconnecting in %s...\n",
			dim(time.Now().Format("15:04:05")),
			yellow("●"),
			backoff)
		time.Sleep(backoff)
		// Jitter + exponential backoff.
		backoff = time.Duration(float64(backoff)*1.5) + time.Duration(rand.Intn(500))*time.Millisecond
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// streamEvents opens a single SSE connection and feeds events to stdout until
// the stream closes. It calls onSeq for each event received so the caller can
// resume from the right position on reconnect.
func streamEvents(stdout, stderr io.Writer, baseURL, token string, fromSeq uint64, filterSet map[string]bool, onSeq func(uint64)) error {
	url := baseURL
	if fromSeq > 0 {
		url += fmt.Sprintf("?from=%d", fromSeq)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Fprintf(stderr, "leather attach: build request: %v\n", err)
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	if fromSeq > 0 {
		req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", fromSeq))
	}

	client := &http.Client{Timeout: 0} // no timeout — SSE is long-lived
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "leather attach: connect: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Fprintf(stderr, "leather attach: authentication failed (token mismatch)\n")
		return fmt.Errorf("unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "leather attach: server returned %s\n", resp.Status)
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	fmt.Fprintf(stdout, "%s  %s stream connected\n",
		dim(time.Now().Format("15:04:05")),
		green("●"))

	return parseSSEStream(stdout, resp.Body, filterSet, onSeq)
}

// parseSSEStream reads an SSE stream line by line and pretty-prints each event.
func parseSSEStream(stdout io.Writer, body io.Reader, filterSet map[string]bool, onSeq func(uint64)) error {
	scanner := bufio.NewScanner(body)
	var (
		eventType string
		dataLines []string
		lastID    string
	)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Blank line — dispatch accumulated event.
			if eventType != "" && len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				printAttachEvent(stdout, eventType, lastID, data, filterSet)
				if lastID != "" {
					var seq uint64
					if _, scanErr := fmt.Sscanf(lastID, "%d", &seq); scanErr == nil && seq > 0 {
						onSeq(seq)
					}
				}
			}
			eventType = ""
			dataLines = nil
		} else if strings.HasPrefix(line, "id:") {
			lastID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		// Lines beginning with ":" are SSE comments (heartbeat ping) — ignore.
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil // EOF — stream closed cleanly
}

// printAttachEvent formats a single DevTools SSE event for terminal output.
func printAttachEvent(stdout io.Writer, eventType, id, data string, filterSet map[string]bool) {
	// Parse the JSON payload to extract structured fields.
	var ev struct {
		Seq        uint64          `json:"seq"`
		At         int64           `json:"at"`
		Kind       string          `json:"kind"`
		Source     string          `json:"source"`
		EntityKind string          `json:"entity_kind"`
		EntityID   string          `json:"entity_id"`
		Payload    json.RawMessage `json:"payload"`
		Err        string          `json:"err"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		// Not a parseable event (e.g. gap or heartbeat) — skip.
		return
	}

	kind := ev.Kind
	if kind == "" {
		kind = eventType
	}

	// Apply filter.
	if filterSet != nil && !filterSet[kind] && !filterSet[ev.Source] {
		return
	}

	// Format timestamp.
	ts := dim(time.Unix(ev.At, 0).Format("15:04:05"))
	if ev.At == 0 {
		ts = dim(time.Now().Format("15:04:05"))
	}

	// Format kind label with color coding.
	label := attachFormatKind(kind)

	// Format entity reference.
	entity := ""
	if ev.EntityID != "" {
		entity = "  " + dim(ev.EntityKind+"/"+attachShortID(ev.EntityID))
	}

	// Format payload fields as key=value pairs (redacted values are already safe).
	kvs := attachFormatPayload(ev.Payload)

	// Format error.
	errStr := ""
	if ev.Err != "" {
		errStr = "  " + boldRed("err="+ev.Err)
	}

	fmt.Fprintf(stdout, "%s  %s%s%s%s\n", ts, label, entity, kvs, errStr)
}

// attachFormatKind returns a color-coded label for the event kind.
func attachFormatKind(kind string) string {
	switch {
	case strings.HasPrefix(kind, "agent."):
		return boldCyan(fmt.Sprintf("%-22s", kind))
	case strings.HasPrefix(kind, "queue."):
		return bold(fmt.Sprintf("%-22s", kind))
	case strings.HasPrefix(kind, "schedule."):
		return cyan(fmt.Sprintf("%-22s", kind))
	case kind == "error":
		return boldRed(fmt.Sprintf("%-22s", kind))
	default:
		return dim(fmt.Sprintf("%-22s", kind))
	}
}

// attachFormatPayload extracts safe key=value pairs from a payload blob.
func attachFormatPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}

	// Emit a curated subset of well-known fields in a stable order.
	order := []string{"agent", "queue", "hide_id", "hide_kind", "attempt", "progress_kind", "message", "payload_keys"}
	var parts []string
	for _, k := range order {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch val := v.(type) {
		case string:
			if val != "" {
				parts = append(parts, dim(k)+"="+val)
			}
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%v", dim(k), int(val)))
		case []any:
			strs := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				parts = append(parts, dim(k)+"=["+strings.Join(strs, ",")+"]")
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, "  ")
}

// attachShortID returns the last 8 chars of an ID for compact display.
func attachShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

// readDevtoolsToken reads the token written by leather serve.
func readDevtoolsToken(stateDir string) (string, error) {
	path := filepath.Join(stateDir, "devtools.token")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
