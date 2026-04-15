-- Phase 8: Vector memory system

CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE memories ADD COLUMN IF NOT EXISTS embedding vector(1536);
ALTER TABLE memories ADD COLUMN IF NOT EXISTS flow_id UUID REFERENCES flows(id) ON DELETE SET NULL;
ALTER TABLE memories ADD COLUMN IF NOT EXISTS tier VARCHAR(20) NOT NULL DEFAULT 'long_term';

CREATE INDEX IF NOT EXISTS idx_memories_embedding
  ON memories USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);

CREATE INDEX IF NOT EXISTS idx_memories_flow_id ON memories(flow_id);
CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories(tier);
CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
