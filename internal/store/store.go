package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultDBPath = ".agentflt/sessions.db"

// DB wraps SQLite and provides session/event access.
type DB struct {
	conn *sql.DB
	path string
	mu   sync.Mutex
}

// Session row from sessions table.
type Session struct {
	ID            string
	Title         string
	AgentType     string // e.g. "claude", "gpt-4", "local", "custom"
	RepoPath      string
	Branch        string
	Cwd           string
	Command       string
	State         string
	StartedAt     int64
	EndedAt       sql.NullInt64
	LastOutputAt  sql.NullInt64
	ExitCode      sql.NullInt64
	TmuxSession   string
	TmuxWindow    string
	TmuxPane      string
	CreatedAt     int64
	UpdatedAt     int64
}

// Open opens or creates the SQLite DB and applies schema.
func Open(dataDir string) (*DB, error) {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, defaultDBPath)
	}
	if err := os.MkdirAll(filepath.Dir(dataDir), 0755); err != nil {
		return nil, fmt.Errorf("mkdir data dir: %w", err)
	}
	conn, err := sql.Open("sqlite", dataDir)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db := &DB{conn: conn, path: dataDir}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate() error {
	schema := mustReadSchema()
	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}
	// Idempotent column additions for existing DBs (SQLite ignores duplicate column errors).
	alterations := []string{
		`ALTER TABLE sessions ADD COLUMN agent_type TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alterations {
		// Ignore "duplicate column name" errors from ALTER TABLE on existing DBs.
		_, _ = db.conn.Exec(stmt)
	}
	return nil
}

func mustReadSchema() string {
	// Embedded in binary or same dir; for simplicity we inline minimal schema
	return `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    agent_type TEXT NOT NULL DEFAULT '',
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
-- Idempotent migration: add agent_type column if not present (for existing DBs).
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state) /* noop if exists */;
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE INDEX IF NOT EXISTS idx_sessions_tmux ON sessions(tmux_session);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    payload TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp);

