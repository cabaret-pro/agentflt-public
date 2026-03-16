package git

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// ModifiedFile represents a file changed in the working tree or index.
type ModifiedFile struct {
	Status string // M, A, D, etc. (porcelain format)
	Path   string // relative to repo root
	Abs    string // absolute path
}

// ModifiedFiles returns files that are modified, added, or deleted in repoPath (git status --porcelain).
// Returns nil if repoPath is not a git repo or on error.
func ModifiedFiles(repoPath string) ([]ModifiedFile, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = abs
	out, err := cmd.Output()
	if err != nil {
		// not a repo or git not found
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	var files []ModifiedFile
	for _, line := range lines {
		if line == "" {
			continue
		}
		// porcelain: XY path (or "XY path\n path" for renames; we keep first path)
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx != -1 {
			path = strings.TrimSpace(path[idx+4:])
		}
		files = append(files, ModifiedFile{
			Status: status,
			Path:   path,
			Abs:    filepath.Join(abs, path),
		})
	}
	return files, nil
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
