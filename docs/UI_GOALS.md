# agentctl — UI design goals

This document states the **design goals** for the TUI dashboard so we stay aligned on intent and priorities. It describes *what* we're aiming for, not implementation details.

---

## Purpose

- **Single place to run and monitor multiple CLI agents** (e.g. Claude, Gemini) from the terminal.
- **Terminal-native**: keyboard-first, no mouse required; works over SSH and in any terminal.
- **Live visibility**: see each agent’s output and git-modified files without leaving the dashboard; optionally **type into** a session (e.g. to answer prompts) and see typing in the pane.
- **Clear errors**: when something fails (tmux, create session, send-keys), show a short, actionable message in the UI and log details for debugging.

---

## Principles

1. **One dashboard, many agents** — All agents appear in one TUI. Switch between panes/views with keys (1–9, j/k, f, t, A, etc.), not by restarting the app.
2. **Fleet → focus → action** — You can see the fleet at a glance, drill into one session for live output + files, then act (attach, restart, stop, type, open file).
3. **Terminals first** — Default view is the **terminals grid**: each agent is a pane with **files (left)** and **terminal output (right)**. This is the primary working view.
4. **Command bar for power users** — Tab / `:` opens a command bar for `new "Title" cmd`, `attach`, `stop`, `restart`, pane switching. Errors from create/restart/send-keys show near the bar.
5. **No silent failures** — Creation, restart, and “type into pane” failures surface in the UI (ERR line) and in the debug log so we can see why (e.g. tmux not running, session gone).
6. **Familiar key semantics** — Vim-style j/k where it makes sense; Esc to exit modes; q to go back/quit. Attach via `a` or command bar so “Enter” doesn’t quit the app.

---

## View goals

| View | Goal |
|------|------|
| **Terminals** | Default. Grid of agent panes. Each pane: **left** = git-modified files (j/k, Enter/o to open), **right** = live terminal output. Focus with 1–9 or j/k; **i** or **Enter** (no file selected) = full-screen pane + **editor-style compose** (type in buffer, Ctrl+Enter to send). Esc back to grid. Command bar at bottom. |
| **Single-pane** | Full-screen on one agent: **output + modified files** on top; **compose editor** at bottom (type there, **Ctrl+Enter** to send to agent). **s** = key-by-key forwarding. Esc returns to grid. No typing into stopped sessions — restart with **r** or create new. |
| **Fleet** | One row per agent: status, title, repo, branch, runtime, last activity. Navigate j/k, Enter to open Focus view for that session. |
| **Focus** | One session: live output (scrollable) + modified files; open file in editor with **o**. Attach / restart / stop from here. |
| **Alerts** | Filtered list of sessions that need attention: waiting for input, failed, idle too long, or done. Enter to focus. |

---

## Interaction goals

- **Create agents**: CLI `agentctl new ...` or dashboard command bar: `new "Title" cmd`. Same repo/cwd as focused pane when possible.
- **Type into a pane**: Focus pane → **i** or **Enter** (no file selected) → full-screen with **editor-style compose**: type in the buffer at the bottom, **Ctrl+Enter** to send block to agent (tmux paste). **s** = switch to key-by-key forwarding; Esc to stop. Stopped sessions cannot receive input — use **r** or create new agent.
- **Attach**: **a** or command bar `attach`. If already in tmux → new window; otherwise quit dashboard and run `tmux attach`.
- **Open file**: In terminals or focus, select file with j/k, then **Enter** or **o** to open in Cursor / VS Code / `$EDITOR`.
- **Restart / stop**: **r** / **x** (or command bar). Errors (e.g. tmux not running) shown in UI and log.
- **Navigate**: 1–9 panes; j/k in lists and file lists; f = fleet, t = terminals, A = alerts; q / Esc = back or quit.

---

## Error and feedback goals

- **Tmux / process errors**: Shown in the UI as a single ERR line (actionable when possible, e.g. “Tmux server not running. In a terminal run: …”). Full error and context in `/tmp/agentctl-debug.log`.
- **Create / restart**: On failure, show message near command bar; don’t leave the user guessing.
- **Send-keys / capture-pane**: On failure (e.g. no server, session gone), show message and log so “typing not working” is diagnosable.

---

## Out of scope (for alignment)

- Web UI or GUI.
- Managing non-CLI or non-tmux runtimes.
- Built-in chat UI (we show the agent’s terminal output; typing is “keys to the pane,” not a custom chat widget).

If you want to change or add goals, update this file and we can align the implementation to it.
