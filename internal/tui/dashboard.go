package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cabaret-pro/agentflt-public/internal/filetree"
	"github.com/cabaret-pro/agentflt-public/internal/git"
	"github.com/cabaret-pro/agentflt-public/internal/store"
	"github.com/cabaret-pro/agentflt-public/internal/supervisor"
	"github.com/cabaret-pro/agentflt-public/internal/tmux"
)

const (
	screenFleet      = "fleet"
	screenFocus      = "focus"
	screenAlerts     = "alerts"
	screenTerminals  = "terminals"
	screenSinglePane = "singlepane"
	screenEditor     = "editor"
	screenTimeline   = "timeline"
	cmdBarPrompt     = " : "
	debugLogPath     = "/tmp/agentflt-debug.log"

	// ASCII logo for agentflt (box-drawing characters)
	asciiLogoAgentflt = `██████╗  ██████╗ ███████╗███╗   ██╗████████╗███████╗██╗  ████████╗
██╔══██╗██╔════╝ ██╔════╝████╗  ██║╚══██╔══╝██╔════╝██║  ╚══██╔══╝
███████║██║  ███╗█████╗  ██╔██╗ ██║   ██║   █████╗  ██║     ██║
██╔══██║██║   ██║██╔══╝  ██║╚██╗██║   ██║   ██╔══╝  ██║     ██║
██║  ██║╚██████╔╝███████╗██║ ╚████║   ██║   ██║     ███████╗██║
╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝     ╚══════╝╚═╝
                                             agentflt · agent fleet`
)

// logDebug appends a line to the debug log for diagnosing process creation and typing.
func logDebug(format string, args ...interface{}) {
	f, err := os.OpenFile(debugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s ", time.Now().Format("15:04:05.000"))
	_, _ = fmt.Fprintf(f, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		_, _ = fmt.Fprintln(f)
	}
}

// tmuxErrForUser converts a tmux error into a short, actionable message for the UI.
// parseNewFlags extracts --repo and --cwd flag values from a raw command string,
// returning the cleaned command (with flags removed) and the flag values.
// Example: "opencode --repo /my/project" → cmd="opencode", repo="/my/project", cwd=""
func parseNewFlags(raw string) (cmd, repo, cwd string) {
	tokens := strings.Fields(raw)
	var cmdParts []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--repo" && i+1 < len(tokens):
			i++
			repo = tokens[i]
		case strings.HasPrefix(t, "--repo="):
			repo = t[len("--repo="):]
		case t == "--cwd" && i+1 < len(tokens):
			i++
			cwd = tokens[i]
		case strings.HasPrefix(t, "--cwd="):
			cwd = t[len("--cwd="):]
		default:
			cmdParts = append(cmdParts, t)
		}
	}
	cmd = strings.Join(cmdParts, " ")
	return
}

func tmuxErrForUser(prefix string, err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "no server running") {
		return prefix + "Tmux server not running. In a terminal run: mkdir -p /private/tmp/tmux-$(id -u) && tmux new -s agentflt"
	}
	if strings.Contains(s, "connection refused") || strings.Contains(s, "No such session") || strings.Contains(s, "can't find session") {
		return prefix + "Session is gone or stopped. Press r to restart this pane or Tab then: new \"Name\" cmd"
	}
	return prefix + err.Error()
}

// sessionIsActive returns true if the session can receive input (tmux session exists).
func sessionIsActive(state string) bool {
	switch state {
	case supervisor.StateRunning, supervisor.StateStalled, supervisor.StateIdle, supervisor.StateWaiting:
		return true
	default:
		return false
	}
}

// Color palette — consistent across all views.
var (
	clrAccent  = lipgloss.Color("99")  // purple — focused borders, gutter, accent
	clrGreen   = lipgloss.Color("76")  // running / success
	clrAmber   = lipgloss.Color("214") // stalled / warning
	clrRed     = lipgloss.Color("196") // failed / error
	clrCyan    = lipgloss.Color("39")  // titles / info
	clrMuted   = lipgloss.Color("242") // secondary text
	clrDimmer  = lipgloss.Color("237") // separators, very dim lines
	clrDone    = lipgloss.Color("240") // done / stopped (grey)
	clrIdle    = lipgloss.Color("244") // idle (lighter grey)
	clrText    = lipgloss.Color("252") // main text on selected row
)

var (
	styleTitle      = lipgloss.NewStyle().Bold(true).Foreground(clrCyan)
	styleDim        = lipgloss.NewStyle().Foreground(clrMuted)
	styleAlert      = lipgloss.NewStyle().Foreground(clrRed).Bold(true)
	styleStalled    = lipgloss.NewStyle().Foreground(clrAmber).Bold(true)
	styleFile       = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	styleHelp       = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Height(1)
	stylePaneBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(clrDimmer)
	stylePaneFocus  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(clrAccent)
	stylePaneHeader = lipgloss.NewStyle().Bold(true).Foreground(clrCyan).Padding(0, 1)
	styleAccent     = lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	styleGutter     = lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	// Selected row: bright text + bold, no background (cleaner feel).
	styleRowSelected = lipgloss.NewStyle().Foreground(clrText).Bold(true)

	// State badge styles.
	styleBadgeRun  = lipgloss.NewStyle().Foreground(clrGreen)
	styleBadgeWait = lipgloss.NewStyle().Foreground(clrCyan)
	styleBadgeFail = lipgloss.NewStyle().Foreground(clrRed)
	styleBadgeDone = lipgloss.NewStyle().Foreground(clrDone)
	styleBadgeIdle = lipgloss.NewStyle().Foreground(clrIdle)
)

// spinnerFrames is a braille spinner for RUNNING agents.
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// stateBadge returns a colored, fixed-width state indicator string.
func stateBadge(state string, spinFrame int) string {
	const w = 11 // "⣾ running  " — fixed width so columns stay aligned
	var s string
	switch state {
	case supervisor.StateRunning:
		sp := spinnerFrames[spinFrame%len(spinnerFrames)]
		s = styleBadgeRun.Render(sp + " running ")
	case supervisor.StateStalled:
		s = styleStalled.Render("◐ stalled ")
	case supervisor.StateIdle:
		s = styleBadgeIdle.Render("· idle    ")
	case supervisor.StateWaiting:
		s = styleBadgeWait.Render("? waiting ")
	case supervisor.StateFailed:
		s = styleBadgeFail.Render("✗ failed  ")
	case supervisor.StateDone:
		s = styleBadgeDone.Render("✓ done    ")
	case supervisor.StateStopped:
		s = styleBadgeDone.Render("■ stopped ")
	default:
		padded := state
		if len(padded) < w {
			padded += strings.Repeat(" ", w-len(padded))
		}
		s = styleBadgeIdle.Render(padded)
	}
	return s
}

// TerminalPane holds one agent's terminal state and viewport for the multi-pane view.
type TerminalPane struct {
	Session       store.Session
	Viewport      viewport.Model
	Lines         []string
	ModifiedFiles []git.ModifiedFile
	FileSelected  int
}

// Model holds dashboard state. Exported so main can read ExitAttach after Run().
type Model struct {
	DB             *store.DB
	Sup            *supervisor.Supervisor
	Screen         string
	Sessions       []store.Session
	AlertSessions  []store.Session
	Selected       int
	FocusSession        store.Session
	OutputLines         []string
	ModifiedFiles       []git.ModifiedFile
	OutputViewport      viewport.Model
	FileSelected        int
	FileSelectedPath    string // tracks selection by path so refreshes don't reset the cursor
	Width          int
	Height         int
	// ExitAttach: when set, main should run tmux attach to this session after quit
	ExitAttach string

	// Terminals view: multiple panes + global command bar
	TerminalPanes    []TerminalPane
	FocusedPaneIndex int
	CmdBarFocused    bool
	CmdInput         textinput.Model
	// PaneInputMode: when true, keypresses are sent to the focused pane's tmux (Escape to exit)
	PaneInputMode bool
	// SinglePaneIndex: when in screenSinglePane, which pane (by index in TerminalPanes) to show full-screen
	SinglePaneIndex int
	// LastError: shown in the command bar area when set (e.g. CreateSession or SendKeys failure)
	LastError string
	// lastCapturePaneAt: when we last applied a direct CapturePane (so refresh doesn't overwrite with stale DB)
	lastCapturePaneAt time.Time
	// ComposeInput: editor-style buffer in single-pane; type here then Ctrl+Enter to send to agent
	ComposeInput  textarea.Model
	ComposeFocused bool
	// FocusShowFiles toggles file list visibility in focus view.
	FocusShowFiles bool
	// FocusAutoFollow keeps focus output pinned to latest terminal line.
	FocusAutoFollow bool
	// FocusLiveBuffer is a local echo line while live typing in focus mode.
	FocusLiveBuffer string
	// Focus file preview scroll state.
	FilePreviewOffset int
	FilePreviewPath   string

	// Embedded editor state (screenEditor).
	EditorPath         string
	EditorInput        textarea.Model
	EditorDirty        bool
	EditorSaveFeedback string

	// Timeline screen state (screenTimeline).
	TimelineSession  store.Session
	TimelineEvents   []store.Event
	TimelineViewport viewport.Model

	// Focus right-panel mode: "modified" (git changed files) or "tree" (repo file tree).
	FocusRightPanel string
	// FileTreeNodes is the flat expanded list from filetree.Walk.
	FileTreeNodes    []filetree.Node
	// FileTreeSelected is the cursor row in the tree.
	FileTreeSelected int
	// FileTreeExpanded tracks which absolute dir paths are expanded.
	FileTreeExpanded map[string]bool

	// SpinFrame drives the braille spinner for RUNNING agents.
	SpinFrame int
}

type refreshMsg struct{}
type spinTickMsg struct{}

func spinTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// capturePaneMsg carries a freshly-captured pane output directly from tmux
// (bypassing the SQLite roundtrip) so typing is visible immediately.
type capturePaneMsg struct {
	paneIdx int
	lines   []string
}

// captureFocusMsg carries freshly-captured focus output from tmux.
type captureFocusMsg struct {
	lines []string
}

// captureFocusErrorMsg carries a focus capture failure.
type captureFocusErrorMsg struct {
	err error
}

// captureFocusCmd captures tmux output for the focus session immediately after input.
func captureFocusCmd(sessName string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(60 * time.Millisecond)
		output, err := tmux.CapturePane(sessName, "0", "0", 500)
		if err != nil {
			logDebug("CaptureFocus session=%s err=%v", sessName, err)
			return captureFocusErrorMsg{err: err}
		}
		raw := strings.TrimRight(output, "\n")
		if raw == "" {
			return nil
		}
		return captureFocusMsg{lines: strings.Split(raw, "\n")}
	}
}

