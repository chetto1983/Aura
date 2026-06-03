-- Reverse 0008: drop the sandbox_sessions table (the status/last_used index drops with it).
DROP TABLE IF EXISTS aura.sandbox_sessions;
