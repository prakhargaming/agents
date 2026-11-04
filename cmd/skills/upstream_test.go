package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bareRepo makes a bare repo whose first commit has files, and returns its
// path and a work clone that can push to it.
func bareRepo(t *testing.T, files map[string]string) (bare, work string) {
	t.Helper()
	base := t.TempDir()
	bare, work = filepath.Join(base, "src.git"), filepath.Join(base, "work")
	sh(t, base, "git", "init", "-q", "--bare", bare)
	sh(t, base, "git", "clone", "-q", bare, work)
	for p, content := range files {
		writeFile(t, filepath.Join(work, p), content)
	}
	sh(t, work, "git", "add", "-A")
	sh(t, work, "git", "commit", "-q", "-m", "init")
	sh(t, work, "git", "push", "-q", "origin", "HEAD:main")
	return bare, work
}

func skillFile(name string) string {
	return "---\nname: " + name + "\ndescription: " + name + ".\n---\n"
}

// agentsRepo makes an agents checkout with an empty catalog and an origin.
func agentsRepo(t *testing.T) *repo {
	t.Helper()
	bare, _ := bareRepo(t, map[string]string{
		catalogFile:                        emptyCatalog,
		".gitignore":                       "/skills/\n",
		"plugins/personal/skills/.gitkeep": "",
	})
	root := filepath.Join(t.TempDir(), "agents")
	sh(t, "/", "git", "clone", "-q", bare, root)
	return newRepo(root, t.TempDir())
}

func TestAddDetectsSkillsPath(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string]string
		skills   string
		kind     string
		path     string
		wantLink string
	}{
		{
			name:     "root",
			files:    map[string]string{"skills/one/SKILL.md": skillFile("one"), "pkg/skills/two/SKILL.md": skillFile("two")},
			skills:   "skills",
			kind:     "url",
			wantLink: "one",
		},
		{
			name: "one nested dir",
			files: map[string]string{
				"pkg/skills/two/SKILL.md": skillFile("two"),
				"docs/skills/README.md":   "not a skill",
			},
			skills:   "pkg/skills",
			kind:     "git-subdir",
			path:     "pkg",
			wantLink: "two",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := agentsRepo(t)
			bare, _ := bareRepo(t, tc.files)

			if err := r.addUpstream(bare, "lib", ""); err != nil {
				t.Fatal(err)
			}

			ups, err := readUpstreams(r.root)
			if err != nil || len(ups) != 1 || ups[0].skills != tc.skills || ups[0].branch != "main" {
				t.Fatalf("upstreams %+v, %v", ups, err)
			}
			c, err := readCatalog(r.root)
			if err != nil {
				t.Fatal(err)
			}
			e := c.entry(ups[0])
			sha := sh(t, r.root, "git", "-C", "upstream/lib", "rev-parse", "HEAD")
			if e == nil || e.Name != "lib" || e.Source.Remote.Kind != tc.kind || e.Source.Remote.Path != tc.path || e.Source.Remote.SHA != sha {
				t.Fatalf("catalog entry %+v", e)
			}
			if got := linkNames(t, filepath.Join(r.home, ".claude/skills")); !slices.Equal(got, []string{tc.wantLink}) {
				t.Errorf("claude links %v", got)
			}
			if out := sh(t, r.root, "git", "status", "--porcelain"); out != "" {
				t.Errorf("uncommitted changes:\n%s", out)
			}
			if msg := sh(t, r.root, "git", "log", "-1", "--format=%s"); msg != "Add lib upstream skills" {
				t.Errorf("last commit %q", msg)
			}
		})
	}
}

func TestAddAmbiguousLayoutNeedsFlagAndRollsBack(t *testing.T) {
	r := agentsRepo(t)
	bare, _ := bareRepo(t, map[string]string{
		"a/skills/x/SKILL.md": skillFile("x"),
		"b/skills/y/SKILL.md": skillFile("y"),
	})

	err := r.addUpstream(bare, "lib", "")
	if err == nil || !strings.Contains(err.Error(), "--skills") {
		t.Fatalf("got %v", err)
	}
	if out := sh(t, r.root, "git", "status", "--porcelain"); out != "" {
		t.Fatalf("rollback left changes:\n%s", out)
	}

	if err := r.addUpstream(bare, "lib", "b/skills"); err != nil {
		t.Fatal(err)
	}
	if got := linkNames(t, filepath.Join(r.root, "skills")); !slices.Equal(got, []string{"y"}) {
		t.Errorf("index %v", got)
	}
}

