// Package hide implements the HideBuffer — an in-process store for large tool
// outputs. Agents receive bounded cut pages instead of the full raw content,
// preserving jurisdiction without saturating the context window.
//
// Design rule: paging is not truncation — it is jurisdiction.
package hide

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// Hide is a stored large raw tool result.
type Hide struct {
	// ID is the unique buffer identifier in the form
	// "hide_<sanitized-source>_<yyyymmdd>_<HHMM>_<4-hex>".
	ID string
	// Source is the tool name that produced this hide.
	Source string
	// Content is the full raw content — never sent to the agent directly.
	Content string
	// CreatedAt is the time the hide was stored.
	CreatedAt time.Time
}

// Cut is a bounded page view of a hide. PageNumber is 1-indexed.
type Cut struct {
	// HideID is the ID of the parent hide.
	HideID string
	// Source is the tool name that produced the parent hide.
	Source string
	// PageNumber is the 1-indexed page number of this cut.
	PageNumber int
	// TotalPages is the total number of pages in the parent hide.
	TotalPages int
	// SizeBytes is the byte count of this cut's Content.
	SizeBytes int
	// Content is the substring of Hide.Content for this page.
	Content string
	// IsFinal is true when PageNumber == TotalPages.
	IsFinal bool
	// ReflectionHint, when non-empty, replaces the default navigation/final-page
	// hint in Format(). Use it to embed per-page instructions (e.g. "list key
	// facts from this page") directly inside the tool result so the model sees
	// them as part of the page content, not as a separate user turn.
	ReflectionHint string
}

// Format returns a plain-text envelope for this cut suitable for delivery to
// an LLM. It is token-efficient, self-contained, and includes a navigation
// hint so the agent knows how to request additional pages.
func (c Cut) Format() string {
	var hint string
	if c.ReflectionHint != "" {
		hint = c.ReflectionHint
		if c.IsFinal {
			hint = "This is the final page. No further pages remain. " + hint
		}
	} else if c.IsFinal {
		hint = "This is the complete content — no further pages."
	} else {
		hint = "Call hide_next or hide_jump to retrieve more pages."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[HIDE id=%s source=%s page=%d/%d bytes=%d]\n",
		c.HideID, c.Source, c.PageNumber, c.TotalPages, c.SizeBytes)
	b.WriteString("Do not infer from unseen pages. ")
	b.WriteString(hint)
	b.WriteByte('\n')
	b.WriteString(c.Content)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "[END page %d/%d]", c.PageNumber, c.TotalPages)
	return b.String()
}

// HideBuffer is an in-process store for hides. It is safe for concurrent use.
type HideBuffer struct {
	mu             sync.Mutex
	hides          map[string]*Hide
	pageSize       int    // bytes per cut; guarded by being set once at construction
	ReflectionHint string // optional per-page instruction embedded in non-final Cut headers
}

// NewHideBuffer creates a new HideBuffer. pageSize is the number of bytes in
// each cut; values <= 0 default to 3800.
func NewHideBuffer(pageSize int) *HideBuffer {
	if pageSize <= 0 {
		pageSize = 3800
	}
	return &HideBuffer{
		hides:    make(map[string]*Hide),
		pageSize: pageSize,
	}
}

// Store adds content to the buffer and returns the newly created Hide.
func (b *HideBuffer) Store(source, content string) *Hide {
	h := &Hide{
		ID:        generateHideID(source),
		Source:    source,
		Content:   content,
		CreatedAt: time.Now(),
	}
	b.mu.Lock()
	b.hides[h.ID] = h
	b.mu.Unlock()
	return h
}

func (b *HideBuffer) storeWithID(id, source, content string, createdAt time.Time) *Hide {
	h := &Hide{
		ID:        id,
		Source:    source,
		Content:   content,
		CreatedAt: createdAt,
	}
	b.mu.Lock()
	b.hides[h.ID] = h
	b.mu.Unlock()
	return h
}

// Get retrieves a Hide by ID. Returns (nil, false) when the ID is not found.
func (b *HideBuffer) Get(id string) (*Hide, bool) {
	b.mu.Lock()
	h, ok := b.hides[id]
	b.mu.Unlock()
	return h, ok
}

// ResolveID returns id when it exists, or the only active hide ID when the
// buffer contains exactly one hide. The fallback is scoped to this buffer so a
// model cannot navigate arbitrary persisted hides by guessing IDs.
func (b *HideBuffer) ResolveID(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id != "" {
		if _, ok := b.hides[id]; ok {
			return id, true
		}
	}
	if len(b.hides) != 1 {
		return "", false
	}
	for activeID := range b.hides {
		return activeID, true
	}
	return "", false
}

// Cut returns a bounded page of a hide. page is 1-indexed.
// Returns an error when id is not found or page is out of range (< 1 or > TotalPages).
func (b *HideBuffer) Cut(id string, page int) (Cut, error) {
	b.mu.Lock()
	h, ok := b.hides[id]
	hint := b.ReflectionHint
	b.mu.Unlock()
	if !ok {
		return Cut{}, fmt.Errorf("hide/Cut: unknown hide id %q", id)
	}
	return makeCut(h, page, b.pageSize, hint)
}

