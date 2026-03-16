package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if db.path != dbPath {
		t.Errorf("path = %q want %q", db.path, dbPath)
	}
}

func TestInsertAndGetSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	s := Session{
		ID:          "s1",
		Title:       "Test task",
		RepoPath:    "/repo",
		Branch:      "main",
		Cwd:         "/repo",
		Command:     "echo hi",
		State:       "running",
		StartedAt:   now,
		TmuxSession: "tmux-s1",
		TmuxWindow:  "0",
		TmuxPane:    "0",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.InsertSession(s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	got, ok, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !ok {
		t.Fatal("GetSession: not found")
	}
	if got.Title != "Test task" || got.RepoPath != "/repo" || got.State != "running" {
		t.Errorf("got %+v", got)
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	for i, id := range []string{"a", "b", "c"} {
		s := Session{
			ID: id, Title: "T" + id, RepoPath: "/r", Branch: "main", Cwd: "/r", Command: "cmd",
			State: "running", StartedAt: now + int64(i), TmuxSession: "tmux-" + id,
			TmuxWindow: "0", TmuxPane: "0", CreatedAt: now, UpdatedAt: now,
		}
		if err := db.InsertSession(s); err != nil {
			t.Fatalf("InsertSession: %v", err)
		}
	}

	list, err := db.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("len(list) = %d want 3", len(list))
	}
	// Order: started_at DESC, so c, b, a
	if list[0].ID != "c" || list[1].ID != "b" || list[2].ID != "a" {
		t.Errorf("order: %v", list)
	}
}

func TestUpdateSessionState(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	s := Session{
		ID: "u1", Title: "U", RepoPath: "/r", Branch: "main", Cwd: "/r", Command: "cmd",
		State: "running", StartedAt: now, TmuxSession: "tmux-u1", TmuxWindow: "0", TmuxPane: "0",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertSession(s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	ended := now + 60
	exitCode := int64(1)
	if err := db.UpdateSessionState("u1", "failed", &ended, &exitCode, nil, nil); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	got, ok, _ := db.GetSession("u1")
	if !ok {
		t.Fatal("session not found")
	}
	if got.State != "failed" {
		t.Errorf("state = %q want failed", got.State)
	}
	if !got.ExitCode.Valid || got.ExitCode.Int64 != 1 {
		t.Errorf("exit_code = %v", got.ExitCode)
	}
	if !got.EndedAt.Valid || got.EndedAt.Int64 != ended {
		t.Errorf("ended_at = %v", got.EndedAt)
	}
}

func TestOutputTail(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	if err := db.AppendOutputTail("sid1", "line1", now); err != nil {
		t.Fatalf("AppendOutputTail: %v", err)
	}
	if err := db.AppendOutputTail("sid1", "line2", now+1); err != nil {
		t.Fatalf("AppendOutputTail: %v", err)
	}

	lines, err := db.GetOutputTail("sid1", 10)
	if err != nil {
		t.Fatalf("GetOutputTail: %v", err)
	}
	if len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("GetOutputTail: %q", lines)
	}

	// Trim to 1 line
	if err := db.TrimOutputTail("sid1", 1); err != nil {
		t.Fatalf("TrimOutputTail: %v", err)
	}
	lines, _ = db.GetOutputTail("sid1", 10)
	if len(lines) != 1 || lines[0] != "line2" {
		t.Errorf("after trim: %q", lines)
	}
}

func TestOpenEmptyDataDirUsesHome(t *testing.T) {
	// Open("") should not fail and should use home dir path
	db, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, defaultDBPath)
	if db.path != want {
		t.Errorf("path = %q want %q", db.path, want)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, ok, err := db.GetSession("nonexistent")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if ok {
		t.Error("expected not found")
	}
}

func TestReplaceOutputTail(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	if err := db.ReplaceOutputTail("r1", []string{"a", "b", "c"}, now); err != nil {
		t.Fatalf("ReplaceOutputTail: %v", err)
	}
	lines, err := db.GetOutputTail("r1", 10)
	if err != nil {
		t.Fatalf("GetOutputTail: %v", err)
	}
	if len(lines) != 3 || lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Errorf("GetOutputTail: %q", lines)
	}
	// Replace with new content
	if err := db.ReplaceOutputTail("r1", []string{"x", "y"}, now+1); err != nil {
		t.Fatalf("ReplaceOutputTail: %v", err)
	}
	lines, _ = db.GetOutputTail("r1", 10)
	if len(lines) != 2 || lines[0] != "x" || lines[1] != "y" {
		t.Errorf("after replace: %q", lines)
	}
}

func TestUpdateSessionStateEmptyState(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().Unix()
	s := Session{
		ID: "e1", Title: "E", RepoPath: "/r", Branch: "main", Cwd: "/r", Command: "cmd",
		State: "running", StartedAt: now, TmuxSession: "tmux-e1", TmuxWindow: "0", TmuxPane: "0",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertSession(s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	lastOut := now + 10
	if err := db.UpdateSessionState("e1", "", nil, nil, &lastOut, nil); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	got, ok, _ := db.GetSession("e1")
	if !ok {
		t.Fatal("not found")
	}
	if got.State != "running" {
		t.Errorf("state should be unchanged: %q", got.State)
	}
	if !got.LastOutputAt.Valid || got.LastOutputAt.Int64 != lastOut {
		t.Errorf("last_output_at = %v", got.LastOutputAt)
	}
}

func TestDeleteSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	now := int64(12345)
	s := Session{
		ID: "del1", Title: "ToDelete", RepoPath: ".", Branch: "main", Cwd: ".", Command: "echo",
		State: "running", StartedAt: now, TmuxSession: "agentctl-del1", TmuxWindow: "0", TmuxPane: "0",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertSession(s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	if err := db.AppendOutputTail("del1", "line1", now); err != nil {
		t.Fatalf("AppendOutputTail: %v", err)
	}
	if err := db.DeleteSession("del1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	_, ok, err := db.GetSession("del1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if ok {
		t.Fatal("session should be gone after DeleteSession")
	}
	lines, err := db.GetOutputTail("del1", 10)
	if err != nil {
		t.Fatalf("GetOutputTail: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("output_tail should be empty after delete: got %d lines", len(lines))
	}
}
