-- Sentrix: Initial database schema
-- Requires PostgreSQL 16+ with pgvector extension

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

-- ============================================================
-- Users
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255) NOT NULL DEFAULT '',
    role          VARCHAR(50)  NOT NULL DEFAULT 'user',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- ============================================================
-- Security assessment flows
-- ============================================================
CREATE TABLE IF NOT EXISTS flows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       VARCHAR(255) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(50)  NOT NULL DEFAULT 'pending',
    config      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_flows_user_id ON flows(user_id);
CREATE INDEX IF NOT EXISTS idx_flows_status  ON flows(status);

-- ============================================================
-- Tasks within a flow
-- ============================================================
CREATE TABLE IF NOT EXISTS tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id     UUID         NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    title       VARCHAR(255) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(50)  NOT NULL DEFAULT 'pending',
    result      TEXT,
    sort_order  INT          NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tasks_flow_id ON tasks(flow_id);

-- ============================================================
-- Subtasks assigned to agents
-- ============================================================
CREATE TABLE IF NOT EXISTS subtasks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title      VARCHAR(255) NOT NULL,
    agent_role VARCHAR(50)  NOT NULL,
    status     VARCHAR(50)  NOT NULL DEFAULT 'pending',
    context    JSONB        NOT NULL DEFAULT '{}',
    result     TEXT,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_subtasks_task_id ON subtasks(task_id);

-- ============================================================
-- Actions executed by agents
-- ============================================================
CREATE TABLE IF NOT EXISTS actions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subtask_id  UUID         NOT NULL REFERENCES subtasks(id) ON DELETE CASCADE,
    action_type VARCHAR(100) NOT NULL,
    status      VARCHAR(50)  NOT NULL DEFAULT 'pending',
    input       JSONB        NOT NULL DEFAULT '{}',
    output      TEXT,
    duration_ms INT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_actions_subtask_id ON actions(subtask_id);

-- ============================================================
-- Artifacts produced by actions
-- ============================================================
CREATE TABLE IF NOT EXISTS artifacts (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id UUID         NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    kind      VARCHAR(100) NOT NULL,
    file_path VARCHAR(500),
    content   TEXT,
    metadata  JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_artifacts_action_id ON artifacts(action_id);

-- ============================================================
-- Vector memory store
-- ============================================================
CREATE TABLE IF NOT EXISTS memories (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category  VARCHAR(100) NOT NULL,
    content   TEXT         NOT NULL,
    embedding vector(1536),
    metadata  JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memories_user_id ON memories(user_id);

-- ============================================================
-- Bearer API tokens
-- ============================================================
CREATE TABLE IF NOT EXISTS api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        VARCHAR(255) NOT NULL,
    token_hash   VARCHAR(255) UNIQUE NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id);

-- ============================================================
-- LLM provider configurations
-- ============================================================
CREATE TABLE IF NOT EXISTS provider_configs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type     VARCHAR(50)  NOT NULL,
    model_name        VARCHAR(255) NOT NULL DEFAULT '',
    api_key_encrypted TEXT,
    base_url          VARCHAR(500),
    is_default        BOOLEAN      NOT NULL DEFAULT false,
    config            JSONB        NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_provider_configs_user_id ON provider_configs(user_id);

-- ============================================================
-- Conversation message chains
-- ============================================================
CREATE TABLE IF NOT EXISTS message_chains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subtask_id  UUID REFERENCES subtasks(id) ON DELETE CASCADE,
    role        VARCHAR(50) NOT NULL,
    content     TEXT        NOT NULL,
    token_count INT         NOT NULL DEFAULT 0,
    metadata    JSONB       NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_message_chains_subtask_id ON message_chains(subtask_id);
