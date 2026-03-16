package tmux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CreateSession creates a new tmux session with the given name, running command in cwd.
// Returns tmux session name and pane id (e.g. %0) or error.
func CreateSession(sessionName, cwd, command string) (sessionID, windowID, paneID string, err error) {
	// tmux new-session -d -s name -c cwd 'command'
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", cwd, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("tmux new-session: %w: %s", err, string(out))
	}
	// Window is 0, pane is 0 by default
	return sessionName, "0", "0", nil
}

// SessionExists returns true if a tmux session with the given name exists.
func SessionExists(sessionName string) (bool, error) {
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	err := cmd.Run()
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// KillSession kills the tmux session.
func KillSession(sessionName string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-session: %w: %s", err, string(out))
	}
	_ = out
	return nil
}

// CapturePane returns the current contents of the pane (for live tail / chat output).
func CapturePane(sessionName, windowID, paneID string, lines int) (string, error) {
	target := sessionName + ":" + windowID + "." + paneID
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w: %s", err, string(out))
	}
	return string(out), nil
}

// Attach runs tmux attach in the foreground (blocking). The user detaches with C-b d.
func Attach(sessionName string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// InTmux returns true if the current process is inside a tmux session (so we can open attach in a new window).
func InTmux() bool {
	return os.Getenv("TMUX") != ""
}

// NewWindowAttach creates a new window in the current tmux session that attaches to the given agent session.
// When the user detaches (C-b d), they return to the previous window (dashboard stays open). Call only when InTmux().
func NewWindowAttach(agentSessionName, windowTitle string) error {
	if windowTitle == "" {
		windowTitle = agentSessionName
	}
	// new-window -a: insert after current; -n: name; command runs in that window
	cmd := exec.Command("tmux", "new-window", "-a", "-n", windowTitle, "tmux", "attach-session", "-t", agentSessionName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-window: %w: %s", err, string(out))
	}
	_ = out
	return nil
}

// SendKeys sends key(s) to the given session's pane (for typing into the agent terminal).
// key can be a single character or a tmux key name like "Enter", "Space", "Escape", "BackSpace".
func SendKeys(sessionName, key string) error {
	target := sessionName + ":0.0"
	cmd := exec.Command("tmux", "send-keys", "-t", target, key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, string(out))
	}
	_ = out
	return nil
}

// PasteText loads text into the tmux paste buffer and pastes it into the session's pane.
// Use this to send a block of text (e.g. from the compose editor) to the agent.
func PasteText(sessionName, text string) error {
	if text == "" {
		return nil
	}
	target := sessionName + ":0.0"
	load := exec.Command("tmux", "load-buffer", "-b", "0", "-")
	load.Stdin = bytes.NewBufferString(text)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w: %s", err, string(out))
	}
	paste := exec.Command("tmux", "paste-buffer", "-b", "0", "-t", target)
	out, err := paste.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux paste-buffer: %w: %s", err, string(out))
	}
	_ = out
	return nil
}

// ListSessions returns list of session names.
func ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	names := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(names) == 1 && names[0] == "" {
		return nil, nil
	}
	return names, nil
}

// GetPaneCwd returns the current working directory of a tmux pane.
func GetPaneCwd(sessionName, windowID, paneID string) (string, error) {
	target := sessionName + ":" + windowID + "." + paneID
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetPaneInfo returns detailed info about a tmux pane including process state.
// Returns: pid (process ID), command (current command), dead (true if pane is dead).
func GetPaneInfo(sessionName, windowID, paneID string) (pid int, command string, dead bool, err error) {
	target := sessionName + ":" + windowID + "." + paneID
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_pid}|#{pane_current_command}|#{pane_dead}")
	out, err := cmd.Output()
	if err != nil {
		return 0, "", false, fmt.Errorf("tmux display-message: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) < 3 {
		return 0, "", false, fmt.Errorf("unexpected pane info format: %s", string(out))
	}
	_, _ = fmt.Sscanf(parts[0], "%d", &pid)
	command = parts[1]
	dead = parts[2] == "1"
	return pid, command, dead, nil
}
