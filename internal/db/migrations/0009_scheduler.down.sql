-- Reverse 0009: drop agent_job_runs first (it FKs scheduler_tasks), then
-- scheduler_tasks (the two indexes drop with their tables).
DROP TABLE IF EXISTS aura.agent_job_runs;
DROP TABLE IF EXISTS aura.scheduler_tasks;
