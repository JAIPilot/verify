package verify

import (
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "verify@example.invalid"}, {"config", "user.name", "Verify test"}} {
		if _, err := git(root, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 1;\n")
	mustWrite(t, root, "existing.test.cjs", "const {test}=require('node:test'); const assert=require('node:assert/strict'); test('existing suite',()=>assert.equal(typeof require('./calc.cjs'),'function'));\n")
	if _, err := git(root, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, nil, "commit", "-m", "baseline"); err != nil {
		t.Fatal(err)
	}
	return root
}

func mustWrite(t *testing.T, root, name, content string) {
	t.Helper()
	if err := writeBytes(filepath.Join(root, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyManagedCommandsAreAvailable(t *testing.T) {
	if code := Main([]string{"start"}); code != 2 {
		t.Fatalf("removed command returned %d", code)
	}
	if code := Main([]string{"--agent", "legacy"}); code != 2 {
		t.Fatalf("removed option returned %d", code)
	}
	if code := Main([]string{"--help"}); code != 0 {
		t.Fatalf("help returned %d", code)
	}
	if code := Main([]string{"--version"}); code != 0 {
		t.Fatalf("version returned %d", code)
	}
}