func Run(db *store.DB, sup *supervisor.Supervisor) error {
	m := Model{
		DB:     db,
		Sup:    sup,
		Screen: screenFleet, // default: fleet dashboard
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// RunWithModel runs the dashboard and returns the final model (e.g. to run attach after quit).
func RunWithModel(db *store.DB, sup *supervisor.Supervisor) (*Model, error) {
	m := &Model{
		DB:     db,
		Sup:    sup,
		Screen: screenFleet, // default: fleet dashboard (table of all agents)
	}
	// Load sessions so first paint shows fleet
	m.Sessions, _ = db.ListSessions()
	m.AlertSessions = filterAlerts(m.Sessions)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return m, err
}

func (m *Model) Init() tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = "new \"Title\" cmd [--repo PATH] | attach | stop"
	ti.Width = 50
	ti.Prompt = cmdBarPrompt
	m.CmdInput = ti
	ta := textarea.New()
	ta.Placeholder = "Type command and press Enter..."
	ta.SetWidth(60)
	ta.SetHeight(1) // Single line for native terminal feel
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	m.ComposeInput = ta
	editorTa := textarea.New()
	editorTa.ShowLineNumbers = true
	editorTa.CharLimit = 0
	m.EditorInput = editorTa
	m.FocusRightPanel = "modified"
	m.FocusShowFiles = true
	m.FileTreeExpanded = make(map[string]bool)
	return tea.Batch(refreshCmd(), spinTick(), tea.EnterAltScreen)
}

func refreshCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return refreshMsg{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Debug logging (write to a file so it doesn't interfere with TUI)
	// Only log significant keys (not every backspace, character, etc.)
	if k, ok := msg.(tea.KeyMsg); ok {
		keyStr := k.String()
		// Only log navigation keys, special keys, and control keys
		isSignificant := len(keyStr) > 1 || // Multi-char keys like "enter", "esc", etc.
			keyStr == ":" || keyStr == "/" || keyStr == "?" || // Command triggers
			(k.Type == tea.KeyRunes && len(keyStr) == 1 && (keyStr >= "a" && keyStr <= "z" || keyStr >= "A" && keyStr <= "Z"))
		
		if isSignificant && !m.PaneInputMode && !m.ComposeFocused && !m.CmdBarFocused {
			// Only log when NOT in typing modes to avoid noise
			logDebug("key screen=%s key=%q", m.Screen, keyStr)
		}
	}

	// ── Embedded editor screen: capture all keys except Ctrl+S and Esc ──
	if m.Screen == screenEditor {
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "ctrl+s":
				if err := os.WriteFile(m.EditorPath, []byte(m.EditorInput.Value()), 0644); err != nil {
					m.EditorSaveFeedback = "save error: " + err.Error()
				} else {
					m.EditorDirty = false
					m.EditorSaveFeedback = "saved  " + m.EditorPath
				}
				return m, refreshCmd()
			case "esc":
				m.Screen = screenFocus
				m.EditorInput.Blur()
				return m, refreshCmd()
			}
		}
		// Forward all other events to the textarea; mark dirty on any change.
		prevVal := m.EditorInput.Value()
		var cmd tea.Cmd
		m.EditorInput, cmd = m.EditorInput.Update(msg)
		if m.EditorInput.Value() != prevVal {
			m.EditorDirty = true
			m.EditorSaveFeedback = ""
		}
		return m, cmd
	}

	// Editor-style compose: used in single-pane only.
	if m.Screen == screenSinglePane && m.ComposeFocused && !m.CmdBarFocused {
		if k, ok := msg.(tea.KeyMsg); ok {
			// Enter sends command (native terminal feel)
			if k.Type == tea.KeyEnter {
				text := strings.TrimSpace(m.ComposeInput.Value())
				var sessName string
				if m.Screen == screenSinglePane && len(m.TerminalPanes) > 0 && m.SinglePaneIndex < len(m.TerminalPanes) {
					pane := &m.TerminalPanes[m.SinglePaneIndex]
					if sessionIsActive(pane.Session.State) {
						sessName = pane.Session.TmuxSession
					}
				} else if m.Screen == screenFocus && m.FocusSession.ID != "" {
					if sessionIsActive(m.FocusSession.State) {
						sessName = m.FocusSession.TmuxSession
					}
				}
				if sessName != "" && text != "" {
					logDebug("Sending text to %s: %d chars", sessName, len(text))
					// Send with newline for native terminal feel
					if err := tmux.PasteText(sessName, text+"\n"); err != nil {
						m.LastError = tmuxErrForUser("", err)
						logDebug("PasteText error: %v", err)
					} else {
						m.ComposeInput.SetValue("")
						m.lastCapturePaneAt = time.Now()
						// Immediate capture for native terminal feel
						return m, func() tea.Msg {
							time.Sleep(100 * time.Millisecond)
							return refreshMsg{}
						}
					}
				} else if sessName == "" {
					m.LastError = "Session is stopped. Press r to restart."
				}
				return m, refreshCmd()
			}
			if k.String() == "esc" {
				m.ComposeFocused = false
				m.ComposeInput.Blur()
				return m, refreshCmd()
			}
		}
		var cmd tea.Cmd
		m.ComposeInput, cmd = m.ComposeInput.Update(msg)
		return m, cmd
	}

	// When typing into a pane (key-by-key), send keys to tmux; Escape exits (single-pane OR focus view)
	if (m.Screen == screenTerminals || m.Screen == screenSinglePane || m.Screen == screenFocus) && m.PaneInputMode && !m.CmdBarFocused {
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "esc" {
				m.PaneInputMode = false
				// If in single-pane mode, return to grid
				if m.Screen == screenSinglePane {
					m.Screen = screenTerminals
				}
				if m.Screen == screenFocus {
					m.FocusAutoFollow = true
				}
				return m, refreshCmd()
			}

			// Allow scrolling even in input mode with special keys
			if k.String() == "pgup" || k.String() == "pgdown" {
				var paneIdx int
				if m.Screen == screenSinglePane {
					paneIdx = m.SinglePaneIndex
				} else {
					paneIdx = m.FocusedPaneIndex
				}
				if paneIdx < len(m.TerminalPanes) {
					pane := &m.TerminalPanes[paneIdx]
					var cmd tea.Cmd
					pane.Viewport, cmd = pane.Viewport.Update(msg)
					return m, cmd
				}
				return m, nil
			}
		// In focus live typing mode, Tab toggles between modified-files and repo-tree panels.
		if m.Screen == screenFocus && k.String() == "tab" {
			if m.FocusRightPanel == "tree" {
				m.FocusRightPanel = "modified"
			} else {
				m.FocusRightPanel = "tree"
				m.loadFileTree()
			}
			m.FocusShowFiles = true
			return m, refreshCmd()
		}
			// Easier file switching while typing.
			if m.Screen == screenFocus && (k.String() == "ctrl+n" || k.String() == "ctrl+p") {
				if len(m.ModifiedFiles) > 0 {
					if k.String() == "ctrl+n" {
						m.FileSelected++
						if m.FileSelected >= len(m.ModifiedFiles) {
							m.FileSelected = 0
						}
					} else {
						m.FileSelected--
						if m.FileSelected < 0 {
							m.FileSelected = len(m.ModifiedFiles) - 1
						}
					}
					m.FileSelectedPath = m.ModifiedFiles[m.FileSelected].Abs
				}
				return m, refreshCmd()
			}

			// Local echo in focus mode so text appears immediately while typing.
			if m.Screen == screenFocus {
				s := k.String()
				switch {
				case s == "backspace":
					if len(m.FocusLiveBuffer) > 0 {
						m.FocusLiveBuffer = m.FocusLiveBuffer[:len(m.FocusLiveBuffer)-1]
					}
				case s == "enter":
					m.FocusLiveBuffer = ""
				case len(s) == 1:
					m.FocusLiveBuffer += s
				}
			}

			// Forward all other keys to tmux (single-pane or focus view)
			var sessName string
			if m.Screen == screenSinglePane && len(m.TerminalPanes) > 0 {
				paneIdx := m.SinglePaneIndex
				if paneIdx < len(m.TerminalPanes) {
					sessName = m.TerminalPanes[paneIdx].Session.TmuxSession
				}
			} else if m.Screen == screenFocus && m.FocusSession.ID != "" {
				sessName = m.FocusSession.TmuxSession
			}
			if sessName != "" {
				tmuxKey := keyToTmux(k)
				if err := tmux.SendKeys(sessName, tmuxKey); err != nil {
					logDebug("SendKeys session=%s key=%q err=%v", sessName, tmuxKey, err)
					m.LastError = tmuxErrForUser("", err)
				} else {
					// Immediate terminal update in focus mode; otherwise normal refresh.
					if m.Screen == screenFocus {
						m.FocusAutoFollow = true
						return m, captureFocusCmd(sessName)
					}
					return m, refreshCmd()
				}
				return m, refreshCmd()
			}
		}
		return m, nil
	}

	// When command bar is focused, all input goes to the text input; Enter runs command
	if (m.Screen == screenTerminals || m.Screen == screenFleet || m.Screen == screenFocus) && m.CmdBarFocused {
		var cmd tea.Cmd
		m.CmdInput, cmd = m.CmdInput.Update(msg)
		if k, ok := msg.(tea.KeyMsg); ok && k.String() == "enter" {
			m.runCommandBar(strings.TrimSpace(m.CmdInput.Value()))
			m.CmdInput.SetValue("")
			m.CmdBarFocused = false
			m.CmdInput.Blur()
			if m.ExitAttach != "" {
				return m, tea.Quit
			}
			return m, tea.Batch(cmd, refreshCmd())
		}
		if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "esc" || k.String() == "ctrl+c") {
			m.CmdBarFocused = false
			m.CmdInput.Blur()
			m.CmdInput.SetValue("")
			return m, cmd
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			if m.CmdBarFocused {
				m.CmdBarFocused = false
				m.CmdInput.Blur()
				m.CmdInput.SetValue("")
				return m, nil
			}
			if m.PaneInputMode {
				m.PaneInputMode = false
				m.FocusLiveBuffer = ""
				// If in single-pane mode, return to grid
				if m.Screen == screenSinglePane {
					m.Screen = screenTerminals
				}
				return m, refreshCmd()
			}
			if m.Screen == screenSinglePane {
				m.Screen = screenTerminals
				return m, refreshCmd()
			}
			if m.Screen == screenTerminals {
				m.Screen = screenFleet
				return m, refreshCmd()
			}
		if m.Screen == screenFocus {
			m.Screen = screenFleet
			m.FocusLiveBuffer = ""
			return m, refreshCmd()
		}
		if m.Screen == screenTimeline {
			m.Screen = screenFleet
			return m, refreshCmd()
		}
		return m, tea.Quit
	case "d":
		// Dashboard shortcut: from Fleet, go to Focus view for selected agent.
		// From other screens, go to Terminals grid view.
		if m.Screen == screenFleet {
			if len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				m.Screen = screenFocus
				m.FocusSession = m.Sessions[m.Selected]
				m.PaneInputMode = false
				m.ComposeFocused = false
				m.FocusShowFiles = true
				m.FileSelectedPath = ""
				m.FileSelected = 0
				m.FocusRightPanel = "modified"
				m.FocusAutoFollow = true
				m.FocusLiveBuffer = ""
				m.loadFocus(m.FocusSession.ID)
				return m, refreshCmd()
			}
		} else if m.Screen != screenTerminals {
			m.Screen = screenTerminals
			m.ensureTerminalsPanes()
			return m, refreshCmd()
		}
		return m, nil
		case "pgup", "pgdown":
			// Allow scrolling in single pane view
			if m.Screen == screenSinglePane && !m.CmdBarFocused && len(m.TerminalPanes) > 0 {
				var paneIdx int
				if m.Screen == screenSinglePane {
					paneIdx = m.SinglePaneIndex
				} else {
					paneIdx = m.FocusedPaneIndex
				}
				if paneIdx < len(m.TerminalPanes) {
					pane := &m.TerminalPanes[paneIdx]
					var cmd tea.Cmd
					pane.Viewport, cmd = pane.Viewport.Update(msg)
					return m, cmd
				}
			}
		if m.Screen == screenFocus {
			// Manual scroll disables auto-follow until typing resumes.
			m.FocusAutoFollow = false
			var cmd tea.Cmd
			m.OutputViewport, cmd = m.OutputViewport.Update(msg)
			return m, cmd
		}
		if m.Screen == screenTimeline {
			var cmd tea.Cmd
			m.TimelineViewport, cmd = m.TimelineViewport.Update(msg)
			return m, cmd
		}
		return m, nil
		case "j", "down", "right":
			paneIdx := m.FocusedPaneIndex
			if m.Screen == screenSinglePane {
				paneIdx = m.SinglePaneIndex
			}
			if (m.Screen == screenTerminals || m.Screen == screenSinglePane) && !m.CmdBarFocused && !m.PaneInputMode && !m.ComposeFocused && len(m.TerminalPanes) > 0 && paneIdx < len(m.TerminalPanes) {
				pane := &m.TerminalPanes[paneIdx]
				if len(pane.ModifiedFiles) > 0 {
					pane.FileSelected++
					if pane.FileSelected >= len(pane.ModifiedFiles) {
						pane.FileSelected = len(pane.ModifiedFiles) - 1
					}
					return m, nil
				}
				if m.Screen == screenTerminals {
					m.FocusedPaneIndex++
					if m.FocusedPaneIndex >= len(m.TerminalPanes) {
						m.FocusedPaneIndex = len(m.TerminalPanes) - 1
					}
				}
				return m, nil
			}
			if m.Screen == screenFleet {
				m.Selected++
				if m.Selected >= len(m.Sessions) {
					m.Selected = len(m.Sessions) - 1
				}
			}
			if m.Screen == screenAlerts {
				m.Selected++
				if m.Selected >= len(m.AlertSessions) {
					m.Selected = len(m.AlertSessions) - 1
				}
			}
		if m.Screen == screenFocus && m.FocusShowFiles {
			if m.FocusRightPanel == "tree" && len(m.FileTreeNodes) > 0 {
				m.FileTreeSelected++
				if m.FileTreeSelected >= len(m.FileTreeNodes) {
					m.FileTreeSelected = len(m.FileTreeNodes) - 1
				}
			} else if len(m.ModifiedFiles) > 0 {
				m.FileSelected++
				if m.FileSelected >= len(m.ModifiedFiles) {
					m.FileSelected = len(m.ModifiedFiles) - 1
				}
				m.FileSelectedPath = m.ModifiedFiles[m.FileSelected].Abs
			}
		}
		return m, nil
	case "k", "up", "left":
			paneIdx := m.FocusedPaneIndex
			if m.Screen == screenSinglePane {
				paneIdx = m.SinglePaneIndex
			}
			if (m.Screen == screenTerminals || m.Screen == screenSinglePane) && !m.CmdBarFocused && !m.PaneInputMode && !m.ComposeFocused && len(m.TerminalPanes) > 0 && paneIdx < len(m.TerminalPanes) {
				pane := &m.TerminalPanes[paneIdx]
				if len(pane.ModifiedFiles) > 0 {
					pane.FileSelected--
					if pane.FileSelected < 0 {
						pane.FileSelected = 0
					}
					return m, nil
				}
				if m.Screen == screenTerminals {
					m.FocusedPaneIndex--
					if m.FocusedPaneIndex < 0 {
						m.FocusedPaneIndex = 0
					}
				}
				return m, nil
			}
			if m.Screen == screenFleet {
				m.Selected--
				if m.Selected < 0 {
					m.Selected = 0
				}
			}
			if m.Screen == screenAlerts {
				m.Selected--
				if m.Selected < 0 {
					m.Selected = 0
				}
			}
		if m.Screen == screenFocus && m.FocusShowFiles {
			if m.FocusRightPanel == "tree" && len(m.FileTreeNodes) > 0 {
				m.FileTreeSelected--
				if m.FileTreeSelected < 0 {
					m.FileTreeSelected = 0
				}
			} else if len(m.ModifiedFiles) > 0 {
				m.FileSelected--
				if m.FileSelected < 0 {
					m.FileSelected = 0
				}
				m.FileSelectedPath = m.ModifiedFiles[m.FileSelected].Abs
			}
		}
		return m, nil
	case "i":
			// Enter single-pane with editor-style compose (type in buffer, Ctrl+Enter to send)
			if m.Screen == screenTerminals && !m.CmdBarFocused && !m.ComposeFocused && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				pane := &m.TerminalPanes[m.FocusedPaneIndex]
				if !sessionIsActive(pane.Session.State) {
					m.LastError = "Session is stopped. Press r to restart or Tab then: new \"Name\" cmd"
					return m, nil
				}
				m.Screen = screenSinglePane
				m.SinglePaneIndex = m.FocusedPaneIndex
				m.PaneInputMode = false
				m.ComposeFocused = true
				m.ComposeInput.Focus()
				return m, tea.Batch(refreshCmd(), textarea.Blink)
			}
			if m.Screen == screenSinglePane && !m.CmdBarFocused && !m.ComposeFocused && !m.PaneInputMode {
				pane := &m.TerminalPanes[m.SinglePaneIndex]
				if !sessionIsActive(pane.Session.State) {
					m.LastError = "Session is stopped. Press r to restart or Tab then: new \"Name\" cmd"
					return m, nil
				}
				m.ComposeFocused = true
				m.ComposeInput.Focus()
				return m, tea.Batch(refreshCmd(), textarea.Blink)
			}
			// In Focus view: start direct live typing into terminal
			if m.Screen == screenFocus && !m.CmdBarFocused && !m.ComposeFocused && !m.PaneInputMode {
				if !sessionIsActive(m.FocusSession.State) {
					m.LastError = "Session is stopped. Press r to restart."
					return m, nil
				}
				m.ComposeFocused = false
				m.ComposeInput.Blur()
				m.PaneInputMode = true
				return m, refreshCmd()
			}
			return m, nil
		case "m":
			if m.Screen == screenFocus {
				// Toggle right panel: modified files ↔ repo tree.
				if m.FocusRightPanel == "tree" {
					m.FocusRightPanel = "modified"
				} else {
					m.FocusRightPanel = "tree"
					m.loadFileTree()
				}
				m.FocusShowFiles = true
				return m, refreshCmd()
			}
			return m, nil
		case "e":
			// Open focused file in embedded editor (works in both modified and tree panels).
			if m.Screen == screenFocus {
				m.FocusShowFiles = true // ensure panel visible
				path := m.focusSelectedFilePath()
				if path != "" {
					m.openEditorScreenCmd(path) // sets m.Screen = screenEditor synchronously
					return m, refreshCmd()
				}
			}
			return m, nil
		case "[":
			if m.Screen == screenFocus {
				if m.FilePreviewOffset > 0 {
					m.FilePreviewOffset -= 5
					if m.FilePreviewOffset < 0 {
						m.FilePreviewOffset = 0
					}
				}
				return m, refreshCmd()
			}
			return m, nil
		case "]":
			if m.Screen == screenFocus {
				m.FilePreviewOffset += 5
				return m, refreshCmd()
			}
			return m, nil
		case "s":
			// In single-pane or focus: switch to key-by-key forwarding (stream keys to tmux)
			if m.Screen == screenSinglePane && !m.CmdBarFocused && len(m.TerminalPanes) > 0 && m.SinglePaneIndex < len(m.TerminalPanes) {
				pane := &m.TerminalPanes[m.SinglePaneIndex]
				if sessionIsActive(pane.Session.State) {
					m.ComposeFocused = false
					m.ComposeInput.Blur()
					m.PaneInputMode = true
					return m, refreshCmd()
				}
			}
			if m.Screen == screenFocus && !m.CmdBarFocused && m.FocusSession.ID != "" {
				if sessionIsActive(m.FocusSession.State) {
					m.ComposeFocused = false
					m.ComposeInput.Blur()
					m.PaneInputMode = true
					return m, refreshCmd()
				}
			}
			return m, nil
		case "enter":
			if m.Screen == screenTerminals && !m.CmdBarFocused && !m.PaneInputMode {
				if len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
					pane := &m.TerminalPanes[m.FocusedPaneIndex]
					// Enter on a file: open it; otherwise go full-screen and enter input mode
					if len(pane.ModifiedFiles) > 0 && pane.FileSelected < len(pane.ModifiedFiles) {
						f := pane.ModifiedFiles[pane.FileSelected]
						return m, openFileCmd(f.Abs)
					}
					if !sessionIsActive(pane.Session.State) {
						m.LastError = "Session is stopped. Press r to restart or Tab then: new \"Name\" cmd"
						return m, nil
					}
					m.Screen = screenSinglePane
					m.SinglePaneIndex = m.FocusedPaneIndex
					m.PaneInputMode = false
					m.ComposeFocused = true
					m.ComposeInput.Focus()
					return m, tea.Batch(refreshCmd(), textarea.Blink)
				}
				return m, nil
			}
			if m.Screen == screenSinglePane && !m.CmdBarFocused && !m.ComposeFocused && !m.PaneInputMode {
				pane := &m.TerminalPanes[m.SinglePaneIndex]
				if len(pane.ModifiedFiles) > 0 && pane.FileSelected < len(pane.ModifiedFiles) {
					f := pane.ModifiedFiles[pane.FileSelected]
					return m, openFileCmd(f.Abs)
				}
				if !sessionIsActive(pane.Session.State) {
					m.LastError = "Session is stopped. Press r to restart or Tab then: new \"Name\" cmd"
					return m, nil
				}
				m.ComposeFocused = true
				m.ComposeInput.Focus()
				return m, tea.Batch(refreshCmd(), textarea.Blink)
			}
		if m.Screen == screenFleet {
			if len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				m.Screen = screenFocus
				m.FocusSession = m.Sessions[m.Selected]
				m.PaneInputMode = false // Start in navigation mode, not typing mode
				m.ComposeFocused = false
				m.FocusShowFiles = true
				m.FocusRightPanel = "modified"
				m.FocusAutoFollow = true
				m.FocusLiveBuffer = ""
				m.loadFocus(m.FocusSession.ID)
				return m, refreshCmd()
			}
		}
		if m.Screen == screenAlerts {
			if len(m.AlertSessions) > 0 && m.Selected < len(m.AlertSessions) {
				m.Screen = screenFocus
				m.FocusSession = m.AlertSessions[m.Selected]
				m.PaneInputMode = false // Start in navigation mode, not typing mode
				m.ComposeFocused = false
				m.FocusShowFiles = true
				m.FocusRightPanel = "modified"
				m.FocusAutoFollow = true
				m.FocusLiveBuffer = ""
				m.loadFocus(m.FocusSession.ID)
				return m, refreshCmd()
			}
		}
			return m, nil
		case "a":
			if m.Screen == screenTerminals && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				sess := m.TerminalPanes[m.FocusedPaneIndex].Session
				if tmux.InTmux() {
					_ = tmux.NewWindowAttach(sess.TmuxSession, sess.Title)
					return m, refreshCmd()
				}
				m.ExitAttach = sess.TmuxSession
				return m, tea.Quit
			}
			if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				m.ExitAttach = m.Sessions[m.Selected].TmuxSession
				return m, tea.Quit
			}
			if m.Screen == screenFocus {
				m.ExitAttach = m.FocusSession.TmuxSession
				return m, tea.Quit
			}
			return m, nil
		case "r":
			if m.Screen == screenTerminals && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				sess := m.TerminalPanes[m.FocusedPaneIndex].Session
			_ = tmux.KillSession(sess.TmuxSession)
			_, _ = m.Sup.CreateSession(sess.Title, sess.AgentType, sess.RepoPath, sess.Cwd, sess.Command)
			return m, refreshCmd()
		}
		if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
			sess := m.Sessions[m.Selected]
			_ = tmux.KillSession(sess.TmuxSession)
			_, _ = m.Sup.CreateSession(sess.Title, sess.AgentType, sess.RepoPath, sess.Cwd, sess.Command)
			return m, refreshCmd()
		}
		if m.Screen == screenFocus && m.FocusSession.ID != "" {
			_ = tmux.KillSession(m.FocusSession.TmuxSession)
			_, _ = m.Sup.CreateSession(m.FocusSession.Title, m.FocusSession.AgentType, m.FocusSession.RepoPath, m.FocusSession.Cwd, m.FocusSession.Command)
				return m, refreshCmd()
			}
			return m, nil
		case "x":
			if m.Screen == screenTerminals && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				sess := m.TerminalPanes[m.FocusedPaneIndex].Session
				_ = tmux.KillSession(sess.TmuxSession)
				now := time.Now().Unix()
				_ = m.DB.UpdateSessionState(sess.ID, supervisor.StateStopped, &now, nil, nil, nil)
				return m, refreshCmd()
			}
			if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				sess := m.Sessions[m.Selected]
				_ = tmux.KillSession(sess.TmuxSession)
				now := time.Now().Unix()
				_ = m.DB.UpdateSessionState(sess.ID, supervisor.StateStopped, &now, nil, nil, nil)
				return m, refreshCmd()
			}
			if m.Screen == screenFocus && m.FocusSession.ID != "" {
				_ = tmux.KillSession(m.FocusSession.TmuxSession)
				now := time.Now().Unix()
				_ = m.DB.UpdateSessionState(m.FocusSession.ID, supervisor.StateStopped, &now, nil, nil, nil)
				return m, refreshCmd()
			}
			return m, nil
		case "X":
			// Close session for good (kill tmux + remove from DB) by focus/selection
			if m.Screen == screenTerminals && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				sess := m.TerminalPanes[m.FocusedPaneIndex].Session
				_ = tmux.KillSession(sess.TmuxSession)
				if err := m.DB.DeleteSession(sess.ID); err != nil {
					m.LastError = "delete: " + err.Error()
				} else {
					m.reloadSessionsAndPanes()
				}
				return m, refreshCmd()
			}
			if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				sess := m.Sessions[m.Selected]
				_ = tmux.KillSession(sess.TmuxSession)
				if err := m.DB.DeleteSession(sess.ID); err != nil {
					m.LastError = "delete: " + err.Error()
				} else {
					m.reloadSessionsAndPanes()
				}
				return m, refreshCmd()
			}
			return m, nil
		case "o":
			if m.Screen == screenFleet {
				// From fleet table, jump to terminal windows grid.
				m.Screen = screenTerminals
				m.ensureTerminalsPanes()
				if m.FocusedPaneIndex >= len(m.TerminalPanes) && len(m.TerminalPanes) > 0 {
					m.FocusedPaneIndex = 0
				}
				return m, refreshCmd()
			}
		if m.Screen == screenFocus && m.FocusShowFiles && m.FocusRightPanel == "tree" {
			if m.FileTreeSelected < len(m.FileTreeNodes) {
				node := m.FileTreeNodes[m.FileTreeSelected]
				if node.IsDir {
					if m.FileTreeExpanded[node.AbsPath] {
						delete(m.FileTreeExpanded, node.AbsPath)
					} else {
						m.FileTreeExpanded[node.AbsPath] = true
					}
					m.loadFileTree()
				} else {
					return m, m.openEditorScreenCmd(node.AbsPath)
				}
			}
			return m, refreshCmd()
		}
		if m.Screen == screenFocus && m.FocusRightPanel == "modified" && len(m.ModifiedFiles) > 0 && m.FileSelected < len(m.ModifiedFiles) {
			f := m.ModifiedFiles[m.FileSelected]
			m.openEditorScreenCmd(f.Abs)
			return m, refreshCmd()
		}
			if (m.Screen == screenTerminals || m.Screen == screenSinglePane) && !m.CmdBarFocused && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
				pane := &m.TerminalPanes[m.FocusedPaneIndex]
				if len(pane.ModifiedFiles) > 0 && pane.FileSelected < len(pane.ModifiedFiles) {
					return m, openFileCmd(pane.ModifiedFiles[pane.FileSelected].Abs)
				}
			}
			return m, nil
		case "f":
			m.Screen = screenFleet
			m.CmdBarFocused = false
			m.CmdInput.Blur()
			return m, refreshCmd()
		case "A":
			m.Screen = screenAlerts
			m.AlertSessions = filterAlerts(m.Sessions)
			m.Selected = 0
			return m, refreshCmd()
		case "L":
			// Open timeline for selected session (Fleet) or focused session (Focus).
			var sess store.Session
			if m.Screen == screenFocus {
				sess = m.FocusSession
			} else if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
				sess = m.Sessions[m.Selected]
			}
			if sess.ID != "" {
				m.TimelineSession = sess
				events, _ := m.DB.ListEvents(sess.ID, 200)
				m.TimelineEvents = events
				m.TimelineViewport = viewport.New(m.Width-2, m.Height-6)
				m.Screen = screenTimeline
			}
			return m, refreshCmd()
		case "t", "T":
			// Toggle to multi-pane terminals grid view
			m.Screen = screenTerminals
			m.ensureTerminalsPanes()
			if m.FocusedPaneIndex >= len(m.TerminalPanes) && len(m.TerminalPanes) > 0 {
				m.FocusedPaneIndex = 0
			}
			return m, refreshCmd()
	case ":", "tab":
		if msg.String() == "tab" && m.Screen == screenFocus {
			// Toggle right panel: modified files ↔ repo tree (always visible).
			if m.FocusRightPanel == "tree" {
				m.FocusRightPanel = "modified"
			} else {
				m.FocusRightPanel = "tree"
				m.loadFileTree()
			}
			m.FocusShowFiles = true
			return m, refreshCmd()
		}
			if msg.String() == ":" && (m.Screen == screenTerminals || m.Screen == screenFleet || m.Screen == screenFocus) {
				m.CmdBarFocused = true
				m.CmdInput.Focus()
				m.CmdInput.SetValue("")
				return m, textinput.Blink
			}
			if msg.String() == "tab" && (m.Screen == screenTerminals || m.Screen == screenFleet) {
				m.CmdBarFocused = true
				m.CmdInput.Focus()
				m.CmdInput.SetValue("")
				return m, textinput.Blink
			}
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.Screen == screenTerminals && !m.CmdBarFocused {
				n, _ := strconv.Atoi(msg.String())
				idx := n - 1
				if idx >= 0 && idx < len(m.TerminalPanes) {
					m.FocusedPaneIndex = idx
				}
				return m, nil
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		vp := viewport.New(msg.Width-4, min(15, msg.Height/3))
		vp.Style = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
		m.OutputViewport = vp
		if m.Screen == screenTerminals {
			m.ensureTerminalsPanes()
		}
		return m, nil
	case spinTickMsg:
		m.SpinFrame = (m.SpinFrame + 1) % len(spinnerFrames)
		return m, spinTick()

	case refreshMsg:
		sessions, _ := m.DB.ListSessions()
		m.Sessions = sessions
		m.AlertSessions = filterAlerts(m.Sessions)
		if m.Screen == screenTerminals || m.Screen == screenSinglePane {
			m.ensureTerminalsPanes()
			skipOutputUpdate := m.PaneInputMode && time.Since(m.lastCapturePaneAt) < 450*time.Millisecond
			for i := range m.TerminalPanes {
				focusedIdx := m.FocusedPaneIndex
				if m.Screen == screenSinglePane {
					focusedIdx = m.SinglePaneIndex
				}
				if !skipOutputUpdate || i != focusedIdx {
					lines, _ := m.DB.GetOutputTail(m.TerminalPanes[i].Session.ID, 500)
					m.TerminalPanes[i].Lines = lines
					content := strings.Join(lines, "\n")
					m.TerminalPanes[i].Viewport.SetContent(content)
					m.TerminalPanes[i].Viewport.GotoBottom()
				}
				m.TerminalPanes[i].ModifiedFiles, _ = m.Sup.GetModifiedFiles(m.TerminalPanes[i].Session.ID)
				if m.TerminalPanes[i].FileSelected >= len(m.TerminalPanes[i].ModifiedFiles) {
					m.TerminalPanes[i].FileSelected = 0
				}
			}
			if m.FocusedPaneIndex >= len(m.TerminalPanes) && len(m.TerminalPanes) > 0 {
				m.FocusedPaneIndex = len(m.TerminalPanes) - 1
			}
			if m.SinglePaneIndex >= len(m.TerminalPanes) && len(m.TerminalPanes) > 0 {
				m.SinglePaneIndex = len(m.TerminalPanes) - 1
			}
		}
		if m.Screen == screenFleet && m.Selected >= len(m.Sessions) && len(m.Sessions) > 0 {
			m.Selected = len(m.Sessions) - 1
		}
		if m.Screen == screenAlerts && m.Selected >= len(m.AlertSessions) && len(m.AlertSessions) > 0 {
			m.Selected = len(m.AlertSessions) - 1
		}
	if m.Screen == screenFocus && m.FocusSession.ID != "" {
		// While actively typing, prefer direct tmux captures and avoid
		// temporarily overwriting with stale DB-backed output.
		skipFocusOutputUpdate := m.PaneInputMode && time.Since(m.lastCapturePaneAt) < 450*time.Millisecond
		if !skipFocusOutputUpdate {
			m.loadFocus(m.FocusSession.ID)
		} else {
			// Even when skipping output update, always refresh modified files list
			files, _ := m.Sup.GetModifiedFiles(m.FocusSession.ID)
			m.ModifiedFiles = files
			m.syncFileSelected()
		}
	}
		if m.Screen == screenTimeline && m.TimelineSession.ID != "" {
			events, _ := m.DB.ListEvents(m.TimelineSession.ID, 200)
			m.TimelineEvents = events
		}
		// Keep both refresh and spinner ticking
		return m, tea.Batch(refreshCmd(), spinTick())
	case capturePaneMsg:
		// Direct tmux capture after a keypress — update the pane viewport
		// immediately without waiting for the supervisor's SQLite write cycle.
		if msg.paneIdx < len(m.TerminalPanes) && len(msg.lines) > 0 {
			pane := &m.TerminalPanes[msg.paneIdx]
			pane.Lines = msg.lines
			pane.Viewport.SetContent(strings.Join(msg.lines, "\n"))
			pane.Viewport.GotoBottom()
			m.lastCapturePaneAt = time.Now()
		}
		return m, nil
	case captureFocusMsg:
		if len(msg.lines) > 0 {
			m.OutputLines = msg.lines
			m.OutputViewport.SetContent(bottomAlignLines(msg.lines, m.OutputViewport.Height))
			if m.FocusAutoFollow {
				m.OutputViewport.GotoBottom()
			}
			m.lastCapturePaneAt = time.Now()
		}
		return m, nil
	case captureFocusErrorMsg:
		if msg.err != nil {
			m.LastError = tmuxErrForUser("", msg.err)
		}
		return m, nil
	}

	// Arrow keys must never go to viewport — ensure they always navigate (some terminals send different key strings)
	if k, ok := msg.(tea.KeyMsg); ok && !m.CmdBarFocused && !m.ComposeFocused && !m.PaneInputMode {
		s := k.String()
		if s == "up" || s == "left" {
			m.handleArrowUpLeft()
			return m, nil
		}
		if s == "down" || s == "right" {
			m.handleArrowDownRight()
			return m, nil
		}
	}

	// Viewport scrolling in focus view
	if m.Screen == screenFocus && m.OutputViewport.Width > 0 {
		var cmd tea.Cmd
		m.OutputViewport, cmd = m.OutputViewport.Update(msg)
		return m, cmd
	}
	// Viewport scrolling in terminals view (focused pane only)
	if m.Screen == screenTerminals && !m.CmdBarFocused && len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
		var cmd tea.Cmd
		m.TerminalPanes[m.FocusedPaneIndex].Viewport, cmd = m.TerminalPanes[m.FocusedPaneIndex].Viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleArrowUpLeft() {
	paneIdx := m.FocusedPaneIndex
	if m.Screen == screenSinglePane {
		paneIdx = m.SinglePaneIndex
	}
	if (m.Screen == screenTerminals || m.Screen == screenSinglePane) && len(m.TerminalPanes) > 0 && paneIdx < len(m.TerminalPanes) {
		pane := &m.TerminalPanes[paneIdx]
		if len(pane.ModifiedFiles) > 0 {
			pane.FileSelected--
			if pane.FileSelected < 0 {
				pane.FileSelected = 0
			}
			return
		}
		if m.Screen == screenTerminals {
			m.FocusedPaneIndex--
			if m.FocusedPaneIndex < 0 {
				m.FocusedPaneIndex = 0
			}
		}
		return
	}
	if m.Screen == screenFleet {
		m.Selected--
		if m.Selected < 0 {
			m.Selected = 0
		}
	}
	if m.Screen == screenAlerts {
		m.Selected--
		if m.Selected < 0 {
			m.Selected = 0
		}
	}
	if m.Screen == screenFocus && len(m.ModifiedFiles) > 0 {
		m.FileSelected--
		if m.FileSelected < 0 {
			m.FileSelected = 0
		}
		m.FileSelectedPath = m.ModifiedFiles[m.FileSelected].Abs
	}
}

