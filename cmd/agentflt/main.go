package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cabaret-pro/agentflt-public/internal/store"
	"github.com/cabaret-pro/agentflt-public/internal/supervisor"
	"github.com/cabaret-pro/agentflt-public/internal/tmux"
	"github.com/cabaret-pro/agentflt-public/internal/tui"
)

func main() {
	dataDir := flag.String("data", "", "Data directory for DB (default: ~/.agentflt)")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}
	cmd := args[0]
	rest := args[1:]

	dbPath := *dataDir
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".agentflt", "sessions.db")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	sup := supervisor.New(db)
	sup.Start()
	defer sup.Stop()

	switch cmd {
	case "new":
		runNew(db, sup, rest)
	case "list":
		runList(db, rest)
	case "dashboard":
		runDashboard(db, sup)
	case "attach":
		runAttach(db, rest)
	case "restart":
		runRestart(db, sup, rest)
	case "stop":
		runStop(db, rest)
	case "logs":
		runLogs(db, rest)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: agentflt <command> [options]

Commands:
  new --title "Task" [--repo PATH] [--cwd PATH] --cmd "command"
  list
  dashboard
  attach <id>
  restart <id>
  stop <id>
  logs <id>

Examples (different agents in one dashboard):
  agentflt new --title "Claude" --repo . --cmd claude
  agentflt new --title "Gemini" --repo . --cmd gemini
  agentflt dashboard   # then 1/2 or j/k to switch, Enter to attach
`)
}

func runNew(db *store.DB, sup *supervisor.Supervisor, args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	title := fs.String("title", "", "Task title")
	agentType := fs.String("type", "", "Agent type label (e.g. claude, gpt-4, gemini, local)")
	repo := fs.String("repo", "", "Repo path (for git branch/modified files)")
	cwd := fs.String("cwd", "", "Working directory (default: repo)")
	cmd := fs.String("cmd", "", "Command to run (e.g. my-agent)")
	_ = fs.Parse(args)
	if *title == "" || *cmd == "" {
		fmt.Fprintln(os.Stderr, "new requires --title and --cmd")
		os.Exit(1)
	}
	sess, err := sup.CreateSession(*title, *agentType, *repo, *cwd, *cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created session %s\n", sess.ID)
	fmt.Printf("  tmux: %s\n", sess.TmuxSession)
	fmt.Printf("  attach: agentflt attach %s\n", sess.ID)
}

func runList(db *store.DB, _ []string) {
	sessions, err := db.ListSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list: %v\n", err)
		os.Exit(1)
	}
	for _, s := range sessions {
		fmt.Printf("%s  %s  %s  %s  %s\n", s.ID, s.State, s.Title, s.RepoPath, s.Branch)
	}
}

func runDashboard(db *store.DB, sup *supervisor.Supervisor) {
	model, err := tui.RunWithModel(db, sup)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
		os.Exit(1)
	}
	if model.ExitAttach != "" {
		if err := tmux.Attach(model.ExitAttach); err != nil {
			fmt.Fprintf(os.Stderr, "attach: %v\n", err)
			os.Exit(1)
		}
	}
}

func runAttach(db *store.DB, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "attach requires session id")
		os.Exit(1)
	}
	id := args[0]
	sess, ok, err := db.GetSession(id)
	if err != nil || !ok {
		fmt.Fprintf(os.Stderr, "session not found: %s\n", id)
		os.Exit(1)
	}
	if err := tmux.Attach(sess.TmuxSession); err != nil {
		fmt.Fprintf(os.Stderr, "attach: %v\n", err)
		os.Exit(1)
	}
}

func runRestart(db *store.DB, sup *supervisor.Supervisor, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "restart requires session id")
		os.Exit(1)
	}
	id := args[0]
	sess, ok, err := db.GetSession(id)
	if err != nil || !ok {
		fmt.Fprintf(os.Stderr, "session not found: %s\n", id)
		os.Exit(1)
	}
	_ = tmux.KillSession(sess.TmuxSession)
	newSess, err := sup.CreateSession(sess.Title, sess.AgentType, sess.RepoPath, sess.Cwd, sess.Command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restart: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Restarted as %s\n", newSess.ID)
}

func runStop(db *store.DB, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "stop requires session id")
		os.Exit(1)
	}
	id := args[0]
	sess, ok, err := db.GetSession(id)
	if err != nil || !ok {
		fmt.Fprintf(os.Stderr, "session not found: %s\n", id)
		os.Exit(1)
	}
	if err := tmux.KillSession(sess.TmuxSession); err != nil {
		fmt.Fprintf(os.Stderr, "stop: %v\n", err)
		os.Exit(1)
	}
	now := time.Now().Unix()
	_ = db.UpdateSessionState(id, supervisor.StateStopped, &now, nil, nil, nil)
	fmt.Println("Stopped")
}

func runLogs(db *store.DB, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "logs requires session id")
		os.Exit(1)
	}
	id := args[0]
	lines, err := db.GetOutputTail(id, 200)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logs: %v\n", err)
		os.Exit(1)
	}
	for _, l := range lines {
		fmt.Println(l)
	}
}
