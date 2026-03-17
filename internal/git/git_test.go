package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestModifiedFiles_NotARepo(t *testing.T) {
	dir := t.TempDir()
	// Write a file so the fallback directory listing has something to return.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	files, err := ModifiedFiles(dir, time.Time{})
	if err != nil {
		t.Fatalf("ModifiedFiles in non-repo returned error: %v", err)
	}
	// The fallback listing should return the file we created.
	var found bool
	for _, f := range files {
		if filepath.Base(f.Path) == "hello.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected hello.txt in non-repo listing, got %+v", files)
	}
}

func TestModifiedFiles_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skip("git not available or init failed:", err)
	}

	files, err := ModifiedFiles(dir, time.Time{})
	if err != nil {
		t.Fatalf("ModifiedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty repo: got %d files", len(files))
	}
}

func TestModifiedFiles_WithChanges(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skip("git not available:", err)
	}

	// Create a file and add it
	f := filepath.Join(dir, "foo.txt")
	if err := os.WriteFile(f, []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "add", "foo.txt").Run(); err != nil {
		t.Skip("git add failed:", err)
	}

	files, err := ModifiedFiles(dir, time.Time{})
	if err != nil {
		t.Fatalf("ModifiedFiles: %v", err)
	}
	if len(files) < 1 {
		t.Fatalf("expected at least one file, got %d", len(files))
	}
	// Resolve symlinks on the expected path (macOS /tmp -> /private/tmp)
	fWant := f
	if resolved, err := filepath.EvalSymlinks(f); err == nil {
		fWant = resolved
	}
	var found bool
	for _, file := range files {
		if filepath.Base(file.Path) == "foo.txt" {
			found = true
			if file.Abs != fWant {
				t.Errorf("Abs = %q want %q", file.Abs, fWant)
			}
			break
		}
	}
	if !found {
		t.Errorf("foo.txt not in %+v", files)
	}
}

func TestBranch_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := Branch(dir)
	if err == nil {
		t.Error("Branch in non-repo: expected error")
	}
}

func TestBranch_Repo(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skip("git not available:", err)
	}
	// Need at least one commit for HEAD to exist
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@test").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	if err := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run(); err != nil {
		t.Skip("git commit failed:", err)
	}

	branch, err := Branch(dir)
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if branch != "main" && branch != "master" {
		t.Errorf("branch = %q (expected main or master)", branch)
	}
}
