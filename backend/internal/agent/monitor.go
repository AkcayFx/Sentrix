package agent

import (
	"fmt"
	"sync"
)

// ExecutionMonitor tracks tool usage to prevent infinite loops.
type ExecutionMonitor struct {
	mu             sync.Mutex
	sameToolLimit  int
	totalToolLimit int
	totalCalls     int
	consecutive    int
	lastTool       string
	warnedSame     bool
	warnedTotal    bool
}

// NewExecutionMonitor creates a monitor with configurable limits.
func NewExecutionMonitor(sameToolLimit, totalToolLimit int) *ExecutionMonitor {
	if sameToolLimit <= 0 {
		sameToolLimit = 5
	}
	if totalToolLimit <= 0 {
		totalToolLimit = 30
	}
	return &ExecutionMonitor{
		sameToolLimit:  sameToolLimit,
		totalToolLimit: totalToolLimit,
	}
}

// RecordToolCall records that a tool was called.
func (m *ExecutionMonitor) RecordToolCall(toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++

	if toolName == m.lastTool {
		m.consecutive++
	} else {
		m.consecutive = 1
		m.lastTool = toolName
		m.warnedSame = false
	}
}

// Check returns an error if any limit has been exceeded.
func (m *ExecutionMonitor) Check() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.totalCalls >= m.totalToolLimit {
		return fmt.Errorf("total tool call limit exceeded (%d/%d)", m.totalCalls, m.totalToolLimit)
	}

	if m.consecutive >= m.sameToolLimit {
		return fmt.Errorf("same tool call limit exceeded for '%s' (%d/%d)", m.lastTool, m.consecutive, m.sameToolLimit)
	}

	return nil
}

// TotalCalls returns the current total tool call count.
func (m *ExecutionMonitor) TotalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalCalls
}

// Stats returns current execution stats.
func (m *ExecutionMonitor) Stats() (totalCalls int, lastTool string, consecutive int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalCalls, m.lastTool, m.consecutive
}

// ShouldAdvise returns true once when execution is approaching a limit.
func (m *ExecutionMonitor) ShouldAdvise() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.totalCalls >= max(m.totalToolLimit-3, 1) && !m.warnedTotal {
		m.warnedTotal = true
		return true, fmt.Sprintf("Execution is close to the total tool limit (%d/%d).", m.totalCalls, m.totalToolLimit)
	}

	if m.consecutive >= max(m.sameToolLimit-1, 1) && m.lastTool != "" && !m.warnedSame {
		m.warnedSame = true
		return true, fmt.Sprintf("The same tool is repeating and is close to the limit: %s (%d/%d).", m.lastTool, m.consecutive, m.sameToolLimit)
	}

	return false, ""
}

// Reset clears all counters for reuse.
func (m *ExecutionMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalCalls = 0
	m.consecutive = 0
	m.lastTool = ""
	m.warnedSame = false
	m.warnedTotal = false
}
