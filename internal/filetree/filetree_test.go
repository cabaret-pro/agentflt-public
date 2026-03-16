package filetree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalk_BasicStructure(t *testing.T) {
	// Create a temp tree: root/a.go  root/sub/b.go  root/.git/config
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(root, "sub", "b.go"), []byte("package sub"), 0644)
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".git", "config"), []byte(""), 0644)

	nodes, err := Walk(root, nil)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	// .git should be excluded, so only sub (dir) and a.go (file) at root depth
	names := make(map[string]bool)
	for _, n := range nodes {
		names[n.Name] = true
	}
	if names[".git"] {
		t.Error("expected .git to be excluded")
	}
	if !names["sub"] {
		t.Error("expected sub directory in nodes")
	}
	if !names["a.go"] {
		t.Error("expected a.go in nodes")
	}
	// sub is not expanded, so b.go should not appear
	if names["b.go"] {
		t.Error("expected b.go to be hidden (sub not expanded)")
	}
}

func TestWalk_ExpandedDir(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "sub")
	_ = os.MkdirAll(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "b.go"), []byte("package sub"), 0644)

	expanded := map[string]bool{subDir: true}
	nodes, err := Walk(root, expanded)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	names := make(map[string]bool)
	for _, n := range nodes {
		names[n.Name] = true
	}
	if !names["b.go"] {
		t.Error("expected b.go to appear when sub is expanded")
	}
}

func TestWalk_DirsFirst(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "aaa.go"), []byte(""), 0644)
	_ = os.MkdirAll(filepath.Join(root, "zzz"), 0755)

	nodes, err := Walk(root, nil)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(nodes))
	}
	if !nodes[0].IsDir {
		t.Errorf("expected first node to be a directory, got %s", nodes[0].Name)
	}
}
