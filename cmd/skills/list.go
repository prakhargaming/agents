package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"gopkg.in/yaml.v3"
)

const listUsage = "Usage: skills [-f|--full] [-d|--descriptions|name]"

// list browses the shared skill index. It keeps the output of the zsh
// `skills` function that it replaces.
func list(dir string, args []string, out io.Writer) error {
	full := false
	if n := len(args); n > 0 && (args[0] == "-f" || args[0] == "--full") {
		full, args = true, args[1:]
	} else if n > 0 && (args[n-1] == "-f" || args[n-1] == "--full") {
		full, args = true, args[:n-1]
	}
	if full && len(args) == 0 {
		args = []string{"-d"}
	}

	switch strings.Join(args, " ") {
	case "":
		names, err := skillNames(dir, false)
		if err != nil {
			return err
		}
		collate(names, "")
		for _, n := range names {
			fmt.Fprintln(out, n)
		}
		return nil
	case "-d", "--descriptions":
		names, err := skillNames(dir, true)
		if err != nil {
			return err
		}
		if full {
			// zsh sorts the glob by the whole path, so ponytail-audit comes
			// before ponytail.
			collate(names, "/SKILL.md")
			for _, n := range names {
				if err := catFile(filepath.Join(dir, n, "SKILL.md"), out); err != nil {
					return err
				}
			}
			return nil
		}
		width := min(terminalColumns(), 96) - 2
		bold := isTTY(os.Stdout) && !hasEnv("NO_COLOR")
		for i, n := range names {
			sentence, err := skillSentence(filepath.Join(dir, n, "SKILL.md"))
			if err != nil {
				return err
			}
			if i > 0 {
				fmt.Fprintln(out)
			}
			if bold {
				fmt.Fprintf(out, "\x1b[1m%s\x1b[0m\n", n)
			} else {
				fmt.Fprintln(out, n)
			}
			for _, line := range wrap(sentence, width) {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
		return nil
	}

	if len(args) != 1 || strings.HasPrefix(args[0], "-") || strings.Contains(args[0], "/") {
		return exitError{2, listUsage}
	}
	file := filepath.Join(dir, args[0], "SKILL.md")
	if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
		return exitError{1, "Unknown skill: " + args[0]}
	}
	if full {
		return catFile(file, out)
	}
	sentence, err := skillSentence(file)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, sentence)
	return nil
}

// skillNames lists the entries of dir in byte order. With withSkill, it
// keeps only the entries that have a SKILL.md.
func skillNames(dir string, withSkill bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if withSkill && !isSkillDir(filepath.Join(dir, e.Name())) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// collate sorts names the way ls and zsh globs do. glibc locales other than
// C compare letters and digits without case first and skip punctuation, so
// "interrogate" sorts before "in-your-own-words". macOS sorts by bytes.
func collate(names []string, suffix string) {
	if runtime.GOOS != "linux" {
		sort.Strings(names)
		return
	}
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LC_COLLATE")
	}
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	if locale == "" || locale == "C" || locale == "POSIX" || strings.HasPrefix(locale, "C.") {
		sort.Strings(names)
		return
	}
	key := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, s)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if ki, kj := key(names[i]+suffix), key(names[j]+suffix); ki != kj {
			return ki < kj
		}
		return names[i] < names[j]
	})
}

func catFile(path string, out io.Writer) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}

var (
	frontmatterFence = regexp.MustCompile(`(?m)^---\s*$`)
	sentenceEnd      = regexp.MustCompile(`[.!?]\s`)
)

// skillSentence returns the first sentence of the frontmatter description.
func skillSentence(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	desc, err := description(b)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return firstSentence(desc), nil
}

func description(skillMD []byte) (string, error) {
	parts := frontmatterFence.Split(string(skillMD), 3)
	if len(parts) < 2 {
		return "", nil
	}
	var header map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &header); err != nil {
		return "", err
	}
	s, _ := header["description"].(string)
	return strings.Join(strings.Fields(s), " "), nil
}

func firstSentence(s string) string {
	if loc := sentenceEnd.FindStringIndex(s); loc != nil {
		return s[:loc[0]+1]
	}
	return s
}

// wrap splits text into lines of at most width characters. A word longer than
// width gets its own line.
func wrap(text string, width int) []string {
	width = max(width, 1)
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > width {
			lines = append(lines, line)
			line = ""
		}
		if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// terminalColumns reads $COLUMNS first. zsh does not export COLUMNS, so it
// then asks the terminal, and it falls back to 80.
func terminalColumns() int {
	if v, ok := os.LookupEnv("COLUMNS"); ok {
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	var ws struct{ rows, cols, x, y uint16 }
	for _, f := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
		if errno == 0 && ws.cols > 0 {
			return int(ws.cols)
		}
	}
	return 80
}

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func hasEnv(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}
