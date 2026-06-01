package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/session"
)

// RunChat starts an interactive chat session backed by the leather session
// abstraction (token budget, automatic context summarization).
// Usage: leather chat [flags]
func RunChat(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("chat", stderr)
	config.BindFlags(fs)
	// Chat-specific flags (not in model.Config).
	systemFlag := fs.String("system", "", "system prompt text")
	agentFlag := fs.String("agent", "", "path to *.agent.md; loads system prompt and name")
	devFlag := fs.Bool("dev", false, "show compaction events and per-turn session diagnostics")

	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather chat: %v\n", err)
		return 1
	}
	// Auto-disable pretty mode when stdout is not an interactive terminal.
	if cfg.Pretty && !isTTY(stdout) {
		cfg.Pretty = false
	}
	if cfg.Model == "" {
		fmt.Fprintln(stderr, "leather chat: --model is required (or set model in config.yaml)")
		return 1
	}

	budget := model.TokenBudget{
		MaxTokens:          cfg.MaxTokens,
		CompletionReserve:  cfg.CompletionReserve,
		SummarizeThreshold: cfg.SummarizeThreshold,
	}
	systemPrompt := *systemFlag
	agentName := filepath.Base(cfg.Model)

	if *agentFlag != "" {
		a, loadErr := agent.LoadFile(*agentFlag)
		if loadErr != nil {
			fmt.Fprintf(stderr, "leather chat: load agent: %v\n", loadErr)
			return 1
		}
		a = resolveAgent(cfg, a)
		budget = resolveTokenBudget(cfg, a)
		if systemPrompt == "" {
			systemPrompt = a.SystemPrompt
		}
		if a.Name != "" {
			agentName = a.Name
		}
	}

	return runChatLoop(cfg, budget, agentName, systemPrompt, *devFlag,
		buildHTTPClient(cfg), stdin, stdout, stderr)
}

// runChatLoop holds the chat implementation. It accepts a session.LLMClient so
// tests can inject session.MockLLM without a live server.
func runChatLoop(
	cfg model.Config,
	budget model.TokenBudget,
	agentName string,
	systemPrompt string,
	dev bool,
	client session.LLMClient,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	sess := session.New(budget, cfg.Model, client)

	if systemPrompt != "" {
		if err := sess.Add(context.Background(), model.Message{Role: "system", Content: systemPrompt}); err != nil {
			fmt.Fprintf(stderr, "leather chat: add system prompt: %v\n", err)
			return 1
		}
	}

	// Compute label width for right-aligned prompt/response prefixes.
	labelWidth := len("you")
	if len(agentName) > labelWidth {
		labelWidth = len(agentName)
	}
	userPad := strings.Repeat(" ", labelWidth-len("you"))
	agentPad := strings.Repeat(" ", labelWidth-len(agentName))
	contIndent := strings.Repeat(" ", labelWidth+3) // label + " › " (3 chars)

	// Header.
	fmt.Fprintf(stdout, "%s  %s  %s\n", bold("leather chat"), dim(fmt.Sprintf("model=%s", cfg.Model)), dim(fmt.Sprintf("ctx=%d", budget.MaxTokens)))
	if dev {
		used, remaining := sess.Usage()
		fmt.Fprintf(stdout, "  %s\n",
			dim(fmt.Sprintf("summarize=%.0f%%  completion_reserve=%d  ctx_used=%d  ctx_remaining=%d",
				cfg.SummarizeThreshold*100, budget.CompletionReserve, used, remaining)))
	}
	fmt.Fprintf(stdout, "  %s  %s  %s  %s  %s\n\n",
		cyan("/reset"), cyan("/stats"), cyan("/show"), cyan("/quit"), cyan("/help"))

	completionOpts := session.CompletionOptions{
		MaxTokens:   budget.CompletionReserve,
		Temperature: cfg.Temperature,
	}

	scanner := bufio.NewScanner(stdin)
	// Allow lines up to 1 MiB (default is 64 KiB) so pasting larger prompts works.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Fprintf(stdout, "%s%s %s ", userPad, boldCyan("you"), dim("›"))
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())

		switch line {
		case "":
			continue
		case "/quit", "/exit":
			fmt.Fprintln(stdout, "bye")
			return 0
		case "/reset":
			sess.Reset()
			used, remaining := sess.Usage()
			fmt.Fprintf(stdout, "  %s\n\n", dim(fmt.Sprintf("session reset  used=%d remaining=%d", used, remaining)))
			continue
		case "/stats":
			used, remaining := sess.Usage()
			msgs := sess.Messages()
			fmt.Fprintf(stdout, "  %s\n\n",
				dim(fmt.Sprintf("ctx_used=%d  ctx_remaining=%d  messages=%d", used, remaining, len(msgs))))
			continue
		case "/show":
			printContext(stdout, sess.Messages(), sess.Usage)
			continue
		case "/help":
			printChatHelp(stdout)
			continue
		}

		// Snapshot before adding user message, for compaction detection.
		var usedBefore, prevCompactions int
		if dev {
			usedBefore, _ = sess.Usage()
			prevCompactions = countSummarized(sess.Messages())
		}

		ctx := context.Background()
		if err := sess.Add(ctx, model.Message{Role: "user", Content: line}); err != nil {
			fmt.Fprintf(stderr, "  error: %v\n\n", err)
			continue
		}

		// Report compaction when summarized message count increases.
		if dev && countSummarized(sess.Messages()) > prevCompactions {
			usedAfter, remainingAfter := sess.Usage()
			fmt.Fprintf(stdout, "  %s\n",
				yellow(fmt.Sprintf("◈ context compacted  %d → %d tokens  remaining=%d",
					usedBefore, usedAfter, remainingAfter)))
		}

		// Show API payload in dev mode before each call.
		if dev {
			apiMsgs := sess.Messages()
			fmt.Fprintf(stdout, "  %s\n", dim(fmt.Sprintf("── api call  %d messages ──", len(apiMsgs))))
			for i, m := range apiMsgs {
				preview := m.Content
				if len(preview) > 72 {
					preview = preview[:69] + "…"
				}
				fmt.Fprintf(stdout, "  %s\n", dim(fmt.Sprintf("  [%d] %-10s %s", i, m.Role, preview)))
			}
		}

		callCtx, callCancel := context.WithTimeout(ctx, cfg.LLMTimeout)
		// SIGINT during the call cancels it; default handler is restored on stop().
		callCtx, stopSig := signal.NotifyContext(callCtx, os.Interrupt, syscall.SIGTERM)
		resp, err := client.Complete(callCtx, cfg.Model, sess.Messages(), completionOpts)
		stopSig()
		callCancel()
		if err != nil {
			fmt.Fprintf(stderr, "  error: %v\n\n", err)
			continue
		}

		// Add the assistant turn to the context window.
		if addErr := sess.Add(ctx, model.Message{Role: "assistant", Content: resp.Content}); addErr != nil {
			fmt.Fprintf(stderr, "  warning: session add: %v\n", addErr)
		}

		// Print agent response with right-aligned label and continuation indent.
		respLines := strings.Split(resp.Content, "\n")
		fmt.Fprintf(stdout, "%s%s %s %s\n", agentPad, boldGreen(agentName), dim("›"), respLines[0])
		for _, rl := range respLines[1:] {
			fmt.Fprintf(stdout, "%s%s\n", contIndent, rl)
		}

		if cfg.Stats || dev {
			used, remaining := sess.Usage()
			var statLine string
			if cfg.Stats {
				statLine = fmt.Sprintf("prompt=%d  completion=%d  total=%d  ctx_used=%d  ctx_remaining=%d",
					resp.PromptTokens, resp.CompletionTokens, resp.TotalTokens, used, remaining)
			} else {
				statLine = fmt.Sprintf("ctx_used=%d  ctx_remaining=%d", used, remaining)
			}
			fmt.Fprintf(stdout, "%s%s\n", contIndent, dim(statLine))
		}
		fmt.Fprintln(stdout)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "leather chat: read error: %v\n", err)
		return 1
	}
	return 0
}

