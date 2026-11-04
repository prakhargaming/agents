package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestFoldedDescriptionGivesFirstSentence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fold/SKILL.md"), `---
name: fold
description: >
  Use this when a
  thing happens at 3.5 times.
  Then do more!
---
Body.
`)
	var out bytes.Buffer
	if err := list(dir, []string{"fold"}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "Use this when a thing happens at 3.5 times.\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDescriptionsWrapToColumnsCappedAt96(t *testing.T) {
	dir := t.TempDir()
	words := strings.Repeat("abcd ", 40) // 200 bytes, 40 words.
	writeSkill(t, filepath.Join(dir, "a"), strings.TrimSpace(words))

	for _, tc := range []struct {
		columns string
		width   int
	}{
		{"30", 28},
		{"200", 94},
	} {
		t.Run(tc.columns, func(t *testing.T) {
			t.Setenv("COLUMNS", tc.columns)
			var out bytes.Buffer
			if err := list(dir, []string{"-d"}, &out); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if lines[0] != "a" {
				t.Fatalf("first line %q", lines[0])
			}
			longest := 0
			for _, l := range lines[1:] {
				longest = max(longest, len(strings.TrimPrefix(l, "  ")))
			}
			// Each word plus its space is 5 bytes, so the longest line is the
			// widest multiple of 5, minus 1, that fits.
			if want := (tc.width+1)/5*5 - 1; longest != want {
				t.Errorf("longest line %d, want %d:\n%s", longest, want, out.String())
			}
		})
	}
}

func TestListRejectsUnknownAndPathNames(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "a"), "A.")
	for arg, code := range map[string]int{"missing": 1, "../a": 2, "-x": 2} {
		err := list(dir, []string{arg}, &bytes.Buffer{})
		if e, ok := err.(exitError); !ok || e.code != code {
			t.Errorf("%s: got %v, want exit %d", arg, err, code)
		}
	}
}

func TestWrapCountsCharactersNotBytes(t *testing.T) {
	// "—" is 3 bytes but one character, so "a — b" fits in 5.
	if got := wrap("a — b", 5); len(got) != 1 {
		t.Errorf("got %q", got)
	}
}

func TestCollateMatchesLocaleOrder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only glibc locales skip punctuation")
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	names := []string{"in-your-own-words", "interrogate", "ponytail", "ponytail-audit"}

	collate(names, "")
	if want := []string{"interrogate", "in-your-own-words", "ponytail", "ponytail-audit"}; !slices.Equal(names, want) {
		t.Errorf("names: %v", names)
	}
	collate(names, "/SKILL.md")
	if want := []string{"interrogate", "in-your-own-words", "ponytail-audit", "ponytail"}; !slices.Equal(names, want) {
		t.Errorf("paths: %v", names)
	}

	t.Setenv("LC_ALL", "C")
	collate(names, "")
	if want := []string{"in-your-own-words", "interrogate", "ponytail", "ponytail-audit"}; !slices.Equal(names, want) {
		t.Errorf("C locale: %v", names)
	}
}
