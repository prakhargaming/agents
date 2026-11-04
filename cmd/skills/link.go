package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type harness struct {
	name string
	// dir is relative to the home directory.
	dir string
	// shared is false for a harness that already reads ~/.agents/skills.
	shared bool
}

var harnesses = []harness{
	{name: "claude", dir: ".claude/skills", shared: true},
	{name: "codex", dir: ".codex/skills", shared: true},
	{name: "amp", dir: ".config/amp/skills", shared: false},
}

// ampSettings turns off Amp's scan of the Claude Code skill dirs. Without it,
// Amp also loads the skills that only Claude must get.
const (
	ampSettingsFile = ".config/amp/settings.json"
	ampSettingKey   = "amp.skills.disableClaudeCodeSkills"
)

type repo struct {
	root string
	home string
	out  io.Writer
}

// linkPlan maps each symlink path to the skill dir it must point at.
type linkPlan struct {
	links map[string]string
	// dirs are the dirs that hold only managed links, for pruning.
	dirs   []string
	shared int
}

func (r *repo) sharedDir() string { return filepath.Join(r.root, "skills") }

func (r *repo) pluginSkills(plugin string) string {
	return filepath.Join(r.root, "plugins", plugin, "skills")
}

// discover returns the skill dirs in dir by name. A missing dir has no skills.
func discover(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	skills := map[string]string{}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if isSkillDir(p) {
			skills[e.Name()] = p
		}
	}
	return skills, nil
}

func (r *repo) plan() (*linkPlan, error) {
	ups, err := readUpstreams(r.root)
	if err != nil {
		return nil, err
	}
	sources := []string{}
	for _, u := range ups {
		dir := filepath.Join(r.root, u.path, u.skills)
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("%s is missing. Run `skills install`", rel(r.root, dir))
		}
		sources = append(sources, dir)
	}
	sources = append(sources, r.pluginSkills("personal"))

	shared := map[string]string{}
	for _, dir := range sources {
		found, err := discover(dir)
		if err != nil {
			return nil, err
		}
		if err := merge(r.root, shared, found); err != nil {
			return nil, err
		}
	}

	p := &linkPlan{links: map[string]string{}, dirs: []string{r.sharedDir()}, shared: len(shared)}
	for name, src := range shared {
		p.links[filepath.Join(r.sharedDir(), name)] = src
	}
	for _, h := range harnesses {
		targets := map[string]string{}
		if h.shared {
			for name := range shared {
				targets[name] = filepath.Join(r.sharedDir(), name)
			}
		}
		extra, err := discover(r.pluginSkills(h.name))
		if err != nil {
			return nil, err
		}
		if err := merge(r.root, targets, extra); err != nil {
			return nil, fmt.Errorf("%s: %w", h.name, err)
		}
		dir := filepath.Join(r.home, h.dir)
		p.dirs = append(p.dirs, dir)
		for name, src := range targets {
			p.links[filepath.Join(dir, name)] = src
		}
	}
	return p, nil
}

func merge(root string, into, found map[string]string) error {
	for name, src := range found {
		if subcommands[name] {
			return fmt.Errorf("skill %s in %s has the name of a subcommand. Rename it", name, rel(root, src))
		}
		if prev, ok := into[name]; ok {
			return fmt.Errorf("two skills are named %s: %s and %s", name, rel(root, prev), rel(root, src))
		}
		into[name] = src
	}
	return nil
}

// link applies the plan. It refuses to touch a path that it does not manage,
// so it checks every path before it changes one.
func (r *repo) link() error {
	p, err := r.plan()
	if err != nil {
		return err
	}
	dests := make([]string, 0, len(p.links))
	for dest := range p.links {
		dests = append(dests, dest)
	}
	sort.Strings(dests)

	for _, dest := range dests {
		info, err := os.Lstat(dest)
		switch {
		case os.IsNotExist(err):
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink == 0:
			return fmt.Errorf("refusing to replace %s, which is not a symlink", dest)
		case !r.managed(dest) && linkTarget(dest) != p.links[dest]:
			target, _ := os.Readlink(dest)
			return fmt.Errorf("refusing to replace %s -> %s, which points outside %s", dest, target, r.root)
		}
	}

	for _, dest := range dests {
		src := p.links[dest]
		if linkTarget(dest) == src {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return err
		}
		target, err := filepath.Rel(filepath.Dir(dest), src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dest); err != nil {
			return err
		}
	}

	pruned := 0
	for _, dir := range p.dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			dest := filepath.Join(dir, e.Name())
			if _, ok := p.links[dest]; ok || e.Type()&os.ModeSymlink == 0 || !r.managed(dest) {
				continue
			}
			if err := os.Remove(dest); err != nil {
				return err
			}
			pruned++
		}
	}

	if err := r.setAmpSetting(); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Linked %d shared skills and the harness-specific skills.", p.shared)
	if pruned > 0 {
		fmt.Fprintf(r.out, " Removed %d stale links.", pruned)
	}
	fmt.Fprintln(r.out)
	return nil
}

// linkTarget returns the absolute, lexically clean target of a symlink, or
// "" if path is not a symlink.
func linkTarget(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

// managed reports whether the symlink at path points into the repo. A
// dangling link counts, because a removed skill leaves one.
func (r *repo) managed(path string) bool {
	target := linkTarget(path)
	if target == "" {
		return false
	}
	roots := []string{filepath.Clean(r.root)}
	if real, err := filepath.EvalSymlinks(r.root); err == nil {
		roots = append(roots, real)
	}
	candidates := []string{target}
	if real, err := filepath.EvalSymlinks(target); err == nil {
		candidates = append(candidates, real)
	}
	for _, root := range roots {
		for _, c := range candidates {
			if c == root || strings.HasPrefix(c, root+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

func (r *repo) setAmpSetting() error {
	path := filepath.Join(r.home, ampSettingsFile)
	settings := map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return err
	case len(bytes.TrimSpace(b)) > 0:
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("cannot read %s (%v). Set %q to true in it", path, err, ampSettingKey)
		}
	}
	if settings[ampSettingKey] == true {
		return nil
	}
	settings[ampSettingKey] = true
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}
