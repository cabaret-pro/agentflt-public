# agentflt — Project Roadmap

> Last updated: March 2026

---

## Vision

**agentflt** is the control plane for multi-agent development. It wraps tmux so agents run persistently and surfaces a live TUI dashboard to monitor and control your entire fleet from one terminal.

---

## Phases

### Phase 1: Foundation (Done ✓)

- [x] Core TUI dashboard with fleet view
- [x] Agent state detection (RUNNING, IDLE, STALLED, WAITING_INPUT, FAILED, DONE, STOPPED)
- [x] Stall detection (30s threshold)
- [x] Grid view (all terminal outputs)
- [x] Focus view (single session with live output + repo file tree)
- [x] Embedded file editor in TUI
- [x] Agent timeline (event log)
- [x] Persistent sessions via tmux
- [x] Multi-provider support (CLI-agnostic)
- [x] Human-in-the-loop (attach, send input, restart, stop)

---

### Phase 2: Polish & Reliability (Current)

#### TUI Improvements

- [x] Visual indicators for agent activity (braille spinner ⣾, state badges)
- [x] Better keyboard navigation (1-9 pane jumping in grid view)
- [x] Improved color palette (purple accent, semantic state colors)
- [x] Gutter selector (▌) for current row
- [x] Consistent breadcrumb headers across all views
- [ ] Command bar with fuzzy search for commands
- [ ] Improved error messages in UI (actionable, not just "error")
- [ ] Smooth scrolling in terminal output views

#### Stability

- [ ] Handle tmux server restarts gracefully
- [ ] Recovery from database corruption
- [ ] Race condition fixes in output capture
- [ ] Better handling of session death mid-operation

---

### Phase 3: Observability

- [ ] Agent timeline improvements:
  - [ ] Filterable by event type
  - [ ] Export to JSON/CSV
  - [ ] Search within timeline
- [ ] Fleet-wide metrics view:
  - [ ] Total runtime per agent
  - [ ] File edit counts
  - [ ] Success/failure rates
- [ ] Alerts view (sessions needing attention)
- [ ] Webhook notifications for state changes

---

### Phase 4: Developer Experience

- [ ] Configuration file (`~/.agentflt/config.yaml`)
  - [ ] Custom keybindings
  - [ ] Default commands
  - [ ] Theme customization
- [ ] Session templates (preset agent configs)
- [ ] Bulk operations:
  - [ ] Start multiple agents from config
  - [ ] Stop/restart all
- [ ] Session groups/folders
- [ ] Save and restore fleet state

---

### Phase 5: Integrations

- [ ] Claude Code integration (native, not just CLI)
- [ ] GitHub integration:
  - [ ] Auto-create PR summary
  - [ ] Branch status in fleet view
- [ ] Claude Code / Cursor integration for file editing
- [ ] Logging integrations (export to Loki, Datadog)
- [ ] SSH mode (connect to remote tmux)

---

### Phase 6: Scaling

- [ ] Support 50+ simultaneous agents
- [ ] Virtualized grid view for large fleets
- [ ] Dashboard performance optimization
- [ ] Resource monitoring (CPU/memory per agent)

---

## Backlog (Unplanned)

- Web UI or GUI
- Non-CLI runtimes
- Built-in chat UI (beyond terminal output)
- Cloud deployment
- Multi-user support

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
