package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// linkFixture makes a repo with one upstream skill (up), one personal skill
// (mine), and one extra skill for each harness.
func linkFixture(t *testing.T) (root, home string) {
	t.Helper()
	root, home = t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"upstream/u\"]\n\tpath = upstream/u\n\turl = https://example.com/u.git\n\tskills = skills\n")
	writeSkill(t, filepath.Join(root, "upstream/u/skills/up"), "Upstream.")
	writeSkill(t, filepath.Join(root, "plugins/personal/skills/mine"), "Mine.")
	writeSkill(t, filepath.Join(root, "plugins/claude/skills/claude-only"), "Claude.")
	writeSkill(t, filepath.Join(root, "plugins/codex/skills/codex-only"), "Codex.")
	writeSkill(t, filepath.Join(root, "plugins/amp/skills/amp-only"), "Amp.")
	return root, home
}

func TestLinkGivesEachHarnessItsSkills(t *testing.T) {
	root, home := linkFixture(t)
	writeFile(t, filepath.Join(home, ampSettingsFile), `{"amp.other": 1}`)

	if err := newRepo(root, home).link(); err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		filepath.Join(root, "skills"):             {"mine", "up"},
		filepath.Join(home, ".claude/skills"):     {"claude-only", "mine", "up"},
		filepath.Join(home, ".codex/skills"):      {"codex-only", "mine", "up"},
		filepath.Join(home, ".config/amp/skills"): {"amp-only"},
	}
	for dir, names := range want {
		if got := linkNames(t, dir); !slices.Equal(got, names) {
			t.Errorf("%s: got %v, want %v", dir, got, names)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".claude/skills/up/SKILL.md")); err != nil || !strings.Contains(string(b), "Upstream.") {
		t.Errorf("claude link to up does not reach the upstream skill: %v", err)
	}

	var settings map[string]any
	b, _ := os.ReadFile(filepath.Join(home, ampSettingsFile))
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	if settings[ampSettingKey] != true || settings["amp.other"] != 1.0 {
		t.Errorf("amp settings: %v", settings)
	}
}

func TestLinkRefusesDuplicateName(t *testing.T) {
	root, home := linkFixture(t)
	writeSkill(t, filepath.Join(root, "plugins/personal/skills/up"), "Shadow.")

	err := newRepo(root, home).link()
	if err == nil || !strings.Contains(err.Error(), "two skills are named up") {
		t.Fatalf("got %v", err)
	}
	if names := linkNames(t, filepath.Join(home, ".claude/skills")); len(names) != 0 {
		t.Errorf("linked %v after a refusal", names)
	}
}

func TestLinkRefusesHarnessSkillThatShadowsShared(t *testing.T) {
	root, home := linkFixture(t)
	writeSkill(t, filepath.Join(root, "plugins/codex/skills/mine"), "Shadow.")

	if err := newRepo(root, home).link(); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("got %v", err)
	}
}

func TestLinkRefusesSubcommandName(t *testing.T) {
	root, home := linkFixture(t)
	writeSkill(t, filepath.Join(root, "plugins/amp/skills/update"), "Bad.")

	if err := newRepo(root, home).link(); err == nil || !strings.Contains(err.Error(), "subcommand") {
		t.Fatalf("got %v", err)
	}
}

func TestLinkRefusesUnmanagedPath(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, dest string){
		"dir": func(t *testing.T, dest string) {
			writeFile(t, filepath.Join(dest, "keep"), "x")
		},
		"symlink outside the repo": func(t *testing.T, dest string) {
			if err := os.Symlink(t.TempDir(), dest); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, home := linkFixture(t)
			dest := filepath.Join(home, ".claude/skills/mine")
			os.MkdirAll(filepath.Dir(dest), 0o755)
			setup(t, dest)

			if err := newRepo(root, home).link(); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
				t.Fatalf("got %v", err)
			}
			if names := linkNames(t, filepath.Join(home, ".codex/skills")); len(names) != 0 {
				t.Errorf("linked %v before the refusal", names)
			}
		})
	}
}

func TestLinkPrunesStaleLinksOnly(t *testing.T) {
	root, home := linkFixture(t)
	r := newRepo(root, home)
	if err := r.link(); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(home, ".claude/skills/foreign")
	if err := os.Symlink(t.TempDir(), foreign); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(root, "plugins/personal/skills/mine")); err != nil {
		t.Fatal(err)
	}
	if err := r.link(); err != nil {
		t.Fatal(err)
	}

	if got := linkNames(t, filepath.Join(home, ".claude/skills")); !slices.Equal(got, []string{"claude-only", "foreign", "up"}) {
		t.Errorf("claude: %v", got)
	}
	if got := linkNames(t, filepath.Join(root, "skills")); !slices.Equal(got, []string{"up"}) {
		t.Errorf("index: %v", got)
	}
}

func TestLinkRepointsMovedSkill(t *testing.T) {
	root, home := linkFixture(t)
	r := newRepo(root, home)
	if err := r.link(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".gitmodules"), "[submodule \"upstream/u\"]\n\tpath = upstream/u\n\turl = https://example.com/u.git\n\tskills = moved\n")
	if err := os.Rename(filepath.Join(root, "upstream/u/skills"), filepath.Join(root, "upstream/u/moved")); err != nil {
		t.Fatal(err)
	}

	if err := r.link(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude/skills/up/SKILL.md")); err != nil {
		t.Error(err)
	}
}
