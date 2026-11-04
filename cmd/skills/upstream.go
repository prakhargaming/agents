package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const catalogFile = ".claude-plugin/marketplace.json"

// upstream is one submodule from .gitmodules.
type upstream struct {
	name   string // submodule name
	path   string
	url    string
	branch string
	// skills is the skills dir inside the submodule, from the custom
	// `skills` key.
	skills string
}

// sparse returns the dir to check out, or "" for the whole repo.
func (u upstream) sparse() string {
	if d := path.Dir(u.skills); d != "." {
		return d
	}
	return ""
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func readUpstreams(root string) ([]upstream, error) {
	if _, err := os.Stat(filepath.Join(root, ".gitmodules")); os.IsNotExist(err) {
		return nil, nil
	}
	out, err := git(root, "config", "-f", ".gitmodules", "--get-regexp", `^submodule\.`)
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return nil, nil
		}
		return nil, err
	}
	byName := map[string]*upstream{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(line, " ")
		k = strings.TrimPrefix(k, "submodule.")
		dot := strings.LastIndex(k, ".")
		if dot < 0 {
			continue
		}
		name, key := k[:dot], k[dot+1:]
		u, ok := byName[name]
		if !ok {
			u = &upstream{name: name}
			byName[name] = u
			order = append(order, name)
		}
		switch key {
		case "path":
			u.path = v
		case "url":
			u.url = v
		case "branch":
			u.branch = v
		case "skills":
			u.skills = v
		}
	}
	ups := make([]upstream, 0, len(order))
	for _, name := range order {
		u := *byName[name]
		if u.skills == "" {
			return nil, fmt.Errorf(".gitmodules: submodule %s has no skills key", name)
		}
		ups = append(ups, u)
	}
	return ups, nil
}

// catalog is the Claude marketplace file. Unknown top-level fields stay as
// they are.
type catalog struct {
	Name     string          `json:"name"`
	Owner    json.RawMessage `json:"owner,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Plugins  []plugin        `json:"plugins"`
}

type plugin struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Source      pluginSource `json:"source"`
}

// pluginSource is a local path string or a remote source object.
type pluginSource struct {
	Local  string
	Remote *remoteSource
}

type remoteSource struct {
	Kind string `json:"source"`
	URL  string `json:"url,omitempty"`
	Repo string `json:"repo,omitempty"`
	Path string `json:"path,omitempty"`
	Ref  string `json:"ref,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

func (s pluginSource) MarshalJSON() ([]byte, error) {
	if s.Remote != nil {
		return json.Marshal(s.Remote)
	}
	return json.Marshal(s.Local)
}

func (s *pluginSource) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &s.Local)
	}
	s.Remote = &remoteSource{}
	return json.Unmarshal(b, s.Remote)
}

func readCatalog(root string) (*catalog, error) {
	b, err := os.ReadFile(filepath.Join(root, catalogFile))
	if err != nil {
		return nil, err
	}
	var c catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", catalogFile, err)
	}
	return &c, nil
}

func writeCatalog(root string, c *catalog) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, catalogFile), buf.Bytes(), 0o644)
}

var githubURL = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

func githubRepo(url string) string {
	if m := githubURL.FindStringSubmatch(url); m != nil {
		return m[1]
	}
	return ""
}

func sameRepo(a, b string) bool {
	norm := func(s string) string {
		if r := githubRepo(s); r != "" {
			return "github:" + strings.ToLower(r)
		}
		return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	}
	return norm(a) == norm(b)
}

// entry finds the catalog plugin for an upstream by its repo and path.
func (c *catalog) entry(u upstream) *plugin {
	for i := range c.Plugins {
		s := c.Plugins[i].Source.Remote
		if s == nil {
			continue
		}
		url := s.URL
		if s.Kind == "github" {
			url = "https://github.com/" + s.Repo
		}
		if sameRepo(url, u.url) && s.Path == u.sparse() {
			return &c.Plugins[i]
		}
	}
	return nil
}

