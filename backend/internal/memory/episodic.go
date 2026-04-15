package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// Episode captures a single action→outcome pair that the system can learn from.
type Episode struct {
	ID        uuid.UUID `json:"id"`
	Action    string    `json:"action"`
	Outcome   string    `json:"outcome"`
	Succeeded bool      `json:"succeeded"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

// EpisodicMemory records past execution episodes and retrieves similar ones
// to inform future agent decision-making. It uses the same MemoryStore
// under the hood, filtering by the "episodic" tier.
type EpisodicMemory struct {
	store  *MemoryStore
	userID uuid.UUID
	flowID uuid.UUID
}

// NewEpisodicMemory creates an episodic memory scoped to a user and flow.
func NewEpisodicMemory(store *MemoryStore, userID, flowID uuid.UUID) *EpisodicMemory {
	return &EpisodicMemory{
		store:  store,
		userID: userID,
		flowID: flowID,
	}
}

// RecordEpisode stores an action→outcome pair as an episodic memory.
// The content is formatted to capture the causal relationship so future
// similarity searches can match on either the action or outcome.
func (em *EpisodicMemory) RecordEpisode(ctx context.Context, action, outcome string, succeeded bool) error {
	if !em.store.Enabled() {
		return nil // silently skip when embeddings are disabled
	}

	resultLabel := "SUCCESS"
	if !succeeded {
		resultLabel = "FAILURE"
	}

	content := fmt.Sprintf(
		"Action: %s\nOutcome [%s]: %s",
		strings.TrimSpace(action),
		resultLabel,
		strings.TrimSpace(outcome),
	)

	meta := map[string]interface{}{
		"succeeded": succeeded,
		"action":    truncate(action, 500),
		"recorded":  time.Now().UTC().Format(time.RFC3339),
	}

	flowID := em.flowID
	_, err := em.store.Store(ctx, MemoryEntry{
		UserID:   em.userID,
		FlowID:   &flowID,
		Tier:     TierEpisodic,
		Category: CategoryTechnique,
		Content:  content,
		Metadata: meta,
	})
	if err != nil {
		log.WithError(err).Warn("episodic: failed to record episode")
		return fmt.Errorf("episodic: record: %w", err)
	}

	return nil
}

// RecallSimilar retrieves past episodes that are semantically similar
// to the described situation. Useful for informing an agent about what
// worked or failed in comparable circumstances.
func (em *EpisodicMemory) RecallSimilar(ctx context.Context, situation string, limit int) ([]Episode, error) {
	if !em.store.Enabled() {
		return nil, nil
	}

	if limit <= 0 {
		limit = 3
	}

	results, err := em.store.Search(ctx, em.userID, situation, SearchOpts{
		Tier:      TierEpisodic,
		Limit:     limit,
		Threshold: 0.15, // slightly more generous for episodic recall
	})
	if err != nil {
		return nil, fmt.Errorf("episodic: recall: %w", err)
	}

	episodes := make([]Episode, 0, len(results))
	for _, r := range results {
		succeeded, _ := r.Metadata["succeeded"].(bool)

		actionText := ""
		if a, ok := r.Metadata["action"].(string); ok {
			actionText = a
		}

		episodes = append(episodes, Episode{
			ID:        r.ID,
			Action:    actionText,
			Outcome:   r.Content,
			Succeeded: succeeded,
			Score:     r.Score,
			CreatedAt: r.CreatedAt,
		})
	}

	return episodes, nil
}

// FormatForPrompt converts recalled episodes into text suitable for
// injection into an agent system prompt.
func FormatEpisodesForPrompt(episodes []Episode) string {
	if len(episodes) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Relevant Past Experience\n\n")

	for i, ep := range episodes {
		status := "✅ Succeeded"
		if !ep.Succeeded {
			status = "❌ Failed"
		}
		fmt.Fprintf(&b, "### Episode %d (similarity: %.0f%%) — %s\n", i+1, ep.Score*100, status)
		fmt.Fprintf(&b, "%s\n\n", ep.Outcome)
	}

	return b.String()
}