func (m *Model) handleArrowDownRight() {
	paneIdx := m.FocusedPaneIndex
	if m.Screen == screenSinglePane {
		paneIdx = m.SinglePaneIndex
	}
	if (m.Screen == screenTerminals || m.Screen == screenSinglePane) && len(m.TerminalPanes) > 0 && paneIdx < len(m.TerminalPanes) {
		pane := &m.TerminalPanes[paneIdx]
		if len(pane.ModifiedFiles) > 0 {
			pane.FileSelected++
			if pane.FileSelected >= len(pane.ModifiedFiles) {
				pane.FileSelected = len(pane.ModifiedFiles) - 1
			}
			return
		}
		if m.Screen == screenTerminals {
			m.FocusedPaneIndex++
			if m.FocusedPaneIndex >= len(m.TerminalPanes) {
				m.FocusedPaneIndex = len(m.TerminalPanes) - 1
			}
		}
		return
	}
	if m.Screen == screenFleet {
		m.Selected++
		if m.Selected >= len(m.Sessions) {
			m.Selected = len(m.Sessions) - 1
		}
	}
	if m.Screen == screenAlerts {
		m.Selected++
		if m.Selected >= len(m.AlertSessions) {
			m.Selected = len(m.AlertSessions) - 1
		}
	}
	if m.Screen == screenFocus && len(m.ModifiedFiles) > 0 {
		m.FileSelected++
		if m.FileSelected >= len(m.ModifiedFiles) {
			m.FileSelected = len(m.ModifiedFiles) - 1
		}
		m.FileSelectedPath = m.ModifiedFiles[m.FileSelected].Abs
	}
}

