# agentflt

> tmux for AI coding agents.

Run multiple coding agents in parallel. See what each is doing. Jump into any session instantly.

```
██████╗  ██████╗ ███████╗███╗   ██╗████████╗███████╗██╗  ████████╗
██╔══██╗██╔════╝ ██╔════╝████╗  ██║╚══██╔══╝██╔════╝██║  ╚══██╔══╝
███████║██║  ███╗█████╗  ██╔██╗ ██║   ██║   █████╗  ██║     ██║
██╔══██║██║   ██║██╔══╝  ██║╚██╗██║   ██║   ██╔══╝  ██║     ██║
██║  ██║╚██████╔╝███████╗██║ ╚████║   ██║   ██║     ███████╗██║
╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝     ╚══════╝╚═╝
                                             agentflt · agent fleet
```

---

## Why

Developers are starting to run 5–20 AI coding agents simultaneously. Today there is no good way to:

- See what every agent is doing at a glance
- Know which ones are stuck or stalled
- Jump into a session to unblock an agent
- Review what files each agent touched

**agentflt** is the control plane for multi-agent development. It wraps `tmux` so agents run persistently, and surfaces a live TUI dashboard so you can monitor and control your entire fleet from one terminal.

---

## Features

| Feature | Description |
|---------|-------------|
| **Fleet dashboard** | Live agent table with spinner animations, colored state badges (⣾ running, ◐ stalled, ✓ done, etc.), gutter selector, and dynamic columns |
| **State detection** | `running` / `idle` / `stalled` / `waiting_input` / `failed` / `done` / `stopped` — visual badges with icons |
| **Stall detection** | Detects no output for 30s and marks agent `stalled` (amber ◐) — catch hung agents instantly |
| **Grid view** | Multi-pane layout showing all terminal outputs side-by-side with live state badges |
| **Focus view** | Full session screen: live terminal output + modified files (git) + full repo tree + file preview |
| **Embedded editor** | Edit files directly in the TUI with syntax-aware textarea, Ctrl+S save, dirty indicators |
| **Agent timeline** | Event log per session: state changes, file modifications, stalls — observability for agent runs |
| **Persistent sessions** | Agents run in tmux sessions — survive dashboard restarts, attach from anywhere |
| **Multi-provider** | Provider-agnostic — works with Claude, GPT-4, Gemini, DeepSeek, local models, or any CLI command |
| **Human-in-the-loop** | Jump into any session with Enter, attach to tmux with `a`, restart/stop with `r`/`x` |

---

## Requirements

- **Go 1.21+**
- **tmux** (on `$PATH`)
- **git** (for branch and modified-files detection in Focus view)

---

## Install

### Quick install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cabaret-pro/agentflt/main/install.sh | bash
```

This will:
- Check for required dependencies (tmux, git, Go)
- Build and install agentflt to `~/.local/bin`
- Provide instructions if PATH needs updating

### Manual install

With Go 1.21+:

```bash
go install github.com/cabaret-pro/agentflt-public/cmd/agentflt@latest
```

Or build from source:

```bash
git clone https://github.com/cabaret-pro/agentflt-public
cd agentflt
go build -o agentflt ./cmd/agentflt
```

---

## Quickstart

```bash
# Start two agents
agentflt new --title "Fix auth bug" --type claude --repo ~/myapp --cmd "claude"
agentflt new --title "Write tests"  --type openai --repo ~/myapp --cmd "aider"

# Open the dashboard
agentflt dashboard
```

Inside the dashboard:
- Navigate with `j`/`k` or arrow keys — purple gutter `▌` marks selection
- Press `Enter` to open Focus view for full terminal + file browser
- Press `t` for Grid view to see all agents at once
- Press `L` for Timeline to see agent activity history
- Press `a` to attach directly to the tmux session

---

## CLI

| Command | Description |
|---------|-------------|
| `agentflt new --title "..." --repo PATH --cmd "..."` | Create a new agent session |
| `agentflt new --type claude --title "..." --cmd "..."` | Tag the agent provider |
| `agentflt list` | List all sessions |
| `agentflt dashboard` | Open the TUI |
| `agentflt attach <id>` | Attach directly to a tmux session |
| `agentflt restart <id>` | Restart a session |
| `agentflt stop <id>` | Stop a session |
| `agentflt logs <id>` | Print last 200 lines of stored output |

---

## Dashboard keys

### Fleet (default — `d` to return)

| Key | Action |
|-----|--------|
| `j` `k` / `↑↓` | Navigate agent list — purple gutter `▌` marks selection |
| `Enter` | Open Focus view for session |
| `t` | Grid view (all terminals side-by-side) |
| `L` | Open agent timeline (event log) |
| `a` | Attach to tmux session (exit TUI) |
| `r` / `x` | Restart / stop |
| `X` | Close for good (kill + remove from DB) |
| `:` / `Tab` | Command bar |
| `q` / `Esc` | Quit |

### Grid view (`t` from fleet)

| Key | Action |
|-----|--------|
| `j` `k` / `↑↓←→` | Navigate between panes |
| `1`–`9` | Jump to pane number |
| `Enter` / `i` | Open Focus view for selected pane |
| `d` / `Esc` | Back to fleet |

### Focus view

| Key | Action |
|-----|--------|
| `i` | Start typing (native terminal — type and Enter to send) |
| `Tab` / `m` | Toggle right panel: Modified files ↔ Repo tree |
| `j` `k` | Navigate file list |
| `Enter` | Expand/collapse directory (tree) or open file in editor |
| `e` | Open selected file in embedded editor |
| `[` `]` | Scroll file preview |
| `pgup` `pgdn` | Scroll terminal output |
| `a` | Attach to tmux session |
| `r` / `x` | Restart / stop |
| `d` / `Esc` | Back to fleet |

### Embedded editor

| Key | Action |
|-----|--------|
| `Ctrl+S` | Save file |
| `Esc` | Back to Focus view |

### Agent timeline (`L` on a session)

| Key | Action |
|-----|--------|
| `pgup` `pgdn` | Scroll events |
| `Esc` | Back to fleet |

---

## Agent states

| State | Badge | Meaning |
|-------|-------|---------|
| `running` | ⣾ (green spinner) | Active output in last 30s — healthy agent |
| `idle` | · (grey) | No output for 10s — waiting or thinking |
| `stalled` | ◐ (amber) | No output for 30s — needs attention |
| `waiting_input` | ? (cyan) | Prompt pattern detected — awaiting user input |
| `done` | ✓ (grey) | Process exited 0 — task complete |
| `failed` | ✗ (red) | Process exited non-zero — error occurred |
| `stopped` | ■ (grey) | Manually stopped by user |

---

## Data

- DB: `~/.agentflt/sessions.db` (override with `-data /path`)
- Sessions are tmux sessions named `agentflt-<id>`; attach anytime with `tmux attach -t agentflt-<id>`
- Debug log: `/tmp/agentflt-debug.log` (live: `tail -f /tmp/agentflt-debug.log`)

---

## Tests

```bash
go test ./...
```

---

## License

[MIT](LICENSE)