// printChatHelp writes the list of chat commands to w.
func printChatHelp(w io.Writer) {
	cmds := []struct{ name, desc string }{
		{"/show", "print the full context window sent to the model on each API call"},
		{"/reset", "clear conversation context (system prompt is preserved)"},
		{"/stats", "show current token usage and message count"},
		{"/quit", "exit leather chat"},
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %s  %s\n", cyan(fmt.Sprintf("%-7s", c.name)), dim(c.desc))
	}
	fmt.Fprintln(w)
}

// printContext writes the full session context window to w in a human-readable
// format that mirrors the message array sent to the model on each API call.
func printContext(w io.Writer, msgs []model.Message, usage func() (int, int)) {
	used, remaining := usage()
	fmt.Fprintf(w, "  %s\n", bold(fmt.Sprintf(
		"── context window  %d messages  %d tokens used  %d remaining ──",
		len(msgs), used, remaining)))
	for i, m := range msgs {
		sumLabel := ""
		if m.Summarized {
			sumLabel = "  " + yellow("[summarized]")
		}
		fmt.Fprintf(w, "  [%d] %s %s%s\n", i, chatRoleColor(m.Role), dim(fmt.Sprintf("%4d tokens", m.Tokens)), sumLabel)
		// Indent and word-wrap content at 72 chars for readability.
		for _, line := range wrapLines(m.Content, 72) {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
	fmt.Fprintln(w)
}

// chatRoleColor returns the role padded to 12 chars, styled by role.
func chatRoleColor(role string) string {
	padded := fmt.Sprintf("%-12s", role)
	switch role {
	case "system":
		return yellow(padded)
	case "user":
		return cyan(padded)
	case "assistant":
		return green(padded)
	default:
		return padded
	}
}

// wrapLines splits text into lines no longer than width, preserving existing
// newlines. Lines already shorter than width are returned as-is.
func wrapLines(text string, width int) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if len(para) <= width {
			out = append(out, para)
			continue
		}
		for len(para) > width {
			cut := width
			// Break at the last space within width.
			if idx := strings.LastIndex(para[:cut], " "); idx > 0 {
				cut = idx
			}
			out = append(out, para[:cut])
			para = strings.TrimLeft(para[cut:], " ")
		}
		if para != "" {
			out = append(out, para)
		}
	}
	return out
}

// countSummarized returns the number of messages flagged as summarized.
func countSummarized(msgs []model.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Summarized {
			n++
		}
	}
	return n
}
