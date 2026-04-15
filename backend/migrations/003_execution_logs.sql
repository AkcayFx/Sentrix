-- Phase 1: Persisted execution log tables

CREATE TABLE IF NOT EXISTS agent_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id    UUID         NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    task_id    UUID             REFERENCES tasks(id) ON DELETE CASCADE,
    subtask_id UUID             REFERENCES subtasks(id) ON DELETE CASCADE,
    agent_role VARCHAR(50)  NOT NULL,
    event_type VARCHAR(50)  NOT NULL,
    message    TEXT         NOT NULL,
    metadata   JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_logs_flow_id ON agent_logs(flow_id);
CREATE INDEX IF NOT EXISTS idx_agent_logs_subtask_id ON agent_logs(subtask_id);
CREATE INDEX IF NOT EXISTS idx_agent_logs_created_at ON agent_logs(created_at);

CREATE TABLE IF NOT EXISTS terminal_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id    UUID         NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    task_id    UUID             REFERENCES tasks(id) ON DELETE CASCADE,
    subtask_id UUID             REFERENCES subtasks(id) ON DELETE CASCADE,
    stream_type VARCHAR(20) NOT NULL,
    command    TEXT,
    content    TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_terminal_logs_flow_id ON terminal_logs(flow_id);
CREATE INDEX IF NOT EXISTS idx_terminal_logs_subtask_id ON terminal_logs(subtask_id);
CREATE INDEX IF NOT EXISTS idx_terminal_logs_created_at ON terminal_logs(created_at);

CREATE TABLE IF NOT EXISTS search_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id     UUID         NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    task_id     UUID             REFERENCES tasks(id) ON DELETE CASCADE,
    subtask_id  UUID             REFERENCES subtasks(id) ON DELETE CASCADE,
    tool_name   VARCHAR(50)  NOT NULL,
    provider    VARCHAR(50)  NOT NULL DEFAULT '',
    query       TEXT         NOT NULL DEFAULT '',
    target      TEXT         NOT NULL DEFAULT '',
    result_count INT         NOT NULL DEFAULT 0,
    summary     TEXT         NOT NULL DEFAULT '',
    metadata    JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_search_logs_flow_id ON search_logs(flow_id);
CREATE INDEX IF NOT EXISTS idx_search_logs_subtask_id ON search_logs(subtask_id);
CREATE INDEX IF NOT EXISTS idx_search_logs_created_at ON search_logs(created_at);

CREATE TABLE IF NOT EXISTS vector_store_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id     UUID         NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    task_id     UUID             REFERENCES tasks(id) ON DELETE CASCADE,
    subtask_id  UUID             REFERENCES subtasks(id) ON DELETE CASCADE,
    action      VARCHAR(20)  NOT NULL,
    query       TEXT         NOT NULL DEFAULT '',
    content     TEXT         NOT NULL DEFAULT '',
    result_count INT         NOT NULL DEFAULT 0,
    metadata    JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_vector_store_logs_flow_id ON vector_store_logs(flow_id);
CREATE INDEX IF NOT EXISTS idx_vector_store_logs_subtask_id ON vector_store_logs(subtask_id);
CREATE INDEX IF NOT EXISTS idx_vector_store_logs_created_at ON vector_store_logs(created_at);