func TestUpdateMovesPinAndCommits(t *testing.T) {
	r := agentsRepo(t)
	bare, work := bareRepo(t, map[string]string{"skills/one/SKILL.md": skillFile("one")})
	if err := r.addUpstream(bare, "lib", ""); err != nil {
		t.Fatal(err)
	}
	sh(t, r.root, "git", "push", "-q", "origin", "HEAD:main")
	sh(t, r.root, "git", "branch", "-q", "-u", "origin/main")

	writeFile(t, filepath.Join(work, "skills/two/SKILL.md"), skillFile("two"))
	sh(t, work, "git", "add", "-A")
	sh(t, work, "git", "commit", "-q", "-m", "Add two")
	sh(t, work, "git", "push", "-q", "origin", "HEAD:main")
	tip := sh(t, work, "git", "rev-parse", "HEAD")

	if err := r.update(); err != nil {
		t.Fatal(err)
	}

	c, _ := readCatalog(r.root)
	if sha := c.Plugins[0].Source.Remote.SHA; sha != tip {
		t.Errorf("catalog sha %s, want %s", sha, tip)
	}
	if pin := sh(t, r.root, "git", "rev-parse", "HEAD:upstream/lib"); pin != tip {
		t.Errorf("committed pin %s, want %s", pin, tip)
	}
	if body := sh(t, r.root, "git", "log", "-1", "--format=%B"); !strings.HasPrefix(body, "Update upstream skills") || !strings.Contains(body, "> Add two") {
		t.Errorf("commit message:\n%s", body)
	}
	if got := linkNames(t, filepath.Join(r.home, ".codex/skills")); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("codex links %v", got)
	}

	before := sh(t, r.root, "git", "rev-parse", "HEAD")
	if err := r.update(); err != nil {
		t.Fatal(err)
	}
	if after := sh(t, r.root, "git", "rev-parse", "HEAD"); after != before {
		t.Error("an update with no upstream change made a commit")
	}
}

func TestInstallRefusesDirtySubmodule(t *testing.T) {
	r := agentsRepo(t)
	bare, _ := bareRepo(t, map[string]string{"skills/one/SKILL.md": skillFile("one")})
	if err := r.addUpstream(bare, "lib", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(r.root, "upstream/lib/skills/one/SKILL.md"), "edited")

	if err := r.install(); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("got %v", err)
	}
}

func TestAddAndRemovePersonalSkill(t *testing.T) {
	r := agentsRepo(t)
	src := filepath.Join(t.TempDir(), "mine")
	writeSkill(t, src, "Mine.")
	writeFile(t, filepath.Join(src, "scripts/run.sh"), "echo hi")

	if err := r.addPersonal(src, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.home, ".claude/skills/mine/scripts/run.sh")); err != nil {
		t.Error(err)
	}
	c, _ := readCatalog(r.root)
	if len(c.Plugins) != 1 || c.Plugins[0].Name != personalPlugin {
		t.Errorf("catalog after add: %+v", c.Plugins)
	}
	if err := r.addPersonal(src, "amp"); err == nil {
		t.Error("added a second skill named mine")
	}

	if err := r.remove("mine"); err != nil {
		t.Fatal(err)
	}
	c, _ = readCatalog(r.root)
	if len(c.Plugins) != 0 {
		t.Errorf("catalog after remove: %+v", c.Plugins)
	}
	if got := linkNames(t, filepath.Join(r.home, ".claude/skills")); len(got) != 0 {
		t.Errorf("claude links after remove: %v", got)
	}
	if out := sh(t, r.root, "git", "status", "--porcelain"); out != "" {
		t.Errorf("uncommitted changes:\n%s", out)
	}
}

func TestAddHarnessSkillLinksOnlyThatHarness(t *testing.T) {
	r := agentsRepo(t)
	src := filepath.Join(t.TempDir(), "amp-thing")
	writeSkill(t, src, "Amp.")

	if err := r.addPersonal(src, "amp"); err != nil {
		t.Fatal(err)
	}
	if got := linkNames(t, filepath.Join(r.home, ".config/amp/skills")); !slices.Equal(got, []string{"amp-thing"}) {
		t.Errorf("amp links %v", got)
	}
	if got := linkNames(t, filepath.Join(r.home, ".claude/skills")); len(got) != 0 {
		t.Errorf("claude links %v", got)
	}
}

func TestRemoveUpstream(t *testing.T) {
	r := agentsRepo(t)
	bare, _ := bareRepo(t, map[string]string{"skills/one/SKILL.md": skillFile("one")})
	if err := r.addUpstream(bare, "lib", ""); err != nil {
		t.Fatal(err)
	}

	if err := r.remove("lib"); err != nil {
		t.Fatal(err)
	}
	if ups, _ := readUpstreams(r.root); len(ups) != 0 {
		t.Errorf("upstreams %+v", ups)
	}
	if c, _ := readCatalog(r.root); len(c.Plugins) != 0 {
		t.Errorf("catalog %+v", c.Plugins)
	}
	if got := linkNames(t, filepath.Join(r.home, ".claude/skills")); len(got) != 0 {
		t.Errorf("claude links %v", got)
	}
	if out := sh(t, r.root, "git", "status", "--porcelain"); out != "" {
		t.Errorf("uncommitted changes:\n%s", out)
	}
	// The name is free again.
	if err := r.addUpstream(bare, "lib", ""); err != nil {
		t.Fatal(err)
	}
}
