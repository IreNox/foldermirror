package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveRules(t *testing.T) {
	rules := []Rule{{Path: "", Include: true}, {Path: "private", Include: false}, {Path: "private/share", Include: true}}
	cases := map[string]bool{"": true, "music": true, "private": false, "private/notes": false, "private/share": true, "private/share/file": true}
	for path, want := range cases {
		if got := effective(rules, path); got != want {
			t.Errorf("effective(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPlanApplyAndSafeStaleRemoval(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "keep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "skip"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep", "a.txt"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skip", "b.txt"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	s := &server{source: source, target: target, statePath: filepath.Join(target, ".foldermirror.json"), state: State{Version: 1, Rules: []Rule{{Path: "keep", Include: true}}, Files: map[string]FileRecord{}}}
	p, _, err := s.makePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Create) != 1 || p.Create[0].Path != "keep/a.txt" {
		t.Fatalf("unexpected create plan: %#v", p.Create)
	}
	src, dst, _ := s.paths("keep/a.txt")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, dst); err != nil {
		t.Fatal(err)
	}
	id, err := fileIdentity(dst)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Files["keep/a.txt"] = FileRecord{Identity: id}
	si, _ := os.Stat(src)
	di, _ := os.Stat(dst)
	if !os.SameFile(si, di) {
		t.Fatal("destination is not a hardlink")
	}
	s.state.Rules = nil
	p, _, err = s.makePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Remove) != 1 || p.Remove[0].Path != "keep/a.txt" {
		t.Fatalf("unexpected remove plan: %#v", p.Remove)
	}
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	p, _, err = s.makePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Remove) != 0 || len(p.Skip) != 1 {
		t.Fatalf("replacement should be protected: %#v", p)
	}
}

func TestNormalizeRulesRejectsEscape(t *testing.T) {
	if _, err := normalizeRules([]Rule{{Path: "../outside", Include: true}}); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestConfiguredRootsAreUsed(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "chosen-source")
	target := filepath.Join(root, "chosen-target")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	s := &server{source: source, target: target}
	src, dst, err := s.paths("folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if src != filepath.Join(source, "folder", "file.txt") || dst != filepath.Join(target, "folder", "file.txt") {
		t.Fatalf("configured roots not used: source=%q target=%q", src, dst)
	}
}

func TestEmptyPlanEncodesArrays(t *testing.T) {
	b, err := json.Marshal(newPlan())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"create":[],"remove":[],"skip":[]}`; got != want {
		t.Fatalf("empty plan JSON = %s, want %s", got, want)
	}
}
