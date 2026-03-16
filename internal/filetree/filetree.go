package filetree

import (
	"os"
	"path/filepath"
	"sort"
)

// Node represents one entry in the file tree flat list.
type Node struct {
	Name    string
	RelPath string // relative to Walk root
	AbsPath string
	IsDir   bool
	Depth   int
}

// skipDirs are directories that are never walked.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	"build":        true,
}

// Walk returns a flat, depth-first list of Nodes starting at root.
// Directories are included and appear before their children.
// expandedDirs controls which directories are expanded; if nil all top-level
// entries are shown collapsed (only direct children of root are included unless
// the caller expands them).
func Walk(root string, expandedDirs map[string]bool) ([]Node, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var nodes []Node
	walkDir(absRoot, absRoot, 0, expandedDirs, &nodes)
	return nodes, nil
}

func walkDir(root, dir string, depth int, expanded map[string]bool, out *[]Node) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// Dirs first, then files — both sorted alphabetically.
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di // dirs first
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && skipDirs[name] {
			continue
		}
		// Skip hidden files/dirs (starting with .) except at root depth.
		if len(name) > 0 && name[0] == '.' && depth > 0 {
			continue
		}
		abs := filepath.Join(dir, name)
		rel, _ := filepath.Rel(root, abs)
		node := Node{
			Name:    name,
			RelPath: rel,
			AbsPath: abs,
			IsDir:   e.IsDir(),
			Depth:   depth,
		}
		*out = append(*out, node)
		if e.IsDir() && expanded != nil && expanded[abs] {
			walkDir(root, abs, depth+1, expanded, out)
		}
	}
}
