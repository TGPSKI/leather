package cache

import (
	"crypto/sha256"
	"fmt"
)

// AgentRunKey computes a sha256 cache key that uniquely identifies an agent
// execution by its observable inputs. Fields are joined with NUL bytes so that
// no field value can shift the boundary into an adjacent field.
//
// The returned string is a 64-character lowercase hex digest, safe to use as a
// filename component.
func AgentRunKey(agentName, systemPrompt, userPrompt, model string) string {
	h := sha256.New()
	for _, s := range []string{agentName, systemPrompt, userPrompt, model} {
		h.Write([]byte(s))
		h.Write([]byte{0}) // NUL separator
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