// keyToTmux maps Bubble Tea key events to tmux send-keys format.
func keyToTmux(k tea.KeyMsg) string {
	s := k.String()
	switch s {
	case "enter":
		return "Enter"
	case " ":
		return "Space"
	case "esc":
		return "Escape"
	case "backspace":
		return "BackSpace"
	case "tab":
		return "Tab"
	case "up", "down", "left", "right":
		return s
	case "ctrl+c", "ctrl+C":
		return "C-c"
	case "ctrl+d":
		return "C-d"
	default:
		// Single rune (letter, number, etc.)
		if len(s) == 1 {
			return s
		}
		return s
	}
}

func openFileCmd(absPath string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "cursor"
		}
		// Try cursor first if available, else code, else editor
		if _, err := exec.LookPath("cursor"); err == nil {
			_ = exec.Command("cursor", absPath).Start()
			return nil
		}
		if _, err := exec.LookPath("code"); err == nil {
			_ = exec.Command("code", absPath).Start()
			return nil
		}
		cmd := exec.Command(editor, absPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		return nil
	}
}

// loadFileTree re-walks the session's repo and updates FileTreeNodes.
func (m *Model) loadFileTree() {
	root := m.FocusSession.RepoPath
	if root == "" {
		root = m.FocusSession.Cwd
	}
	if root == "" {
		return
	}
	if m.FileTreeExpanded == nil {
		m.FileTreeExpanded = make(map[string]bool)
	}
	nodes, err := filetree.Walk(root, m.FileTreeExpanded)
	if err != nil {
		return
	}
	m.FileTreeNodes = nodes
	if m.FileTreeSelected >= len(nodes) {
		m.FileTreeSelected = max(0, len(nodes)-1)
	}
}

