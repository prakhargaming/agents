package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME":     "test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		// Submodules from local paths need this since git 2.38.1.
		"GIT_CONFIG_COUNT":   "2",
		"GIT_CONFIG_KEY_0":   "protocol.file.allow",
		"GIT_CONFIG_VALUE_0": "always",
		"GIT_CONFIG_KEY_1":   "init.defaultBranch",
		"GIT_CONFIG_VALUE_1": "main",
	} {
		os.Setenv(k, v)
	}
	os.Exit(m.Run())
}

func writeSkill(t *testing.T, dir, description string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+filepath.Base(dir)+"\ndescription: "+description+"\n---\n\nBody.\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sh(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// linkNames returns the names of the symlinks in dir.
func linkNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			names = append(names, e.Name())
		}
	}
	return names
}

func newRepo(root, home string) *repo {
	return &repo{root: root, home: home, out: io.Discard}
}

const emptyCatalog = `{
  "name": "test",
  "plugins": []
}
`
