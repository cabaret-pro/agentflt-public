package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cabaret-pro/agentflt-public/internal/git"
	"github.com/cabaret-pro/agentflt-public/internal/store"
	"github.com/cabaret-pro/agentflt-public/internal/tmux"
)

const (
	StateRunning = "running"
	StateStalled = "stalled"
	StateIdle    = "idle"
	StateWaiting = "waiting"
	StateStopped = "stopped"
	StateFailed  = "failed"
	StateDone    = "done"

	stallThreshold  = 30 * time.Second
	idleThreshold   = 2 * time.Minute
	waitingPatterns = "continue?,y/n,approve,press enter,? (y/n),(y/n),[y/n],waiting for,paused,suspended"
	failurePatterns = "error:,exception:,fatal:,failed:,traceback,segmentation fault,panic:"
	maxTailLines    = 500
)

// Supervisor tracks sessions and updates state from tmux/output.
type Supervisor struct {
	db           *store.DB
	stop         chan struct{}
	interval     time.Duration
	// prevModFiles stores the last-seen set of modified file paths per session (for debounced file-change events).
	prevModFiles map[string]map[string]bool
	// prevState stores the last-seen state per session (for state-change events).
	prevState map[string]string
	// prevOutputSig stores the last-seen output signature (last non-empty line + line count)
	// so we only update last_output_at when the pane content actually changes.
	prevOutputSig map[string]string
}

// New creates a supervisor that polls tmux and updates DB state.
func New(db *store.DB) *Supervisor {
	return &Supervisor{
		db:            db,
		stop:          make(chan struct{}),
		interval:      500 * time.Millisecond,
		prevModFiles:  make(map[string]map[string]bool),
		prevState:     make(map[string]string),
		prevOutputSig: make(map[string]string),
	}
}

// Start begins the background poll loop.
func (s *Supervisor) Start() {
	go s.loop()
}

// Stop stops the background loop.
func (s *Supervisor) Stop() {
	close(s.stop)
}

func (s *Supervisor) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Supervisor) tick() {
	sessions, err := s.db.ListSessions()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for _, sess := range sessions {
		if sess.State == StateStopped || sess.State == StateFailed || sess.State == StateDone {
			continue
		}
		ok, _ := tmux.SessionExists(sess.TmuxSession)
		if !ok {
			// tmux session gone => stopped (or failed if we had exit code)
			exitCode := int64(1)
			if sess.ExitCode.Valid {
				exitCode = sess.ExitCode.Int64
			}
			var state string
			if exitCode != 0 {
				state = StateFailed
			} else {
				state = StateDone
			}
			_ = s.db.UpdateSessionState(sess.ID, state, &now, &exitCode, nil, nil)
			continue
		}

		// Check if pane is dead or process has exited
		pid, _, dead, err := tmux.GetPaneInfo(sess.TmuxSession, sess.TmuxWindow, sess.TmuxPane)
		if err == nil && dead {
			// Pane is marked dead by tmux
			exitCode := int64(1)
			if sess.ExitCode.Valid {
				exitCode = sess.ExitCode.Int64
			}
			var state string
			if exitCode != 0 {
				state = StateFailed
			} else {
				state = StateDone
			}
			_ = s.db.UpdateSessionState(sess.ID, state, &now, &exitCode, nil, nil)
			continue
		}
		// If we have a PID, check if the process is still running
		if err == nil && pid > 0 {
			// Check process state via ps
			if procState := checkProcessState(pid); procState != "" {
				if procState == "stopped" {
					_ = s.db.UpdateSessionState(sess.ID, StateStopped, nil, nil, nil, nil)
					continue
				} else if procState == "zombie" || procState == "dead" {
					exitCode := int64(1)
					_ = s.db.UpdateSessionState(sess.ID, StateFailed, &now, &exitCode, nil, nil)
					continue
				}
			}
		}

		// Capture current pane output (chat/CLI) and replace stored tail; update last_output_at
		out, err := tmux.CapturePane(sess.TmuxSession, sess.TmuxWindow, sess.TmuxPane, 100)
		if err != nil {
			continue
		}
		rawLines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		var lines []string
		var lastLine string
		for _, line := range rawLines {
			line = strings.TrimRight(line, " \t")
			lines = append(lines, line)
			if line != "" {
				lastLine = line
			}
		}
		if len(lines) > maxTailLines {
			lines = lines[len(lines)-maxTailLines:]
		}
		_ = s.db.ReplaceOutputTail(sess.ID, lines, now)
		// Only update last_output_at when the pane content has actually changed,
		// so the Activity timer and stall/idle detection are meaningful.
		sig := fmt.Sprintf("%d|%s", len(lines), lastLine)
		if sig != s.prevOutputSig[sess.ID] {
			s.prevOutputSig[sess.ID] = sig
			_ = s.db.UpdateSessionState(sess.ID, "", nil, nil, &now, nil)
		}

		// Update current working directory from tmux pane
		if cwd, err := tmux.GetPaneCwd(sess.TmuxSession, sess.TmuxWindow, sess.TmuxPane); err == nil && cwd != "" {
			_ = s.db.UpdateSessionCwd(sess.ID, cwd)
		}

		// Infer state: check for failures, waiting prompts, stalled (30s), idle (2m), else running
		state := StateRunning
		failed := false
		if lastLine != "" {
			lower := strings.ToLower(lastLine)
			// Check for failure patterns first
			for _, p := range strings.Split(failurePatterns, ",") {
				if strings.Contains(lower, strings.TrimSpace(p)) {
					exitCode := int64(1)
					_ = s.db.UpdateSessionState(sess.ID, StateFailed, &now, &exitCode, nil, nil)
					failed = true
					break
				}
			}
			// Check for waiting patterns if not failed
			if !failed {
				for _, p := range strings.Split(waitingPatterns, ",") {
					if strings.Contains(lower, strings.TrimSpace(p)) {
						state = StateWaiting
						break
					}
				}
			}
		}
		// Skip further state updates if we detected a failure
		if failed {
			continue
		}
		if sess.LastOutputAt.Valid && state == StateRunning {
			elapsed := time.Duration(now-sess.LastOutputAt.Int64) * time.Second
			if elapsed >= idleThreshold {
				state = StateIdle
			} else if elapsed >= stallThreshold {
				state = StateStalled
			}
		}
		_ = s.db.UpdateSessionState(sess.ID, state, nil, nil, nil, nil)

		// Emit state_change event when state transitions.
		if prev, ok := s.prevState[sess.ID]; ok && prev != state {
			_ = s.db.InsertEvent(sess.ID, "state_change", prev+"→"+state, now)
			if state == StateStalled {
				_ = s.db.InsertEvent(sess.ID, "stalled", "no output for 30s", now)
			}
		}
		s.prevState[sess.ID] = state

		// Emit file_changed events when the modified-files set changes (debounced per tick).
		s.emitFileChangeEvents(sess.ID, sess.RepoPath, sess.Cwd, now)
	}
}

