package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/embedding"
)

// MemoryTier classifies how memory is scoped and retained.
type MemoryTier string

const (
	TierLongTerm MemoryTier = "long_term"
	TierWorking  MemoryTier = "working"
	TierEpisodic MemoryTier = "episodic"
)

// Category labels for organizing stored memories.
const (
	CategoryObservation   = "observation"
	CategoryConclusion    = "conclusion"
	CategoryToolOutput    = "tool_output"
	CategoryVulnerability = "vulnerability"
	CategoryTechnique     = "technique"
	CategoryGeneral       = "general"
)

// MemoryEntry represents data to be stored in the memory system.
type MemoryEntry struct {
	UserID   uuid.UUID
	FlowID   *uuid.UUID
	Tier     MemoryTier
	Category string
	Content  string
	Metadata map[string]interface{}
}

// MemoryResult is a search result with similarity score.
type MemoryResult struct {
	ID        uuid.UUID              `json:"id"`
	Tier      string                 `json:"tier"`
	Category  string                 `json:"category"`
	Content   string                 `json:"content"`
	Score     float64                `json:"score"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
}

// SearchOpts controls similarity search behaviour.
type SearchOpts struct {
	FlowID    *uuid.UUID // nil = search all flows for user
	Tier      MemoryTier // empty = all tiers
	Category  string     // empty = all categories
	Limit     int        // 0 → default (5)
	Threshold float64    // 0 → default (0.2)
}

func (o SearchOpts) effectiveLimit() int {
	if o.Limit > 0 {
		return o.Limit
	}
	return 5
}

func (o SearchOpts) effectiveThreshold() float64 {
	if o.Threshold > 0 {
		return o.Threshold
	}
	return 0.2
}

// MemoryStore is the primary interface for persisting and querying vector memories.
type MemoryStore struct {
	db       *gorm.DB
	embedder embedding.Embedder
}

// NewMemoryStore creates a store backed by PostgreSQL+pgvector and the given embedder.
func NewMemoryStore(db *gorm.DB, embedder embedding.Embedder) *MemoryStore {
	return &MemoryStore{
		db:       db,
		embedder: embedder,
	}
}

// Enabled reports whether the memory system has a working embedder.
func (s *MemoryStore) Enabled() bool {
	return s.embedder.Available()
}

// Store persists a memory entry with its computed embedding.
func (s *MemoryStore) Store(ctx context.Context, entry MemoryEntry) (*database.Memory, error) {
	if entry.Content == "" {
		return nil, fmt.Errorf("memory: cannot store empty content")
	}

	tier := entry.Tier
	if tier == "" {
		tier = TierLongTerm
	}
	category := entry.Category
	if category == "" {
		category = CategoryGeneral
	}

	// Generate embedding vector.
	vec, err := s.embedder.Embed(ctx, entry.Content)
	if err != nil {
		log.WithError(err).Warn("memory: embedding failed, storing without vector")
		vec = nil
	}

	metaJSON := "{}"
	if entry.Metadata != nil {
		if raw, err := marshalJSON(entry.Metadata); err == nil {
			metaJSON = string(raw)
		}
	}

	record := database.Memory{
		UserID:    entry.UserID,
		FlowID:    entry.FlowID,
		Tier:      string(tier),
		Category:  category,
		Content:   entry.Content,
		Embedding: vec,
		Metadata:  metaJSON,
	}

	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, fmt.Errorf("memory: insert failed: %w", err)
	}

	log.WithFields(log.Fields{
		"id":       record.ID.String()[:8],
		"tier":     tier,
		"category": category,
		"has_vec":  vec != nil,
	}).Debug("memory: stored entry")

	return &record, nil
}

// Search performs cosine similarity search against stored memories.
func (s *MemoryStore) Search(ctx context.Context, userID uuid.UUID, query string, opts SearchOpts) ([]MemoryResult, error) {
	if query == "" {
		return nil, fmt.Errorf("memory: search query cannot be empty")
	}

	// Embed the query.
	queryVec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory: embed query: %w", err)
	}

	// Build the pgvector cosine distance query.
	// 1 - (embedding <=> query_vec) gives us cosine similarity.
	limit := opts.effectiveLimit()
	threshold := opts.effectiveThreshold()

	vecLiteral := vectorLiteral(queryVec)

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "user_id = ?")
	args = append(args, userID)

	if opts.FlowID != nil {
		conditions = append(conditions, "flow_id = ?")
		args = append(args, *opts.FlowID)
	}
	if opts.Tier != "" {
		conditions = append(conditions, "tier = ?")
		args = append(args, string(opts.Tier))
	}
	if opts.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, opts.Category)
	}

	// Require non-null embedding for similarity search.
	conditions = append(conditions, "embedding IS NOT NULL")

	whereClause := strings.Join(conditions, " AND ")

	sql := fmt.Sprintf(`
		SELECT id, tier, category, content, metadata, created_at,
		       1 - (embedding <=> '%s'::vector) AS score
		FROM memories
		WHERE %s
		  AND 1 - (embedding <=> '%s'::vector) >= ?
		ORDER BY score DESC
		LIMIT ?
	`, vecLiteral, whereClause, vecLiteral)

	args = append(args, threshold, limit)

	type row struct {
		ID        uuid.UUID `gorm:"column:id"`
		Tier      string    `gorm:"column:tier"`
		Category  string    `gorm:"column:category"`
		Content   string    `gorm:"column:content"`
		Metadata  string    `gorm:"column:metadata"`
		CreatedAt time.Time `gorm:"column:created_at"`
		Score     float64   `gorm:"column:score"`
	}

	var rows []row
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("memory: similarity search: %w", err)
	}

	results := make([]MemoryResult, 0, len(rows))
	for _, r := range rows {
		meta := parseMetadata(r.Metadata)
		results = append(results, MemoryResult{
			ID:        r.ID,
			Tier:      r.Tier,
			Category:  r.Category,
			Content:   r.Content,
			Score:     r.Score,
			Metadata:  meta,
			CreatedAt: r.CreatedAt,
		})
	}

	log.WithFields(log.Fields{
		"query":   truncate(query, 80),
		"results": len(results),
		"limit":   limit,
	}).Debug("memory: search completed")

	return results, nil
}

// Delete removes a memory entry by ID.
func (s *MemoryStore) Delete(ctx context.Context, id uuid.UUID) error {
	result := s.db.WithContext(ctx).Delete(&database.Memory{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("memory: delete: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("memory: entry %s not found", id)
	}
	return nil
}

// ListByFlow returns all memories for a given flow, ordered by creation time.
func (s *MemoryStore) ListByFlow(ctx context.Context, flowID uuid.UUID) ([]MemoryResult, error) {
	var records []database.Memory
	err := s.db.WithContext(ctx).
		Where("flow_id = ?", flowID).
		Order("created_at DESC").
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("memory: list by flow: %w", err)
	}

	results := make([]MemoryResult, 0, len(records))
	for _, r := range records {
		results = append(results, MemoryResult{
			ID:        r.ID,
			Tier:      r.Tier,
			Category:  r.Category,
			Content:   r.Content,
			Score:     1.0,
			Metadata:  parseMetadata(r.Metadata),
			CreatedAt: r.CreatedAt,
		})
	}
	return results, nil
}

// ListByUser returns all memories for a user with pagination.
func (s *MemoryStore) ListByUser(ctx context.Context, userID uuid.UUID, offset, limit int) ([]MemoryResult, int64, error) {
	if limit <= 0 {
		limit = 20
	}

	var total int64
	s.db.WithContext(ctx).Model(&database.Memory{}).Where("user_id = ?", userID).Count(&total)

	var records []database.Memory
	err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, 0, fmt.Errorf("memory: list by user: %w", err)
	}

	results := make([]MemoryResult, 0, len(records))
	for _, r := range records {
		results = append(results, MemoryResult{
			ID:        r.ID,
			Tier:      r.Tier,
			Category:  r.Category,
			Content:   r.Content,
			Score:     1.0,
			Metadata:  parseMetadata(r.Metadata),
			CreatedAt: r.CreatedAt,
		})
	}
	return results, total, nil
}