// focusSelectedFilePath returns the absolute path of the currently selected file
// in whichever right panel is active (modified or tree).
func (m *Model) focusSelectedFilePath() string {
	if m.FocusRightPanel == "tree" {
		if m.FileTreeSelected < len(m.FileTreeNodes) {
			n := m.FileTreeNodes[m.FileTreeSelected]
			if !n.IsDir {
				return n.AbsPath
			}
		}
		return ""
	}
	if m.FileSelected < len(m.ModifiedFiles) {
		return m.ModifiedFiles[m.FileSelected].Abs
	}
	return ""
}

// openEditorScreenCmd is a placeholder tea.Cmd used to signal opening the editor screen.
// The actual screen switch happens synchronously before Update returns.
func (m *Model) openEditorScreenCmd(path string) tea.Cmd {
	m.Screen = screenEditor
	m.EditorPath = path
	data, err := os.ReadFile(path)
	if err == nil {
		m.EditorInput.SetValue(string(data))
	} else {
		m.EditorInput.SetValue("")
	}
	m.EditorDirty = false
	m.EditorSaveFeedback = ""
	return refreshCmd()
}

func (m *Model) loadFocus(sessionID string) {
	lines, _ := m.DB.GetOutputTail(sessionID, 500)
	m.OutputLines = lines
	files, _ := m.Sup.GetModifiedFiles(sessionID)
	m.ModifiedFiles = files
	m.syncFileSelected()
	content := strings.Join(m.OutputLines, "\n")
	m.OutputViewport.SetContent(content)
	m.OutputViewport.GotoBottom()
}

// syncFileSelected restores FileSelected to the index of FileSelectedPath in ModifiedFiles.
// If the previously selected path is no longer in the list, it clamps to a valid index.
func (m *Model) syncFileSelected() {
	if m.FileSelectedPath != "" {
		for i, f := range m.ModifiedFiles {
			if f.Abs == m.FileSelectedPath {
				m.FileSelected = i
				return
			}
		}
	}
	// Path not found (new session or file removed): keep current index if valid, else reset.
	if m.FileSelected >= len(m.ModifiedFiles) {
		m.FileSelected = 0
		if len(m.ModifiedFiles) > 0 {
			m.FileSelectedPath = m.ModifiedFiles[0].Abs
		} else {
			m.FileSelectedPath = ""
		}
	}
}

// reloadSessionsAndPanes reloads sessions from DB and rebuilds panes; clamps focus/selection indices.
// Call after deleting a session so the next keypress sees the correct list.
func (m *Model) reloadSessionsAndPanes() {
	m.Sessions, _ = m.DB.ListSessions()
	m.AlertSessions = filterAlerts(m.Sessions)
	if m.Screen == screenTerminals || m.Screen == screenSinglePane {
		m.ensureTerminalsPanes()
		if len(m.TerminalPanes) > 0 {
			if m.FocusedPaneIndex >= len(m.TerminalPanes) {
				m.FocusedPaneIndex = len(m.TerminalPanes) - 1
			}
			if m.SinglePaneIndex >= len(m.TerminalPanes) {
				m.SinglePaneIndex = len(m.TerminalPanes) - 1
			}
		}
	}
	if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected >= len(m.Sessions) {
		m.Selected = len(m.Sessions) - 1
	}
	if m.Screen == screenAlerts && len(m.AlertSessions) > 0 && m.Selected >= len(m.AlertSessions) {
		m.Selected = len(m.AlertSessions) - 1
	}
}

// ensureTerminalsPanes builds or rebuilds TerminalPanes from current Sessions (one pane per session).
func (m *Model) ensureTerminalsPanes() {
	w, h := m.Width, m.Height
	if w < 40 {
		w = 80
	}
	if h < 12 {
		h = 24
	}
	sessions := m.Sessions
	if len(sessions) == 0 {
		m.TerminalPanes = nil
		return
	}
	// Keep existing viewport state (scroll) where session ID matches; otherwise create new panes
	newPanes := make([]TerminalPane, 0, len(sessions))
	oldByID := make(map[string]TerminalPane)
	for _, p := range m.TerminalPanes {
		oldByID[p.Session.ID] = p
	}
	cols := 2
	if len(sessions) == 1 {
		cols = 1
	}
	cellW := (w - 4) / cols
	if cellW < 20 {
		cellW = w - 4
	}
	rows := (len(sessions) + cols - 1) / cols
	cellH := (h - 4 - 2) / rows // reserve 2 for cmd bar + header
	if cellH < 3 {
		cellH = 5
	}
	for i, sess := range sessions {
		pane := TerminalPane{Session: sess}
		if old, ok := oldByID[sess.ID]; ok {
			pane.Viewport = old.Viewport
			pane.FileSelected = old.FileSelected
		}
		if pane.Viewport.Width != cellW-2 || pane.Viewport.Height != cellH-2 {
			pane.Viewport = viewport.New(cellW-2, max(3, cellH-2))
			pane.Viewport.Style = stylePaneBorder
		}
		lines, _ := m.DB.GetOutputTail(sess.ID, 500)
		pane.Lines = lines
		pane.Viewport.SetContent(strings.Join(lines, "\n"))
		pane.Viewport.GotoBottom()
		pane.ModifiedFiles, _ = m.Sup.GetModifiedFiles(sess.ID)
		if pane.FileSelected >= len(pane.ModifiedFiles) {
			pane.FileSelected = 0
		}
		newPanes = append(newPanes, pane)
		_ = i
	}
	m.TerminalPanes = newPanes
	if m.FocusedPaneIndex >= len(m.TerminalPanes) {
		m.FocusedPaneIndex = max(0, len(m.TerminalPanes)-1)
	}
}

