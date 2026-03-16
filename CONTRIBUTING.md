# Contributing to agentflt

Thank you for your interest in contributing to **agentflt**! This document provides guidelines for contributing to the project.

---

## Getting Started

### Prerequisites

- **Go 1.21+**
- **tmux** (for testing session functionality)
- **git** (for repository operations)

### Setup

```bash
# Clone the repository
git clone https://github.com/cabaret-pro/agentflt
cd agentflt

# Build the binary
go build -o agentflt ./cmd/agentflt

# Run tests
go test ./...
```

---

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Make Your Changes

- Write clear, idiomatic Go code
- Follow existing code style and patterns
- Add tests for new functionality
- Update documentation as needed

### 3. Test Your Changes

```bash
# Run all tests
go test ./...

# Run a specific package's tests
go test ./internal/tui -v

# Build and test the binary
go build -o agentflt ./cmd/agentflt
./agentflt dashboard
```

### 4. Commit Your Changes

Use clear, descriptive commit messages following this format:

```
type: brief description

Longer explanation if needed.
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

**Examples:**
```
feat: add JSON export for agent timeline
fix: prevent spinner animation from stopping on refresh
docs: update README with new keybindings
```

### 5. Push and Create a Pull Request

```bash
git push origin feature/your-feature-name
```

Then open a PR on GitHub with:
- Clear title describing the change
- Description of what you changed and why
- Reference any related issues (`Fixes #123`)

---

## Code Style

### Go Code

- Use `gofmt` (automatically applied by most editors)
- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Keep functions focused and reasonably sized
- Add comments for non-obvious logic
- Use meaningful variable names

### TUI Code

- Use Lipgloss for all styling (don't use raw ANSI codes)
- Keep view logic separate from update logic (Bubbletea pattern)
- Test TUI changes manually in different terminal sizes

---

## Testing

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# Verbose output
go test -v ./...
```

### Writing Tests

- Place tests in `*_test.go` files alongside the code
- Use table-driven tests for multiple test cases
- Mock external dependencies (tmux, filesystem) when possible

---

## Project Structure

```
agentflt/
├── cmd/agentflt/          # CLI entry point
├── internal/
│   ├── filetree/          # File tree walking
│   ├── git/               # Git operations
│   ├── store/             # SQLite database
│   ├── supervisor/        # Agent state monitoring
│   ├── tmux/              # Tmux session management
│   └── tui/               # Bubbletea TUI
├── docs/                  # Documentation
├── README.md
├── CONTRIBUTING.md
└── LICENSE
```

---

## Areas to Contribute

### Good First Issues

- Improve error messages in the TUI
- Add more test coverage
- Fix typos or improve documentation
- Add new color themes

### Feature Ideas

See [ROADMAP.md](docs/ROADMAP.md) for planned features. Before starting work on a major feature, open an issue to discuss the approach.

### Bug Reports

When reporting bugs, please include:
- **Operating system and version**
- **Go version** (`go version`)
- **tmux version** (`tmux -V`)
- **Steps to reproduce**
- **Expected vs actual behavior**
- **Relevant logs** (from `/tmp/agentflt-debug.log`)

---

## Questions?

- Open an issue for questions about contributing
- Check existing issues and PRs for similar topics
- Review the [README](README.md) and [UI_GOALS](docs/UI_GOALS.md) docs

---

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