// emitFileChangeEvents compares current git-modified files to last known set and inserts events.
func (s *Supervisor) emitFileChangeEvents(sessionID, repoPath, cwd string, now int64) {
	root := repoPath
	if root == "" {
		root = cwd
	}
	if root == "" {
		return
	}
	files, err := git.ModifiedFiles(root, time.Time{})
	if err != nil {
		return
	}
	current := make(map[string]bool, len(files))
	for _, f := range files {
		current[f.Path] = true
	}
	prev := s.prevModFiles[sessionID]
	if prev == nil {
		s.prevModFiles[sessionID] = current
		return
	}
	for path := range current {
		if !prev[path] {
			_ = s.db.InsertEvent(sessionID, "file_changed", path, now)
		}
	}
	s.prevModFiles[sessionID] = current
}

// CreateSession creates a new agent session (tmux + DB row).
func (s *Supervisor) CreateSession(title, agentType, repoPath, cwd, command string) (store.Session, error) {
	if cwd == "" {
		cwd = repoPath
	}
	if cwd == "" {
		cwd = "."
	}
	branch := ""
	if repoPath != "" {
		branch, _ = git.Branch(repoPath)
	}
	if branch == "" {
		branch = "main"
	}
	sessionID := fmt.Sprintf("agent-%d", time.Now().UnixNano()/1e6)
	tmuxName := "agentflt-" + sessionID
	_, _, _, err := tmux.CreateSession(tmuxName, cwd, command)
	if err != nil {
		return store.Session{}, err
	}
	now := time.Now().Unix()
	sess := store.Session{
		ID:          sessionID,
		Title:       title,
		AgentType:   agentType,
		RepoPath:    repoPath,
		Branch:      branch,
		Cwd:         cwd,
		Command:     command,
		State:       StateRunning,
		StartedAt:   now,
		TmuxSession: tmuxName,
		TmuxWindow:  "0",
		TmuxPane:    "0",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.InsertSession(sess); err != nil {
		_ = tmux.KillSession(tmuxName)
		return store.Session{}, err
	}
	_ = s.db.InsertEvent(sessionID, "created", command, now)
	s.prevState[sessionID] = StateRunning
	return sess, nil
}

// GetModifiedFiles returns git modified files for the session's current working directory.
func (s *Supervisor) GetModifiedFiles(sessionID string) ([]git.ModifiedFile, error) {
	sess, ok, err := s.db.GetSession(sessionID)
	if err != nil || !ok {
		return nil, err
	}
	// Resolve the best path to inspect for modified files.
	// Priority:
	//   1. repo_path if it's absolute AND the directory actually exists on disk
	//   2. cwd (updated dynamically every tick from the tmux pane)
	//   3. repo_path as a fallback (relative or unverified)
	path := ""
	if filepath.IsAbs(sess.RepoPath) {
		if info, serr := os.Stat(sess.RepoPath); serr == nil && info.IsDir() {
			path = sess.RepoPath
		}
	}
	if path == "" && sess.Cwd != "" {
		path = sess.Cwd
	}
	if path == "" && sess.RepoPath != "" && sess.RepoPath != "." {
		path = sess.RepoPath
	}
	if path == "" {
		return nil, nil
	}
	// Use session start time as the cutoff so we only show files the agent itself touched.
	startedAt := time.Unix(sess.StartedAt, 0)
	return git.ModifiedFiles(path, startedAt)
}

// checkProcessState returns the state of a process: "running", "stopped", "zombie", "dead", or empty string if unknown.
func checkProcessState(pid int) string {
	// Use ps to check process state on macOS/Linux
	cmd := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid))
	out, err := cmd.Output()
	if err != nil {
		// Process doesn't exist or ps failed
		return "dead"
	}
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "dead"
	}
	// Common process state codes:
	// R = running, S = sleeping (interruptible), D = sleeping (uninterruptible)
	// T = stopped (on a signal), Z = zombie, X = dead
	switch state[0] {
	case 'T':
		return "stopped"
	case 'Z', 'X':
		return "zombie"
	case 'R', 'S', 'D', 'I':
		return "running"
	default:
		return ""
	}
}
