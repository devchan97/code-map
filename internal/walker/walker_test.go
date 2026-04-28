package walker

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, root string, opts Options) []Entry {
	t.Helper()
	var got []Entry
	err := Walk(context.Background(), root, opts, func(e Entry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return got
}

func TestWalk_BasicEnumerate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	writeFile(t, root, "sub/b.py", "x = 1\n")
	got := collect(t, root, Options{})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d (%v)", len(got), got)
	}
	paths := []string{got[0].Path, got[1].Path}
	sort.Strings(paths)
	if paths[0] != "a.go" || paths[1] != "sub/b.py" {
		t.Errorf("paths = %v", paths)
	}
	for _, e := range got {
		if e.SHA1 == "" || len(e.SHA1) != 40 {
			t.Errorf("bad SHA1 %q for %s", e.SHA1, e.Path)
		}
	}
}

func TestWalk_DefaultExcludeDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, ".git/config", "secret\n")
	writeFile(t, root, "node_modules/foo/index.js", "console.log(1)\n")
	writeFile(t, root, "__pycache__/x.pyc", "junk\n")
	writeFile(t, root, "dist/out.bin", "junk\n")
	got := collect(t, root, Options{})
	if len(got) != 1 || got[0].Path != "main.go" {
		t.Errorf("expected only main.go; got %v", got)
	}
}

func TestWalk_SkipsSecretFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "code.go", "package x\n")
	writeFile(t, root, ".env", "API=secret\n")
	writeFile(t, root, "key.pem", "stuff\n")
	got := collect(t, root, Options{})
	if len(got) != 1 || got[0].Path != "code.go" {
		t.Errorf("expected only code.go; got %v", got)
	}
}

func TestWalk_SkipsLargeFiles(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 200)
	writeFile(t, root, "big.txt", string(big))
	writeFile(t, root, "small.txt", "ok")
	got := collect(t, root, Options{MaxFileBytes: 100})
	for _, e := range got {
		if e.Path == "big.txt" {
			t.Errorf("big.txt should be skipped (size 200 > limit 100)")
		}
	}
	found := false
	for _, e := range got {
		if e.Path == "small.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("small.txt should be present; got %v", got)
	}
}

func TestWalk_SkipsContentSecrets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ok.go", "package x\n")
	writeFile(t, root, "leak.txt", "AWS key: AKIAIOSFODNN7EXAMPLE here\n")
	got := collect(t, root, Options{})
	for _, e := range got {
		if e.Path == "leak.txt" {
			t.Errorf("leak.txt with AWS key should be skipped")
		}
	}
}

func TestWalk_GitIgnore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "ignored.txt\n*.log\n")
	writeFile(t, root, "ignored.txt", "x")
	writeFile(t, root, "kept.txt", "x")
	writeFile(t, root, "app.log", "x")
	got := collect(t, root, Options{})
	for _, e := range got {
		if e.Path == "ignored.txt" || e.Path == "app.log" {
			t.Errorf("expected %s ignored; got %v", e.Path, got)
		}
	}
}

func TestWalk_Lang(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.py", "x = 1\n")
	writeFile(t, root, "b.go", "package x\n")
	writeFile(t, root, "c.mjs", "export {}\n")
	writeFile(t, root, "d.cjs", "module.exports = {}\n")
	writeFile(t, root, "e.txt", "hi\n")
	got := collect(t, root, Options{})
	want := map[string]string{
		"a.py":  "python",
		"b.go":  "go",
		"c.mjs": "ts",
		"d.cjs": "ts",
		"e.txt": "",
	}
	for _, e := range got {
		if w, ok := want[e.Path]; ok && e.Lang != w {
			t.Errorf("Lang(%s) = %q; want %q", e.Path, e.Lang, w)
		}
	}
}

func TestWalk_CtxCancelled(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, root, filepath.Join("dir", "f"+string(rune('a'+i%26))+".go"), "package x\n")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	err := Walk(ctx, root, Options{}, func(Entry) error { return nil })
	if err == nil {
		t.Error("Walk with cancelled ctx should return an error")
	}
}