func newEntry(name string, u upstream, sha string) plugin {
	src := &remoteSource{Ref: u.branch, SHA: sha}
	switch repo := githubRepo(u.url); {
	case u.sparse() != "":
		src.Kind, src.URL, src.Path = "git-subdir", u.url, u.sparse()
	case repo != "":
		src.Kind, src.Repo = "github", repo
	default:
		src.Kind, src.URL = "url", u.url
	}
	return plugin{Name: name, Description: "Upstream " + name + " skills.", Source: pluginSource{Remote: src}}
}

const personalPlugin = "personal"

// syncCatalog writes each upstream SHA to the catalog, and keeps the
// personal entry only while the personal plugin has skills.
func (r *repo) syncCatalog() error {
	c, err := readCatalog(r.root)
	if err != nil {
		return err
	}
	ups, err := readUpstreams(r.root)
	if err != nil {
		return err
	}
	for _, u := range ups {
		e := c.entry(u)
		if e == nil {
			return fmt.Errorf("%s has no entry for %s", catalogFile, u.url)
		}
		sha, err := git(filepath.Join(r.root, u.path), "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		e.Source.Remote.SHA = sha
	}

	personal, err := discover(r.pluginSkills(personalPlugin))
	if err != nil {
		return err
	}
	idx := -1
	for i, p := range c.Plugins {
		if p.Name == personalPlugin {
			idx = i
		}
	}
	switch {
	case len(personal) > 0 && idx < 0:
		c.Plugins = append([]plugin{{
			Name:        personalPlugin,
			Description: "Personal skills.",
			Source:      pluginSource{Local: "./plugins/" + personalPlugin},
		}}, c.Plugins...)
	case len(personal) == 0 && idx >= 0:
		c.Plugins = append(c.Plugins[:idx], c.Plugins[idx+1:]...)
	}
	return writeCatalog(r.root, c)
}

// commit commits only paths, so unrelated staged work stays out.
func (r *repo) commit(msg, body string, paths ...string) error {
	for _, p := range paths {
		// A path that `git rm` already removed matches nothing, and that is fine.
		if _, err := git(r.root, "add", "-A", "--", p); err != nil && !strings.Contains(err.Error(), "did not match") {
			return err
		}
	}
	if _, err := git(r.root, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return nil
	}
	if body != "" {
		msg += "\n\n" + body
	}
	if _, err := git(r.root, append([]string{"commit", "-q", "-m", msg, "--"}, paths...)...); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Committed %q. Push it when you are ready.\n", strings.SplitN(msg, "\n", 2)[0])
	return nil
}

// checkout initializes each submodule at its pinned commit and limits it
// to the dir that holds its skills.
func (r *repo) checkout(ups []upstream) error {
	for _, u := range ups {
		dir := filepath.Join(r.root, u.path)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		out, err := git(dir, "status", "--porcelain")
		if err != nil {
			return err
		}
		if out != "" {
			return fmt.Errorf("%s has local changes. Commit or discard them first", u.path)
		}
	}
	for _, u := range ups {
		if _, err := git(r.root, "submodule", "update", "--init", "--depth", "1", "--", u.path); err != nil {
			return err
		}
		if err := r.sparseCheckout(u); err != nil {
			return err
		}
	}
	return nil
}

func (r *repo) sparseCheckout(u upstream) error {
	if u.sparse() == "" {
		return nil
	}
	_, err := git(filepath.Join(r.root, u.path), "sparse-checkout", "set", u.sparse())
	return err
}

func (r *repo) install() error {
	ups, err := readUpstreams(r.root)
	if err != nil {
		return err
	}
	if err := r.checkout(ups); err != nil {
		return err
	}
	return r.link()
}

func (r *repo) update() error {
	if _, err := git(r.root, "pull", "-q", "--ff-only"); err != nil {
		return err
	}
	ups, err := readUpstreams(r.root)
	if err != nil {
		return err
	}
	if err := r.checkout(ups); err != nil {
		return err
	}
	paths := []string{".gitmodules", catalogFile}
	for _, u := range ups {
		dir := filepath.Join(r.root, u.path)
		branch := u.branch
		if branch == "" {
			branch = "HEAD"
		}
		if _, err := git(dir, "fetch", "-q", "--depth", "1", "origin", branch); err != nil {
			return err
		}
		if _, err := git(dir, "checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
			return err
		}
		paths = append(paths, u.path)
	}
	if err := r.syncCatalog(); err != nil {
		return err
	}
	if err := r.link(); err != nil {
		return err
	}
	body, err := git(r.root, append([]string{"diff", "--submodule=log", "--"}, paths...)...)
	if err != nil {
		return err
	}
	return r.commit("Update upstream skills", submoduleLog(body), paths...)
}

// submoduleLog keeps the submodule lines of a diff and drops the catalog
// hunk, which only repeats the SHAs.
func submoduleLog(diff string) string {
	var keep []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "Submodule ") || strings.HasPrefix(line, "  > ") || strings.HasPrefix(line, "  < ") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

func (r *repo) addUpstream(url, name, skillsPath string) error {
	if name == "" {
		name = strings.TrimSuffix(path.Base(strings.TrimSuffix(url, "/")), ".git")
	}
	if subcommands[name] || name == personalPlugin || strings.ContainsAny(name, `/\ `) || strings.HasPrefix(name, ".") {
		return fmt.Errorf("cannot use %q as an upstream name. Pass --name", name)
	}
	c, err := readCatalog(r.root)
	if err != nil {
		return err
	}
	for _, p := range c.Plugins {
		if p.Name == name {
			return fmt.Errorf("the catalog already has a plugin named %s. Pass --name", name)
		}
	}
	u := upstream{name: "upstream/" + name, path: "upstream/" + name, url: url}
	if _, err := os.Lstat(filepath.Join(r.root, u.path)); err == nil {
		return fmt.Errorf("%s already exists", u.path)
	}

	modules := filepath.Join(r.root, ".gitmodules")
	before, beforeErr := os.ReadFile(modules)
	if _, err := git(r.root, "submodule", "add", "-q", "--depth", "1", "--name", u.name, "--", url, u.path); err != nil {
		return err
	}
	if err := r.finishAdd(c, name, &u, skillsPath); err != nil {
		rbErr := r.removeSubmodule(u)
		if beforeErr == nil {
			rbErr = errors.Join(rbErr, os.WriteFile(modules, before, 0o644))
			_, addErr := git(r.root, "add", "--", ".gitmodules")
			rbErr = errors.Join(rbErr, addErr)
		} else {
			git(r.root, "rm", "-q", "-f", "--cached", "--", ".gitmodules")
			rbErr = errors.Join(rbErr, os.Remove(modules))
		}
		if rbErr != nil {
			err = errors.Join(err, fmt.Errorf("roll back %s: %w", u.path, rbErr))
		}
		return err
	}
	return r.commit("Add "+name+" upstream skills", "", ".gitmodules", catalogFile, u.path)
}

func (r *repo) finishAdd(c *catalog, name string, u *upstream, skillsPath string) error {
	dir := filepath.Join(r.root, u.path)
	if skillsPath == "" {
		var err error
		if skillsPath, err = detectSkills(dir); err != nil {
			return err
		}
	}
	skillsPath = path.Clean(filepath.ToSlash(skillsPath))
	if info, err := os.Stat(filepath.Join(dir, skillsPath)); err != nil || !info.IsDir() {
		return fmt.Errorf("%s has no dir %s", u.url, skillsPath)
	}
	u.skills = skillsPath
	branch, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	u.branch = branch
	for k, v := range map[string]string{"branch": u.branch, "skills": u.skills} {
		if _, err := git(r.root, "config", "-f", ".gitmodules", "submodule."+u.name+"."+k, v); err != nil {
			return err
		}
	}
	if err := r.sparseCheckout(*u); err != nil {
		return err
	}
	if _, err := r.plan(); err != nil {
		return err
	}
	sha, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	c.Plugins = append(c.Plugins, newEntry(name, *u, sha))
	if err := writeCatalog(r.root, c); err != nil {
		return err
	}
	return r.link()
}

// detectSkills finds the skills dir of a new upstream: a root skills/, or
// else the single */skills dir that holds skills.
func detectSkills(dir string) (string, error) {
	if info, err := os.Stat(filepath.Join(dir, "skills")); err == nil && info.IsDir() {
		return "skills", nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*", "skills"))
	if err != nil {
		return "", err
	}
	var found []string
	for _, m := range matches {
		skills, err := discover(m)
		if err != nil {
			return "", err
		}
		if len(skills) > 0 {
			found = append(found, filepath.ToSlash(rel(dir, m)))
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", errors.New("cannot find a skills dir. Pass --skills <path>")
	}
	return "", fmt.Errorf("found more than one skills dir (%s). Pass --skills <path>", strings.Join(found, ", "))
}

func (r *repo) addPersonal(src, harnessName string) error {
	plugin := personalPlugin
	if harnessName != "" {
		known := false
		for _, h := range harnesses {
			known = known || h.name == harnessName
		}
		if !known {
			return fmt.Errorf("unknown harness %s. Use claude, codex, or amp", harnessName)
		}
		plugin = harnessName
	}
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	name := filepath.Base(src)
	if r.skillInUse(name) {
		return fmt.Errorf("a skill named %s already exists", name)
	}
	dest := filepath.Join(r.pluginSkills(plugin), name)
	if err := copyDir(src, dest); err != nil {
		return err
	}
	if err := r.relinkAndSync(); err != nil {
		os.RemoveAll(dest)
		return err
	}
	return r.commit("Add "+name+" skill", "", rel(r.root, dest), catalogFile)
}

// skillInUse reports whether any upstream or plugin has a skill named name.
func (r *repo) skillInUse(name string) bool {
	if subcommands[name] {
		return true
	}
	p, err := r.plan()
	if err != nil {
		return false
	}
	for _, src := range p.links {
		if filepath.Base(src) == name {
			return true
		}
	}
	return false
}

func (r *repo) relinkAndSync() error {
	if _, err := r.plan(); err != nil {
		return err
	}
	if err := r.syncCatalog(); err != nil {
		return err
	}
	return r.link()
}

func (r *repo) remove(name string) error {
	for _, plugin := range append([]string{personalPlugin}, harnessNames()...) {
		dir := filepath.Join(r.pluginSkills(plugin), name)
		if !isSkillDir(dir) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := r.relinkAndSync(); err != nil {
			return err
		}
		return r.commit("Remove "+name+" skill", "", rel(r.root, dir), catalogFile)
	}

	ups, err := readUpstreams(r.root)
	if err != nil {
		return err
	}
	c, err := readCatalog(r.root)
	if err != nil {
		return err
	}
	for _, u := range ups {
		e := c.entry(u)
		if path.Base(u.path) != name && (e == nil || e.Name != name) {
			continue
		}
		if e != nil {
			for i := range c.Plugins {
				if &c.Plugins[i] == e {
					c.Plugins = append(c.Plugins[:i], c.Plugins[i+1:]...)
					break
				}
			}
		}
		if err := r.removeSubmodule(u); err != nil {
			return err
		}
		if err := writeCatalog(r.root, c); err != nil {
			return err
		}
		if err := r.link(); err != nil {
			return err
		}
		return r.commit("Remove "+name+" upstream skills", "", ".gitmodules", catalogFile, u.path)
	}
	return fmt.Errorf("no personal skill or upstream is named %s", name)
}

func harnessNames() []string {
	var names []string
	for _, h := range harnesses {
		names = append(names, h.name)
	}
	return names
}

func (r *repo) removeSubmodule(u upstream) error {
	git(r.root, "submodule", "deinit", "-q", "-f", "--", u.path)
	if _, err := git(r.root, "rm", "-q", "-f", "--", u.path); err != nil {
		return err
	}
	gitDir, err := git(r.root, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(r.root, gitDir)
	}
	return os.RemoveAll(filepath.Join(gitDir, "modules", u.name))
}

func copyDir(src, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel(src, p))
		if p == src {
			target = dest
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, info.Mode().Perm())
		}
	})
}