// runCommandBar runs a command typed in the global command bar (e.g. "attach", "new", "stop", "2").
func (m *Model) runCommandBar(line string) {
	m.LastError = ""
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	lower := strings.ToLower(line)

	// timeline — open agent event log for selected session
	if lower == "timeline" || lower == "tl" {
		var sess store.Session
		if m.Screen == screenFocus {
			sess = m.FocusSession
		} else if len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
			sess = m.Sessions[m.Selected]
		}
		if sess.ID != "" {
			m.TimelineSession = sess
			events, _ := m.DB.ListEvents(sess.ID, 200)
			m.TimelineEvents = events
			m.TimelineViewport = viewport.New(m.Width-2, m.Height-6)
			m.Screen = screenTimeline
		}
		return
	}

	// Aliases
	if strings.HasPrefix(lower, "add ") || strings.HasPrefix(lower, "window ") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			line = "new " + strings.TrimSpace(parts[1])
			lower = strings.ToLower(line)
		}
	}

	// new "Title" cmd [--repo PATH] [--cwd PATH]  or  new Title cmd [--repo PATH] [--cwd PATH]
	// --repo and --cwd may appear anywhere after the title.
	if strings.HasPrefix(lower, "new ") {
		rest := strings.TrimSpace(line[4:])
		var title, cmdRaw string
		if len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'') {
			quote := rest[0]
			end := strings.IndexRune(rest[1:], rune(quote))
			if end >= 0 {
				title = rest[1 : end+1]
				cmdRaw = strings.TrimSpace(rest[end+2:])
			}
		}
		if title == "" {
			parts := strings.SplitN(rest, " ", 2)
			title = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				cmdRaw = strings.TrimSpace(parts[1])
			}
		}
		if title != "" && cmdRaw != "" {
			// Parse --repo and --cwd flags out of cmdRaw, leaving the bare command.
			cmd, flagRepo, flagCwd := parseNewFlags(cmdRaw)

			// Determine repo/cwd. If the user provided explicit flags, use them.
			// Otherwise, default to "." (current directory where agentflt was launched)
			// to avoid incorrectly inheriting a different session's paths.
			repo := flagRepo
			cwd := flagCwd
			if repo == "" {
				repo = "."
			}
			if cwd == "" {
				cwd = "."
			}
			logDebug("CreateSession title=%q repo=%q cwd=%q cmd=%q", title, repo, cwd, cmd)
			_, err := m.Sup.CreateSession(title, "", repo, cwd, cmd)
			if err != nil {
				logDebug("CreateSession failed: %v", err)
				m.LastError = tmuxErrForUser("create: ", err)
			}
		}
		return
	}

	// close N  or  close "Name"  — remove session for good (kill tmux + delete from DB)
	if strings.HasPrefix(lower, "close ") {
		arg := strings.TrimSpace(line[len("close "):])
		arg = strings.Trim(arg, "\"'")
		if arg == "" {
			return
		}
		var sess *store.Session
		if m.Screen == screenFleet {
			if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(m.Sessions) {
				sess = &m.Sessions[n-1]
			} else {
				// Match by id/title (case-insensitive)
				for i := range m.Sessions {
					if strings.EqualFold(m.Sessions[i].ID, arg) || strings.EqualFold(m.Sessions[i].Title, arg) {
						sess = &m.Sessions[i]
						break
					}
				}
			}
		} else {
			if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(m.TerminalPanes) {
				sess = &m.TerminalPanes[n-1].Session
			} else {
				// Match by id/title (case-insensitive)
				for i := range m.TerminalPanes {
					if strings.EqualFold(m.TerminalPanes[i].Session.ID, arg) || strings.EqualFold(m.TerminalPanes[i].Session.Title, arg) {
						sess = &m.TerminalPanes[i].Session
						break
					}
				}
			}
		}
		if sess != nil {
			_ = tmux.KillSession(sess.TmuxSession)
			if err := m.DB.DeleteSession(sess.ID); err != nil {
				m.LastError = "close: " + err.Error()
			} else {
				m.reloadSessionsAndPanes()
			}
		} else {
			m.LastError = "no session matching: " + truncate(arg, 30)
		}
		return
	}
	// run CMD or shell CMD — send a shell command into selected session
	if strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "shell ") {
		var payload string
		if strings.HasPrefix(lower, "run ") {
			payload = strings.TrimSpace(line[len("run "):])
		} else {
			payload = strings.TrimSpace(line[len("shell "):])
		}
		if payload == "" {
			m.LastError = "run: command is empty"
			return
		}
		var target *store.Session
		if m.Screen == screenFleet && len(m.Sessions) > 0 && m.Selected < len(m.Sessions) {
			target = &m.Sessions[m.Selected]
		} else if m.Screen == screenFocus && m.FocusSession.ID != "" {
			target = &m.FocusSession
		} else if len(m.TerminalPanes) > 0 && m.FocusedPaneIndex < len(m.TerminalPanes) {
			target = &m.TerminalPanes[m.FocusedPaneIndex].Session
		}
		if target == nil {
			m.LastError = "run: no target session selected"
			return
		}
		if err := tmux.PasteText(target.TmuxSession, payload+"\n"); err != nil {
			m.LastError = tmuxErrForUser("run: ", err)
			return
		}
		return
	}

	// Switch pane by number
	if n, err := strconv.Atoi(lower); err == nil {
		if m.Screen == screenFleet && n >= 1 && n <= len(m.Sessions) {
			m.Selected = n - 1
			return
		}
		if m.Screen != screenFleet && n >= 1 && n <= len(m.TerminalPanes) {
			m.FocusedPaneIndex = n - 1
			return
		}
		return
	}
	if m.Screen == screenFleet && len(m.Sessions) == 0 {
		return
	}
	if m.Screen != screenFleet && len(m.TerminalPanes) == 0 {
		return
	}
	var sess store.Session
	if m.Screen == screenFleet {
		sess = m.Sessions[m.Selected]
	} else {
		sess = m.TerminalPanes[m.FocusedPaneIndex].Session
	}
	switch lower {
	case "attach", "a":
		if tmux.InTmux() {
			_ = tmux.NewWindowAttach(sess.TmuxSession, sess.Title)
			return
		}
		m.ExitAttach = sess.TmuxSession
		return
	case "stop", "x":
		_ = tmux.KillSession(sess.TmuxSession)
		now := time.Now().Unix()
		_ = m.DB.UpdateSessionState(sess.ID, supervisor.StateStopped, &now, nil, nil, nil)
	case "close", "c":
		_ = tmux.KillSession(sess.TmuxSession)
		if err := m.DB.DeleteSession(sess.ID); err != nil {
			m.LastError = "close: " + err.Error()
		} else {
			m.reloadSessionsAndPanes()
		}
	case "restart", "r":
		_ = tmux.KillSession(sess.TmuxSession)
		logDebug("CreateSession (restart) title=%q repo=%q cwd=%q cmd=%q", sess.Title, sess.RepoPath, sess.Cwd, sess.Command)
		_, err := m.Sup.CreateSession(sess.Title, sess.AgentType, sess.RepoPath, sess.Cwd, sess.Command)
		if err != nil {
			logDebug("CreateSession (restart) failed: %v", err)
			m.LastError = tmuxErrForUser("restart: ", err)
		}
	case "next", "n", "j":
		m.FocusedPaneIndex++
		if m.FocusedPaneIndex >= len(m.TerminalPanes) {
			m.FocusedPaneIndex = len(m.TerminalPanes) - 1
		}
	case "prev", "p", "k":
		m.FocusedPaneIndex--
		if m.FocusedPaneIndex < 0 {
			m.FocusedPaneIndex = 0
		}
	}
}

func (m *Model) viewTerminals() string {
	var b strings.Builder
	b.WriteString(m.renderBreadcrumb("grid") + styleDim.Render("  j/k navigate  Enter focus  i type  d fleet  q quit") + "\n")
	b.WriteString(m.renderSep())

	if len(m.TerminalPanes) == 0 {
		b.WriteString(styleDim.Render("No sessions. Run: agentflt new --title \"Agent\" --cmd command") + "\n")
		if m.LastError != "" {
			b.WriteString(styleAlert.Render("ERR: "+truncate(m.LastError, max(40, m.Width-8))) + "\n")
		}
		m.CmdInput.Width = min(70, max(24, m.Width-6))
		if m.CmdBarFocused {
			b.WriteString(stylePaneFocus.Render(" cmd: ") + m.CmdInput.View())
		} else {
			b.WriteString(styleHelp.Render("Tab/: command bar  |  new \"Name\" cmd  |  f fleet"))
		}
		return b.String()
	}

	// Calculate grid layout (2 columns)
	cols := 2
	if len(m.TerminalPanes) == 1 {
		cols = 1
	}
	rows := (len(m.TerminalPanes) + cols - 1) / cols

	// Overhead: global header=2 lines, footer=3 lines, per-row border=2, row-separator=1 → 3 per row
	paneWidth := (m.Width - 4) / cols
	paneHeight := (m.Height - 5 - 3*rows) / rows
	if paneHeight < 8 {
		paneHeight = 8
	}

	// Render panes in grid
	for row := 0; row < rows; row++ {
		var rowPanes []string
		for col := 0; col < cols; col++ {
			paneIdx := row*cols + col
			if paneIdx >= len(m.TerminalPanes) {
				break
			}
			pane := &m.TerminalPanes[paneIdx]
			
			// Pane header
			var paneStr strings.Builder
			title := pane.Session.Title
			if title == "" {
				title = pane.Session.ID
			}
			state := pane.Session.State
			badge := stateBadge(state, m.SpinFrame)
			header := fmt.Sprintf("[%d] %s  %s", paneIdx+1, truncate(title, paneWidth-15), badge)
			if paneIdx == m.FocusedPaneIndex {
				paneStr.WriteString(lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Padding(0, 1).Render(header) + "\n")
			} else {
				paneStr.WriteString(stylePaneHeader.Render(header) + "\n")
			}
			
			// Terminal output
			contentHeight := paneHeight - 2
			start := 0
			if len(pane.Lines) > contentHeight {
				start = len(pane.Lines) - contentHeight
			}
			outputLines := pane.Lines[start:]
			for i := 0; i < contentHeight; i++ {
				if i < len(outputLines) {
					line := truncate(outputLines[i], paneWidth-2)
					paneStr.WriteString(line + "\n")
				} else {
					paneStr.WriteString("\n")
				}
			}
			
			// Apply width and border
			paneContent := lipgloss.NewStyle().
				Width(paneWidth).
				Height(paneHeight).
				Border(lipgloss.RoundedBorder()).
				Render(paneStr.String())
			
			rowPanes = append(rowPanes, paneContent)
		}
		
		// Join panes horizontally
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rowPanes...))
		b.WriteString("\n")
	}

	// Help + command bar at bottom
	b.WriteString("\n")
	if m.LastError != "" {
		b.WriteString(styleAlert.Render("ERR: "+truncate(m.LastError, max(40, m.Width-8))) + "\n")
	}
	m.CmdInput.Width = min(70, max(24, m.Width-6))
	if m.CmdBarFocused {
		b.WriteString(styleAccent.Render(" : ") + m.CmdInput.View() + "\n")
	} else {
		b.WriteString(styleDim.Render("Enter/i interact  1-9 jump  Tab/: cmd  d fleet") + "\n")
	}
	return b.String()
}