CREATE TABLE IF NOT EXISTS output_tail (
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    line TEXT NOT NULL,
    ts INTEGER NOT NULL,
    PRIMARY KEY (session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_output_tail_session ON output_tail(session_id);
`
}

// Close closes the database connection.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.conn.Close()
}

// InsertSession inserts a new session.
func (db *DB) InsertSession(s Session) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(
		`INSERT INTO sessions (id, title, agent_type, repo_path, branch, cwd, command, state, started_at, ended_at, last_output_at, exit_code, tmux_session, tmux_window, tmux_pane, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.AgentType, s.RepoPath, s.Branch, s.Cwd, s.Command, s.State, s.StartedAt, nil, nil, nil,
		s.TmuxSession, s.TmuxWindow, s.TmuxPane, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

// UpdateSessionState updates state (if non-empty) and optional ended_at, exit_code, last_output_at, updated_at.
func (db *DB) UpdateSessionState(id, state string, endedAt, exitCode, lastOutputAt, updatedAt *int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if updatedAt == nil {
		t := nowUnix()
		updatedAt = &t
	}
	var err error
	if state != "" {
		_, err = db.conn.Exec(
			`UPDATE sessions SET state = ?, ended_at = COALESCE(?, ended_at), exit_code = COALESCE(?, exit_code), last_output_at = COALESCE(?, last_output_at), updated_at = ? WHERE id = ?`,
			state, endedAt, exitCode, lastOutputAt, *updatedAt, id,
		)
	} else {
		_, err = db.conn.Exec(
			`UPDATE sessions SET ended_at = COALESCE(?, ended_at), exit_code = COALESCE(?, exit_code), last_output_at = COALESCE(?, last_output_at), updated_at = ? WHERE id = ?`,
			endedAt, exitCode, lastOutputAt, *updatedAt, id,
		)
	}
	return err
}

// UpdateSessionCwd updates the cwd field based on the current working directory.
func (db *DB) UpdateSessionCwd(id, cwd string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	now := nowUnix()
	_, err := db.conn.Exec(
		`UPDATE sessions SET cwd = ?, updated_at = ? WHERE id = ?`,
		cwd, now, id,
	)
	return err
}

// ListSessions returns all sessions ordered by started_at desc.
func (db *DB) ListSessions() ([]Session, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	rows, err := db.conn.Query(
		`SELECT id, title, agent_type, repo_path, branch, cwd, command, state, started_at, ended_at, last_output_at, exit_code, tmux_session, tmux_window, tmux_pane, created_at, updated_at FROM sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var s Session
		var endedAt, lastOut, exitCode sql.NullInt64
		err := rows.Scan(&s.ID, &s.Title, &s.AgentType, &s.RepoPath, &s.Branch, &s.Cwd, &s.Command, &s.State, &s.StartedAt, &endedAt, &lastOut, &exitCode, &s.TmuxSession, &s.TmuxWindow, &s.TmuxPane, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.EndedAt = endedAt
		s.LastOutputAt = lastOut
		s.ExitCode = exitCode
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSession removes a session and its output_tail/events (close for good).
func (db *DB) DeleteSession(id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := db.conn.Exec(`DELETE FROM output_tail WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`DELETE FROM events WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err := db.conn.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// GetSession returns one session by id.
func (db *DB) GetSession(id string) (Session, bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var s Session
	var endedAt, lastOut, exitCode sql.NullInt64
	err := db.conn.QueryRow(
		`SELECT id, title, agent_type, repo_path, branch, cwd, command, state, started_at, ended_at, last_output_at, exit_code, tmux_session, tmux_window, tmux_pane, created_at, updated_at FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.Title, &s.AgentType, &s.RepoPath, &s.Branch, &s.Cwd, &s.Command, &s.State, &s.StartedAt, &endedAt, &lastOut, &exitCode, &s.TmuxSession, &s.TmuxWindow, &s.TmuxPane, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	s.EndedAt = endedAt
	s.LastOutputAt = lastOut
	s.ExitCode = exitCode
	return s, true, nil
}

// AppendOutputTail appends a line to the tail for a session (caller can trim to last N).
func (db *DB) AppendOutputTail(sessionID, line string, ts int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	seq, err := db.nextSeq(sessionID)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`INSERT INTO output_tail (session_id, seq, line, ts) VALUES (?, ?, ?, ?)`, sessionID, seq, line, ts)
	return err
}

func (db *DB) nextSeq(sessionID string) (int64, error) {
	var seq sql.NullInt64
	err := db.conn.QueryRow(`SELECT MAX(seq) FROM output_tail WHERE session_id = ?`, sessionID).Scan(&seq)
	if err != nil || !seq.Valid {
		return 1, nil
	}
	return seq.Int64 + 1, nil
}

// GetOutputTail returns the last N lines for a session (oldest first).
func (db *DB) GetOutputTail(sessionID string, n int) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	rows, err := db.conn.Query(
		`SELECT line FROM output_tail WHERE session_id = ? ORDER BY seq DESC LIMIT ?`, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	// reverse so oldest first
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines, rows.Err()
}

// ReplaceOutputTail replaces the entire tail for a session with the given lines (e.g. current pane).
func (db *DB) ReplaceOutputTail(sessionID string, lines []string, ts int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := db.conn.Exec(`DELETE FROM output_tail WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	for i, line := range lines {
		if _, err := db.conn.Exec(`INSERT INTO output_tail (session_id, seq, line, ts) VALUES (?, ?, ?, ?)`, sessionID, int64(i+1), line, ts); err != nil {
			return err
		}
	}
	return nil
}

// TrimOutputTail keeps only the last maxLines per session.
func (db *DB) TrimOutputTail(sessionID string, maxLines int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	var maxSeq sql.NullInt64
	_ = db.conn.QueryRow(`SELECT MAX(seq) FROM output_tail WHERE session_id = ?`, sessionID).Scan(&maxSeq)
	if !maxSeq.Valid {
		return nil
	}
	threshold := maxSeq.Int64 - int64(maxLines)
	if threshold <= 0 {
		return nil
	}
	_, err := db.conn.Exec(`DELETE FROM output_tail WHERE session_id = ? AND seq <= ?`, sessionID, threshold)
	return err
}

// InsertEvent inserts an event.
func (db *DB) InsertEvent(sessionID, eventType, payload string, ts int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec(`INSERT INTO events (session_id, type, timestamp, payload) VALUES (?, ?, ?, ?)`, sessionID, eventType, ts, payload)
	return err
}

// Event is a single agent timeline entry.
type Event struct {
	ID        int64
	SessionID string
	Type      string
	Payload   string
	Timestamp int64
}

// ListEvents returns the most recent limit events for a session (newest first).
func (db *DB) ListEvents(sessionID string, limit int) ([]Event, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	rows, err := db.conn.Query(
		`SELECT id, session_id, type, COALESCE(payload,''), timestamp FROM events WHERE session_id = ? ORDER BY timestamp DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Type, &e.Payload, &e.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nowUnix() int64 { return time.Now().Unix() }
