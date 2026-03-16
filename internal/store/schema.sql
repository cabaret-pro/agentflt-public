-- Sessions: one row per agent session
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    repo_path TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'running',
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    last_output_at INTEGER,
    exit_code INTEGER,
    tmux_session TEXT NOT NULL,
    tmux_window TEXT NOT NULL DEFAULT '0',
    tmux_pane TEXT NOT NULL DEFAULT '0',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE INDEX IF NOT EXISTS idx_sessions_tmux ON sessions(tmux_session);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at);

-- Events: state changes and metadata
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    payload TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp);

-- Output tail: last N lines of stdout/stderr per session (for dashboard live view)
CREATE TABLE IF NOT EXISTS output_tail (
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    line TEXT NOT NULL,
    ts INTEGER NOT NULL,
    PRIMARY KEY (session_id, seq),
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_output_tail_session ON output_tail(session_id);