func (m *Model) View() string {
	if m.Width == 0 {
		m.Width = 80
	}
	if m.Height == 0 {
		m.Height = 24
	}

	switch m.Screen {
	case screenAlerts:
		return m.viewAlerts()
	case screenFocus:
		return m.viewFocus()
	case screenSinglePane:
		return m.viewSinglePane()
	case screenTerminals:
		return m.viewTerminals()
	case screenEditor:
		return m.viewEditor()
	case screenTimeline:
		return m.viewTimeline()
	default:
		return m.viewFleet()
	}
}

func (m *Model) viewSinglePane() string {
	if len(m.TerminalPanes) == 0 || m.SinglePaneIndex >= len(m.TerminalPanes) {
		return styleDim.Render("No pane selected. Press Esc to return.")
	}
	pane := &m.TerminalPanes[m.SinglePaneIndex]
	var b strings.Builder
	// Header
	agentTitle := pane.Session.Title
	if agentTitle == "" {
		agentTitle = pane.Session.ID
	}
	badge := stateBadge(pane.Session.State, m.SpinFrame)
	b.WriteString(m.renderBreadcrumb("pane", agentTitle) + "  " + badge + "\n")
	b.WriteString(m.renderSep())
	if m.ComposeFocused {
		b.WriteString(styleBadgeRun.Render("● COMPOSE") + styleDim.Render("  Ctrl+Enter send  Esc blur") + "\n")
	} else if m.PaneInputMode {
		b.WriteString(styleBadgeRun.Render("● KEY-FORWARD") + styleDim.Render("  Esc exit") + "\n")
	} else {
		b.WriteString(styleDim.Render("i/Enter compose  s key-forward  o file  Esc back") + "\n")
	}
	if m.LastError != "" {
		b.WriteString(styleAlert.Render("  ✗ "+truncate(m.LastError, max(60, m.Width-8))) + "\n")
	}

	// Compose area height when shown (editor-style input at bottom)
	const composeLines = 5
	composeH := 0
	if m.ComposeFocused {
		composeH = composeLines
	}

	// Split: left = files (wider), right = terminal output (wider); reserve space for compose at bottom
	const fileColWidth = 28
	contentW := m.Width - 4
	vpWidth := contentW - fileColWidth
	if vpWidth < 20 {
		vpWidth = contentW
	}
	contentH := m.Height - 5 - composeH
	if contentH < 6 {
		contentH = 6
	}

	// Left: files
	var contentRow string
	if fileColWidth > 0 && vpWidth < contentW {
		filesLines := []string{styleTitle.Render("Modified Files")}
		if len(pane.ModifiedFiles) > 0 {
			start := max(0, min(pane.FileSelected, len(pane.ModifiedFiles)-contentH))
			end := min(len(pane.ModifiedFiles), start+contentH)
			for i := start; i < end; i++ {
				f := pane.ModifiedFiles[i]
				line := truncate(f.Path, fileColWidth-2)
				if i == pane.FileSelected {
					line = styleFile.Render("▸ " + line)
				} else {
					line = "  " + line
				}
				filesLines = append(filesLines, line)
			}
		} else {
			filesLines = append(filesLines, styleDim.Render("  (no changes)"))
		}
		filesBlock := strings.Join(filesLines, "\n")
		lines := strings.Split(filesBlock, "\n")
		if len(lines) > contentH {
			lines = lines[:contentH]
		}
		filesBlock = lipgloss.NewStyle().Width(fileColWidth).Render(strings.Join(lines, "\n"))

		// Right: terminal output - show full scrollable history
		if pane.Viewport.Width != vpWidth || pane.Viewport.Height != contentH {
			pane.Viewport.Width = vpWidth
			pane.Viewport.Height = contentH
		}
		// Load all lines and let viewport handle scrolling
		// Only auto-scroll to bottom if in input mode (typing)
		pane.Viewport.SetContent(strings.Join(pane.Lines, "\n"))
		if m.PaneInputMode {
			pane.Viewport.GotoBottom()
		}

		contentRow = lipgloss.JoinHorizontal(lipgloss.Top, filesBlock, pane.Viewport.View())
	} else {
		if pane.Viewport.Width != contentW || pane.Viewport.Height != contentH {
			pane.Viewport.Width = contentW
			pane.Viewport.Height = contentH
		}
		pane.Viewport.SetContent(strings.Join(pane.Lines, "\n"))
		if m.PaneInputMode {
			pane.Viewport.GotoBottom()
		}
		contentRow = pane.Viewport.View()
	}
	b.WriteString(contentRow)

	// Editor-style compose area at bottom (type here, Ctrl+Enter to send)
	if m.ComposeFocused {
		m.ComposeInput.SetWidth(min(contentW, m.Width-4))
		m.ComposeInput.SetHeight(composeH - 1)
		b.WriteString("\n")
		b.WriteString(styleDim.Render("  ─── compose ─── Ctrl+Enter to send ───") + "\n")
		b.WriteString(m.ComposeInput.View())
	}
	return b.String()
}

// renderBreadcrumb renders a consistent "agentflt › screen › sub" header.
func (m *Model) renderBreadcrumb(parts ...string) string {
	var b strings.Builder
	b.WriteString(styleAccent.Render("agentflt"))
	for _, p := range parts {
		b.WriteString(styleDim.Render(" › "))
		b.WriteString(styleDim.Render(p))
	}
	return b.String()
}

// renderSep renders a full-width horizontal separator line.
func (m *Model) renderSep() string {
	return lipgloss.NewStyle().Foreground(clrDimmer).Render(strings.Repeat("─", m.Width-2)) + "\n"
}

func (m *Model) viewFleet() string {
	var b strings.Builder

	// ── Header ─────────────────────────────────────────────────────────────
	if len(m.Sessions) == 0 {
		// Full logo when no agents — gives the tool a presence on first launch.
		logo := strings.TrimPrefix(asciiLogoAgentflt, "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(clrCyan).Render(logo) + "\n\n")
		b.WriteString(styleDim.Render("No agents running.") + "\n\n")
		b.WriteString(styleAccent.Render("  agentflt new --title \"Claude\" --cmd claude") + "\n")
		return b.String()
	}

	// Logo + compact breadcrumb header when agents exist.
	logo := strings.TrimPrefix(asciiLogoAgentflt, "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(clrCyan).Render(logo) + "\n")
	crumb := styleAccent.Render("agentflt") + styleDim.Render(" › fleet")
	count := styleDim.Render(fmt.Sprintf("  %d agent(s)", len(m.Sessions)))
	b.WriteString(crumb + count + "\n")
	sep := strings.Repeat("─", m.Width-2)
	b.WriteString(lipgloss.NewStyle().Foreground(clrDimmer).Render(sep) + "\n")

	// ── Column widths ───────────────────────────────────────────────────────
	const gutterW = 3  // "▌ " or "  "
	const agentW  = 22 // name [type]
	const stateW  = 12 // "⣾ running  "
	const repoW   = 20 // repo basename
	const actW    = 6  // "3s", "2m", "—"
	// task fills remaining width
	taskW := max(12, m.Width-gutterW-agentW-stateW-repoW-actW-10)

	headerLine := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s",
		agentW, "Agent", stateW, "State", repoW, "Repo", taskW, "Task", actW, "Activity")
	b.WriteString(styleTitle.Render(headerLine) + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(clrDimmer).Render(strings.Repeat("─", m.Width-2)) + "\n")

	// ── Rows ────────────────────────────────────────────────────────────────
	now := time.Now().Unix()
	for i, s := range m.Sessions {
		// Agent column = command (e.g. "claude", "aider") with [type] badge
		agent := s.Command
		if agent == "" {
			agent = s.ID
		}
		if s.AgentType != "" {
			typeBadge := lipgloss.NewStyle().Foreground(clrAccent).Render("[" + s.AgentType + "]")
			agent = truncate(agent, agentW-len(s.AgentType)-3) + " " + typeBadge
		}

	// Task column = title (e.g. "Fix auth bug", "Write tests")
	task := s.Title
	if task == "" {
		task = filepath.Base(s.RepoPath)
	}

	// Repo column = current working directory basename (shows where the agent is working)
	repo := filepath.Base(s.Cwd)
	if repo == "" || repo == "." || repo == "/" {
		repo = filepath.Base(s.RepoPath)
	}
	if repo == "" || repo == "." || repo == "/" {
		repo = "—"
	}

	activity := "—"
	if s.LastOutputAt.Valid && s.State != supervisor.StateStopped && s.State != supervisor.StateDone && s.State != supervisor.StateFailed {
		elapsed := time.Duration(now-s.LastOutputAt.Int64) * time.Second
		activity = formatDuration(elapsed)
	}

	badge := stateBadge(s.State, m.SpinFrame)

	// Gutter: accent bar for selected, blank for others.
	gutter := "   "
	if i == m.Selected {
		gutter = styleGutter.Render("▌") + " "
	}

	// Build the data portion; highlight selected row.
	data := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
		agentW, truncate(agent, agentW),
		stateW, badge,
		repoW, truncate(repo, repoW),
		taskW, truncate(task, taskW),
		actW, activity)
		if i == m.Selected {
			data = styleRowSelected.Render(data)
		} else {
			data = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(data)
		}

		b.WriteString(gutter + data + "\n")
	}

	// ── Footer ──────────────────────────────────────────────────────────────
	b.WriteString("\n")
	if m.LastError != "" {
		b.WriteString(styleAlert.Render("  ✗ "+truncate(m.LastError, max(40, m.Width-8))) + "\n")
	}
	m.CmdInput.Width = min(70, max(24, m.Width-6))
	if m.CmdBarFocused {
		b.WriteString(styleAccent.Render(" : ") + m.CmdInput.View() + "\n")
	} else {
		b.WriteString(styleDim.Render("  Enter focus  t grid  L timeline  a attach  r restart  x stop  X close  Tab/: cmd") + "\n")
	}
	return b.String()
}

