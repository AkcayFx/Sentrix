-- Agent planning foundation:
-- richer subtasks and task-level message chains

ALTER TABLE subtasks
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_subtasks_task_sort_order ON subtasks(task_id, sort_order);

ALTER TABLE message_chains
    ADD COLUMN IF NOT EXISTS flow_id UUID REFERENCES flows(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS agent_role VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS chain_type VARCHAR(50) NOT NULL DEFAULT 'subtask_execution';

UPDATE message_chains AS mc
SET
    task_id = s.task_id,
    flow_id = t.flow_id,
    agent_role = COALESCE(NULLIF(mc.agent_role, ''), NULLIF(mc.metadata->>'agent_role', ''), 'unknown'),
    chain_type = COALESCE(NULLIF(mc.chain_type, ''), 'subtask_execution')
FROM subtasks AS s
JOIN tasks AS t ON t.id = s.task_id
WHERE mc.subtask_id = s.id
  AND (mc.task_id IS NULL OR mc.flow_id IS NULL OR mc.agent_role = '');

UPDATE message_chains
SET agent_role = COALESCE(NULLIF(agent_role, ''), NULLIF(metadata->>'agent_role', ''), 'unknown')
WHERE agent_role = '';

CREATE INDEX IF NOT EXISTS idx_message_chains_flow_id ON message_chains(flow_id);
CREATE INDEX IF NOT EXISTS idx_message_chains_task_id ON message_chains(task_id);
CREATE INDEX IF NOT EXISTS idx_message_chains_chain_type ON message_chains(chain_type);
