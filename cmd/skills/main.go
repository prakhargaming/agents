// Command skills installs, updates, and browses the agent skills in ~/.agents.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var version = "dev"

// subcommands is also the set of names a skill cannot take, because
// `skills <name>` prints a skill.
var subcommands = map[string]bool{
	"install": true,
	"update":  true,
	"add":     true,
	"remove":  true,
	"help":    true,
	"version": true,
}

const usage = `Usage:
  skills [-f|--full] [-d|--descriptions|name]
  skills install [--home dir]
  skills update [--home dir]
  skills add <url> [--name n] [--skills path] [--home dir]
  skills add <dir> [--harness claude|codex|amp] [--home dir]
  skills remove <name> [--home dir]
`

// errUsage makes main exit with status 2.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Stdout)
	var exit exitError
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	case errors.As(err, &exit):
		fmt.Fprintln(os.Stderr, exit.msg)
		os.Exit(exit.code)
	default:
		fmt.Fprintln(os.Stderr, "skills:", err)
		os.Exit(1)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

func run(args []string, out io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if len(args) == 0 || !subcommands[args[0]] {
		return list(filepath.Join(root, "skills"), args, out)
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "help":
		fmt.Fprint(out, usage)
		return nil
	case "version":
		fmt.Fprintln(out, version)
		return nil
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	home := fs.String("home", "", "harness home directory (default $HOME)")
	name := fs.String("name", "", "upstream name")
	skillsPath := fs.String("skills", "", "skills path inside the upstream repo")
	harness := fs.String("harness", "", "harness for a personal skill")
	if err := fs.Parse(interleave(fs, args)); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if *home == "" {
		if *home, err = os.UserHomeDir(); err != nil {
			return err
		}
	}
	r := &repo{root: root, home: *home, out: out}
	pos := fs.Args()

	switch cmd {
	case "install":
		if len(pos) != 0 {
			return errUsage
		}
		return r.install()
	case "update":
		if len(pos) != 0 {
			return errUsage
		}
		return r.update()
	case "add":
		if len(pos) != 1 {
			return errUsage
		}
		if isSkillDir(pos[0]) {
			return r.addPersonal(pos[0], *harness)
		}
		if *harness != "" {
			return fmt.Errorf("%s has no SKILL.md, so --harness does not apply", pos[0])
		}
		return r.addUpstream(pos[0], *name, *skillsPath)
	case "remove":
		if len(pos) != 1 {
			return errUsage
		}
		return r.remove(pos[0])
	}
	return errUsage
}

// interleave moves flags in front of positional args, because the flag
// package stops at the first positional arg.
func interleave(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if fs.Lookup(name) != nil && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(append(flags, "--"), pos...)
}

func repoRoot() (string, error) {
	if root := os.Getenv("AGENTS_HOME"); root != "" {
		return filepath.Abs(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents"), nil
}

func isSkillDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, "SKILL.md"))
	return err == nil && info.Mode().IsRegular()
}