// Search returns the first Cut containing query (case-insensitive byte scan).
// Returns (cut, true, nil) on hit; (page-1 cut, false, nil) on miss; error on bad id.
func (b *HideBuffer) Search(id, query string) (Cut, bool, error) {
	b.mu.Lock()
	h, ok := b.hides[id]
	hint := b.ReflectionHint
	b.mu.Unlock()
	if !ok {
		return Cut{}, false, fmt.Errorf("hide/Search: unknown hide id %q", id)
	}
	if query == "" {
		cut, err := makeCut(h, 1, b.pageSize, hint)
		return cut, false, err
	}
	lowerContent := strings.ToLower(h.Content)
	lowerQuery := strings.ToLower(query)
	total := totalPages(len(h.Content), b.pageSize)
	for page := 1; page <= total; page++ {
		start := (page - 1) * b.pageSize
		end := start + b.pageSize
		if end > len(h.Content) {
			end = len(h.Content)
		}
		if bytes.Contains([]byte(lowerContent[start:end]), []byte(lowerQuery)) {
			cut, err := makeCut(h, page, b.pageSize, hint)
			return cut, true, err
		}
	}
	// Not found — return page 1 as a fallback.
	cut, err := makeCut(h, 1, b.pageSize, hint)
	return cut, false, err
}

// ToolDefs returns the three built-in hide navigation tool definitions.
// These are injected into every turn's tool scope when a HideBuffer is configured.
func ToolDefs() []model.ToolDefinition {
	hideIDProp := map[string]any{
		"type":        "string",
		"description": "The hide ID from the [HIDE id=...] header in the content you received.",
	}
	return []model.ToolDefinition{
		{
			Name: "hide_next",
			Type: "hide",
			Description: "Retrieve the next page of a buffered hide. " +
				"Use when the hide envelope shows more pages are available. " +
				"Pass the hide_id from the [HIDE id=...] header and the current page number.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hide_id":      hideIDProp,
					"current_page": map[string]any{"type": "integer", "description": "The page number shown in the current [HIDE ...] header."},
				},
				"required": []string{"hide_id", "current_page"},
			},
		},
		{
			Name: "hide_jump",
			Type: "hide",
			Description: "Retrieve a specific page of a buffered hide by page number. " +
				"Use when you know which page contains the content you need. " +
				"Pass the hide_id from the [HIDE id=...] header and the target page number.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hide_id": hideIDProp,
					"page":    map[string]any{"type": "integer", "description": "The 1-indexed page number to retrieve."},
				},
				"required": []string{"hide_id", "page"},
			},
		},
		{
			Name: "hide_search",
			Type: "hide",
			Description: "Search a buffered hide for a query string (case-insensitive). " +
				"Returns the first page containing the query. " +
				"Pass the hide_id from the [HIDE id=...] header and the search query.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hide_id": hideIDProp,
					"query":   map[string]any{"type": "string", "description": "Case-insensitive search term."},
				},
				"required": []string{"hide_id", "query"},
			},
		},
	}
}

// NeedsPaging reports whether any hide in the buffer has more than one page.
// Used by callers to decide whether to include hide navigation tools in the
// model's tool scope — there is no point advertising hide_next when all
// content fits on page 1.
func (b *HideBuffer) NeedsPaging() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.hides {
		if totalPages(len(h.Content), b.pageSize) > 1 {
			return true
		}
	}
	return false
}

// FirstCut returns the first cut (page 1) of the first hide in the buffer.
// Returns an error when the buffer is empty.
func (b *HideBuffer) FirstCut() (Cut, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.hides {
		return makeCut(h, 1, b.pageSize, b.ReflectionHint)
	}
	return Cut{}, fmt.Errorf("hide/FirstCut: no hides in buffer")
}

// generateHideID generates a unique hide identifier from source.
// Format: "hide_<sanitized>_<yyyymmdd>_<HHMM>_<4-hex>"
func generateHideID(source string) string {
	sanitized := sanitizeSource(source)
	ts := time.Now().Format("20060102_1504")
	suffix := fmt.Sprintf("%04x", rand.Intn(0x10000)) //nolint:gosec // ID uniqueness only, not security
	return "hide_" + sanitized + "_" + ts + "_" + suffix
}

// sanitizeSource replaces all characters that are not [a-z0-9_] with '_'.
func sanitizeSource(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// makeCut builds a Cut for the given hide and 1-indexed page number.
// reflectionHint, when non-empty, is stored on the Cut and embedded in Format()
// for non-final pages in place of the default navigation hint.
func makeCut(h *Hide, page, pageSize int, reflectionHint string) (Cut, error) {
	total := totalPages(len(h.Content), pageSize)
	if total == 0 {
		total = 1 // empty content is a single empty page
	}
	if page < 1 || page > total {
		return Cut{}, fmt.Errorf("hide/Cut: page %d out of range [1,%d] for hide %q", page, total, h.ID)
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(h.Content) {
		end = len(h.Content)
	}
	content := h.Content[start:end]
	return Cut{
		HideID:         h.ID,
		Source:         h.Source,
		PageNumber:     page,
		TotalPages:     total,
		SizeBytes:      len(content),
		Content:        content,
		IsFinal:        page == total,
		ReflectionHint: reflectionHint,
	}, nil
}

// totalPages computes the number of pages for a content of size bytes with the given pageSize.
func totalPages(size, pageSize int) int {
	if size == 0 {
		return 1
	}
	pages := size / pageSize
	if size%pageSize != 0 {
		pages++
	}
	return pages
}
