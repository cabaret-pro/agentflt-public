package git

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ModifiedFile represents a file changed in the working tree or index.
type ModifiedFile struct {
	Status string // M, A, D, etc. (porcelain format)
	Path   string // relative to repo root
	Abs    string // absolute path
}

// ModifiedFiles returns files that are modified, added, or untracked relative to the nearest
// git repo root found by walking up from repoPath. If no git repo is found, it falls back to
// listing files in repoPath modified since the given `since` time (pass a zero time to use a
// 24-hour default window). The `since` parameter is ignored for git repos.
func ModifiedFiles(repoPath string, since time.Time) ([]ModifiedFile, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	// Resolve symlinks so paths agree with git's output (on macOS /tmp -> /private/tmp).
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}

	// Walk up from abs to find the git root.
	gitRoot, err := findGitRoot(abs)
	if err != nil || gitRoot == "" {
		// Not inside a git repo — list files modified since session start.
		return listDirFiles(abs, since)
	}

	cmd := exec.Command("git", "status", "--porcelain", "-u")
	cmd.Dir = gitRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	var files []ModifiedFile
	for _, line := range lines {
		if line == "" {
			continue
		}
		// porcelain: XY path (or "XY old -> new" for renames; keep the new path)
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx != -1 {
			path = strings.TrimSpace(path[idx+4:])
		}
		files = append(files, ModifiedFile{
			Status: status,
			Path:   path,
			Abs:    filepath.Join(gitRoot, path),
		})
	}
	return files, nil
}

// findGitRoot walks up from dir until it finds a directory containing ".git".
// Returns the directory path or "" if no git repo is found.
func findGitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// textExts is the set of file extensions considered readable text. Only these are included
// in the non-git directory fallback listing — binary files (executables, images, DBs, etc.)
// would produce garbage in the preview panel and are never relevant agent edits.
var textExts = map[string]bool{
	// code
	".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".rb": true, ".rs": true, ".java": true, ".c": true, ".cpp": true, ".h": true,
	".cs": true, ".swift": true, ".kt": true, ".php": true, ".lua": true, ".r": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ps1": true,
	// data / config
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".env": true, ".xml": true, ".csv": true, ".sql": true, ".graphql": true,
	// docs / markup
	".md": true, ".txt": true, ".rst": true, ".html": true, ".css": true,
	".scss": true, ".sass": true, ".less": true, ".tex": true,
	// misc text
	".tf": true, ".hcl": true, ".proto": true, ".dockerfile": true,
}

// isTextFile returns true if the file has a known text extension, or no extension but is
// small enough to be a script/config (executables with no extension are excluded by checking
// the executable bit would be complex; we skip extensionless files in the fallback listing).
func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		// Extensionless files could be executables (Makefile, Dockerfile, etc.) or binaries.
		// Allow known names; skip everything else to avoid showing binary executables.
		base := strings.ToLower(name)
		return base == "makefile" || base == "dockerfile" || base == "readme" ||
			base == "license" || base == "contributing" || base == "changelog"
	}
	return textExts[ext]
}

// noisyDirs lists directory names that should never be recursed into when scanning for
// agent-created files. These are either OS internals or large asset stores that produce
// false positives and drown out real work.
var noisyDirs = map[string]bool{
	// macOS system / user-data directories inside $HOME
	"Library": true, "Applications": true, "Movies": true, "Music": true, "Pictures": true,
	// Common build / dependency / cache directories
	"node_modules": true, "venv": true, ".venv": true,
	"__pycache__": true, ".cache": true, ".gradle": true, "vendor": true,
	"target": true, "dist": true, "build": true,
}

// fileEntry is used to sort walk results by modification time.
type fileEntry struct {
	mtime time.Time
	mf    ModifiedFile
}

// listDirFiles walks dir (up to 3 levels deep), collects files modified after `since`,
// then returns them sorted newest-first (up to 200). This is used as a fallback when the
// working directory is not inside a git repository — sorting by mtime ensures agent-created
// files always surface at the top even in large directories like $HOME.
func listDirFiles(dir string, since time.Time) ([]ModifiedFile, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	const maxDepth = 3
	const maxResults = 200
	// If since is zero (shouldn't happen in practice), fall back to 24 h.
	cutoff := since
	if cutoff.IsZero() {
		cutoff = time.Now().Add(-24 * time.Hour)
	}

	var entries []fileEntry
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden dirs (e.g. .git, .ssh) and known noise dirs.
			if strings.HasPrefix(name, ".") || noisyDirs[name] {
				return filepath.SkipDir
			}
			depth := strings.Count(rel, string(filepath.Separator))
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTextFile(d.Name()) {
			return nil
		}
		fi, ferr := d.Info()
		if ferr != nil {
			return nil
		}
		if fi.ModTime().After(cutoff) {
			entries = append(entries, fileEntry{
				mtime: fi.ModTime(),
				mf:    ModifiedFile{Status: "?", Path: rel, Abs: path},
			})
		}
		return nil
	})

	// Sort newest-first so the most relevant files appear at the top.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mtime.After(entries[j].mtime)
	})
	files := make([]ModifiedFile, 0, min(len(entries), maxResults))
	for i := range entries {
		if i >= maxResults {
			break
		}
		files = append(files, entries[i].mf)
	}
	return files, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Branch returns current branch name in repoPath.
func Branch(repoPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
