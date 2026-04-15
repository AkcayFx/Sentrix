package memory

import (
	"fmt"
	"strings"
	"sync"
)

// WorkingMemory holds volatile, in-process state for a single flow execution.
// It provides the agent with awareness of current goals, accumulated context,
// and a scratchpad for intermediate reasoning — none of which persists to DB.
type WorkingMemory struct {
	mu sync.RWMutex

	goals       []string
	contextLog  []contextEntry
	findings    []string
	scratchpad  map[string]string
	iterationNo int
}

type contextEntry struct {
	Source  string // e.g. "researcher", "pentester", "tool:nmap"
	Detail string
}

// NewWorkingMemory creates an empty working memory for a flow.
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		scratchpad: make(map[string]string),
	}
}

// SetGoals replaces the current goal list.
func (w *WorkingMemory) SetGoals(goals []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.goals = append([]string{}, goals...)
}

// AddContext appends a context observation from a source (agent or tool).
func (w *WorkingMemory) AddContext(source, detail string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.contextLog = append(w.contextLog, contextEntry{Source: source, Detail: detail})

	// Keep the log bounded to avoid unbounded growth during long flows.
	const maxEntries = 200
	if len(w.contextLog) > maxEntries {
		w.contextLog = w.contextLog[len(w.contextLog)-maxEntries:]
	}
}

// AddFinding records a notable discovery during execution.
func (w *WorkingMemory) AddFinding(finding string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.findings = append(w.findings, finding)
}

// SetScratch writes a key-value into the scratchpad.
func (w *WorkingMemory) SetScratch(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scratchpad[key] = value
}

// GetScratch reads a value from the scratchpad.
func (w *WorkingMemory) GetScratch(key string) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	v, ok := w.scratchpad[key]
	return v, ok
}

// IncrementIteration advances the iteration counter and returns the new value.
func (w *WorkingMemory) IncrementIteration() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.iterationNo++
	return w.iterationNo
}

// Summary produces a condensed text representation of the current working state,
// suitable for injection into an agent's system prompt.
func (w *WorkingMemory) Summary() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var b strings.Builder

	if len(w.goals) > 0 {
		b.WriteString("## Current Goals\n")
		for i, g := range w.goals {
			fmt.Fprintf(&b, "%d. %s\n", i+1, g)
		}
		b.WriteByte('\n')
	}

	if len(w.findings) > 0 {
		b.WriteString("## Key Findings So Far\n")
		for _, f := range w.findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteByte('\n')
	}

	// Show only the most recent context entries to keep prompt size reasonable.
	recentCount := 15
	if len(w.contextLog) > 0 {
		b.WriteString("## Recent Context\n")
		start := 0
		if len(w.contextLog) > recentCount {
			start = len(w.contextLog) - recentCount
		}
		for _, entry := range w.contextLog[start:] {
			fmt.Fprintf(&b, "- [%s] %s\n", entry.Source, truncate(entry.Detail, 300))
		}
		b.WriteByte('\n')
	}

	if b.Len() == 0 {
		return ""
	}

	return "# Working Memory\n\n" + b.String()
}

// Clear resets all working memory state.
func (w *WorkingMemory) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.goals = nil
	w.contextLog = nil
	w.findings = nil
	w.scratchpad = make(map[string]string)
	w.iterationNo = 0
}