func (m *Model) viewFocus() string {
	for _, s := range m.Sessions {
		if s.ID == m.FocusSession.ID {
			m.FocusSession = s
			break
		}
	}

	var b strings.Builder
	title := m.FocusSession.Title
	if title == "" {
		title = m.FocusSession.ID
	}

	// Breadcrumb header row.
	badge := stateBadge(m.FocusSession.State, m.SpinFrame)
	meta := ""
	if m.FocusSession.Branch != "" {
		meta = styleDim.Render("  " + m.FocusSession.Branch)
	}
	b.WriteString(m.renderBreadcrumb("focus", title) + "  " + badge + meta + "\n")
	b.WriteString(m.renderSep())

	// Key-hint row.
	if m.PaneInputMode {
		b.WriteString(styleBadgeRun.Render("● LIVE") + styleDim.Render("  Esc shortcuts  Tab modified↔tree  e edit  : cmd") + "\n")
	} else {
		b.WriteString(styleDim.Render("i type  Tab modified↔tree  j/k file  e/Enter edit  : cmd  d dashboard") + "\n")
	}
	if m.LastError != "" {
		b.WriteString(styleAlert.Render("  ✗ "+truncate(m.LastError, max(60, m.Width-8))) + "\n")
	}
	b.WriteString("\n")

	// Strict 50/50 split: left terminal, right files+preview (no min-width clamp to preserve half-and-half).
	totalContentW := m.Width - 6
	leftW := totalContentW / 2
	rightW := totalContentW - leftW
	contentHeight := m.Height - 9
	if contentHeight < 12 {
		contentHeight = 12
	}

	// Left panel: terminal output + always-visible live prompt line.
	termHeight := contentHeight - 1
	if termHeight < 8 {
		termHeight = 8
	}
	if m.OutputViewport.Width != leftW || m.OutputViewport.Height != termHeight {
		m.OutputViewport.Width = leftW
		m.OutputViewport.Height = termHeight
	}
	m.OutputViewport.SetContent(bottomAlignLines(m.OutputLines, termHeight))
	if m.FocusAutoFollow {
		m.OutputViewport.GotoBottom()
	}
	var left strings.Builder
	left.WriteString(styleTitle.Render("Terminal") + styleDim.Render("  pgup/pgdn scroll") + "\n")
	left.WriteString(m.OutputViewport.View() + "\n")
	prompt := "› "
	if m.FocusLiveBuffer != "" {
		prompt += m.FocusLiveBuffer
	}
	left.WriteString(stylePaneHeader.Render(truncate(prompt, max(10, leftW-2))))
	leftPanel := stylePaneBorder.Width(leftW).Height(contentHeight).Render(left.String())

	// Right panel: modified files or repo file tree, plus file preview.
	var right strings.Builder
	listHeight := min(10, max(4, contentHeight/3))
	previewHeight := contentHeight - listHeight - 4

	if m.FocusRightPanel == "tree" {
		right.WriteString(styleTitle.Render("Repo tree") + " " + styleDim.Render("j/k  Enter expand  e edit  Tab→modified") + "\n")
		if len(m.FileTreeNodes) == 0 {
			right.WriteString(styleDim.Render("  (empty)") + "\n")
		}
		// scroll window for tree
		treeStart := max(0, m.FileTreeSelected-listHeight+1)
		treeEnd := min(len(m.FileTreeNodes), treeStart+listHeight)
		for i := treeStart; i < treeEnd; i++ {
			node := m.FileTreeNodes[i]
			indent := strings.Repeat("  ", node.Depth)
			icon := "  "
			if node.IsDir {
				if m.FileTreeExpanded[node.AbsPath] {
					icon = "▾ "
				} else {
					icon = "▸ "
				}
			}
			label := indent + icon + truncate(node.Name, max(8, rightW-len(indent)-6))
			if i == m.FileTreeSelected {
				label = styleFile.Render(label)
			}
			right.WriteString(label + "\n")
		}
		right.WriteString(styleTitle.Render("Preview") + " " + styleDim.Render("[ / ] scroll") + "\n")
		path := m.focusSelectedFilePath()
		if path != "" {
			if path != m.FilePreviewPath {
				m.FilePreviewPath = path
				m.FilePreviewOffset = 0
			}
			preview, off := renderFilePreview(path, rightW-2, previewHeight, m.FilePreviewOffset)
			m.FilePreviewOffset = off
			right.WriteString(preview)
		} else {
			right.WriteString(styleDim.Render("Select a file to preview"))
		}
	} else {
		right.WriteString(styleTitle.Render("Modified files") + " " + styleDim.Render("j/k  Enter/e edit  Tab→tree") + "\n")
		if len(m.ModifiedFiles) == 0 {
			right.WriteString(styleDim.Render("  (no changes)") + "\n")
		} else {
			start := max(0, min(m.FileSelected, len(m.ModifiedFiles)-listHeight))
			end := min(len(m.ModifiedFiles), start+listHeight)
			for i := start; i < end; i++ {
				f := m.ModifiedFiles[i]
				row := fmt.Sprintf("  %s  %s", f.Status, truncate(f.Path, max(10, rightW-10)))
				if i == m.FileSelected {
					row = styleFile.Render("▸ " + row)
				}
				right.WriteString(row + "\n")
			}
		}
		right.WriteString(styleTitle.Render("Preview") + " " + styleDim.Render("[ / ] scroll") + "\n")
		if len(m.ModifiedFiles) > 0 && m.FileSelected < len(m.ModifiedFiles) {
			selected := m.ModifiedFiles[m.FileSelected].Abs
			if selected != m.FilePreviewPath {
				m.FilePreviewPath = selected
				m.FilePreviewOffset = 0
			}
			preview, off := renderFilePreview(selected, rightW-2, previewHeight, m.FilePreviewOffset)
			m.FilePreviewOffset = off
			right.WriteString(preview)
		} else {
			m.FilePreviewPath = ""
			m.FilePreviewOffset = 0
			right.WriteString(styleDim.Render("No file selected"))
		}
	}
	rightPanel := stylePaneBorder.Width(rightW).Height(contentHeight).Render(right.String())

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel))
	b.WriteString("\n" + styleDim.Render("pgup/pgdn terminal  ·  j/k file  [/] preview  e/Enter edit  Tab panel"))
	return b.String()
}

func (m *Model) viewAlerts() string {
	var b strings.Builder
	b.WriteString(m.renderBreadcrumb("alerts") + "\n")
	b.WriteString(m.renderSep())

	if len(m.AlertSessions) == 0 {
		b.WriteString(styleDim.Render("  No alerts — all agents healthy.") + "\n")
	}
	for i, s := range m.AlertSessions {
		badge := stateBadge(s.State, m.SpinFrame)
		gutter := "   "
		if i == m.Selected {
			gutter = styleGutter.Render("▌") + " "
		}
		row := fmt.Sprintf("%s  %-20s  %s", badge, truncate(s.Title, 20), truncate(s.RepoPath, 30))
		if i == m.Selected {
			row = styleRowSelected.Render(row)
		}
		b.WriteString(gutter + row + "\n")
	}
	b.WriteString("\n" + styleDim.Render("  Enter focus  f fleet  q quit"))
	return b.String()
}

func filterAlerts(sessions []store.Session) []store.Session {
	var out []store.Session
	for _, s := range sessions {
		switch s.State {
		case supervisor.StateWaiting, supervisor.StateFailed, supervisor.StateDone:
			out = append(out, s)
		case supervisor.StateIdle:
			out = append(out, s)
		}
	}
	return out
}


func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-2] + ".."
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func renderFilePreview(absPath string, width, maxLines, offset int) (string, int) {
	if maxLines < 3 {
		maxLines = 3
	}
	if width < 10 {
		width = 10
	}
	if absPath == "" {
		return styleDim.Render("(no path)"), 0
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return styleDim.Render("(unable to read file)"), 0
	}
	all := strings.Split(string(data), "\n")
	maxOffset := max(0, len(all)-maxLines)
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	end := min(len(all), offset+maxLines)
	lines := all[offset:end]
	var out []string
	for i, ln := range lines {
		out = append(out, truncate(fmt.Sprintf("%4d | %s", offset+i+1, ln), width))
	}
	if len(lines) == 0 {
		return styleDim.Render("(empty file)"), offset
	}
	return strings.Join(out, "\n"), offset
}

// viewEditor is the embedded file editor screen (Feature 5 — full implementation).
func (m *Model) viewEditor() string {
	var b strings.Builder
	filename := filepath.Base(m.EditorPath)
	dirty := ""
	if m.EditorDirty {
		dirty = "  " + styleStalled.Render("● modified")
	}
	b.WriteString(m.renderBreadcrumb("editor", filename) + dirty + "\n")
	b.WriteString(m.renderSep())
	if m.EditorSaveFeedback != "" {
		b.WriteString(styleBadgeRun.Render("  ✓ "+m.EditorSaveFeedback) + "\n")
	} else {
		b.WriteString(styleDim.Render("  Ctrl+S save  Esc back to focus") + "\n")
	}
	edH := m.Height - 4
	if edH < 4 {
		edH = 4
	}
	m.EditorInput.SetWidth(m.Width - 2)
	m.EditorInput.SetHeight(edH)
	m.EditorInput.Focus()
	b.WriteString(m.EditorInput.View())
	return b.String()
}

// viewTimeline renders the per-agent event log screen.
func (m *Model) viewTimeline() string {
	var b strings.Builder
	title := m.TimelineSession.Title
	if title == "" {
		title = m.TimelineSession.ID
	}
	meta := ""
	if m.TimelineSession.Branch != "" {
		meta = "  " + m.TimelineSession.Branch
	}
	b.WriteString(m.renderBreadcrumb("timeline", title) + styleDim.Render(meta) + "\n")
	b.WriteString(m.renderSep())
	b.WriteString(styleDim.Render("pgup/pgdn scroll  Esc/f fleet") + "\n\n")

	if len(m.TimelineEvents) == 0 {
		b.WriteString(styleDim.Render("  No events yet — events appear as agent works.") + "\n")
	} else {
		var rows strings.Builder
		for i := len(m.TimelineEvents) - 1; i >= 0; i-- {
			e := m.TimelineEvents[i]
			ts := time.Unix(e.Timestamp, 0).Format("15:04:05")
			typeLabel := fmt.Sprintf("%-14s", e.Type)
			var styled string
			switch e.Type {
			case "stalled":
				styled = styleStalled.Render(typeLabel)
			case "state_change":
				styled = styleTitle.Render(typeLabel)
			case "file_changed":
				styled = styleFile.Render(typeLabel)
			case "created":
				styled = lipgloss.NewStyle().Foreground(lipgloss.Color("79")).Render(typeLabel)
			default:
				styled = styleDim.Render(typeLabel)
			}
			payload := truncate(e.Payload, max(10, m.Width-30))
			fmt.Fprintf(&rows, "%s  %s  %s\n", styleDim.Render(ts), styled, payload)
		}
		vpH := m.Height - 6
		if vpH < 4 {
			vpH = 4
		}
		if m.TimelineViewport.Width != m.Width-2 || m.TimelineViewport.Height != vpH {
			m.TimelineViewport.Width = m.Width - 2
			m.TimelineViewport.Height = vpH
		}
		m.TimelineViewport.SetContent(rows.String())
		b.WriteString(m.TimelineViewport.View())
	}
	return b.String()
}

func bottomAlignLines(lines []string, height int) string {
	if height <= 0 || len(lines) >= height {
		return strings.Join(lines, "\n")
	}
	pad := make([]string, height-len(lines))
	return strings.Join(append(pad, lines...), "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
