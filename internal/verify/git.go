package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type fileState struct {
	Hash string
	Mode os.FileMode
}
type tree map[string]fileState
type workspace struct {
	Root, Base, Baseline, Candidate, RunDir string
	Original                                tree
	Known                                   map[string]bool
	BaselineScope, FinalScope               []string
}

func git(root string, input []byte, args ...string) ([]byte, error) {
	c := exec.Command("git", append([]string{"-c", "core.quotePath=false"}, args...)...)
	c.Dir = root
	c.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func gitText(root string, args ...string) (string, error) {
	b, e := git(root, nil, args...)
	return strings.TrimSpace(string(b)), e
}
func paths(b []byte) []string {
	out := []string{}
	for _, s := range strings.Split(string(b), "\x00") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func testPath(p string) bool {
	p = strings.ToLower(filepath.ToSlash(p))
	b := filepath.Base(p)
	return strings.Contains("/"+p, "/test/") || strings.Contains("/"+p, "/tests/") || strings.Contains("/"+p, "/__tests__/") || strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.go") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") || strings.HasSuffix(b, "test.java") || strings.HasSuffix(b, "tests.java") || strings.HasSuffix(b, "test.kt") || strings.HasSuffix(b, "_test.py") || strings.HasSuffix(b, ".snap")
}
func sourcePath(p string) bool {
	if testPath(p) {
		return false
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go", ".java", ".kt", ".kts", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".py", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".swift", ".scala":
		return true
	}
	return false
}
func relevant(p string) bool {
	if sourcePath(p) || testPath(p) {
		return true
	}
	b := filepath.Base(p)
	switch b {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties", "Cargo.toml", "Cargo.lock", "pyproject.toml", "requirements.txt", "Makefile", "tsconfig.json":
		return true
	}
	return false
}
func safePath(root, p string) (string, error) {
	if p == "" || filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", errors.New("path must be relative to the workspace")
	}
	p = filepath.FromSlash(p)
	clean := filepath.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace")
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if strings.EqualFold(part, ".git") {
			return "", errors.New("Git internals are inaccessible")
		}
	}
	// Reject symlinks in every existing component, including the final file.
	curr := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		curr = filepath.Join(curr, part)
		info, err := os.Lstat(curr)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink paths are unsupported")
		}
	}
	return filepath.Join(root, clean), nil
}
func snapshot(root string, known map[string]bool) (tree, error) {
	tracked, err := git(root, nil, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	untracked, err := git(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	names := paths(tracked)
	for _, p := range paths(untracked) {
		if known[p] || relevant(p) {
			names = append(names, p)
		}
	}
	out := tree{}
	for _, p := range unique(names) {
		full, err := safePath(root, p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		f, err := os.Open(full)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return nil, fmt.Errorf("unsupported file %s", p)
		}
		hash := sha256.New()
		_, err = io.Copy(hash, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		out[p] = fileState{hex.EncodeToString(hash.Sum(nil)), info.Mode().Perm()}
	}
	return out, nil
}
func fingerprint(t tree) string {
	names := make([]string, 0, len(t))
	for p := range t {
		names = append(names, p)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, p := range names {
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", p, t[p].Hash, t[p].Mode)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func changed(a, b tree) []string {
	out := []string{}
	for p, v := range a {
		if b[p] != v {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
func prepare(root, base string) (*workspace, error) {
	root, err := gitText(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if base == "" {
		for _, ref := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/heads/main", "refs/remotes/origin/master", "refs/heads/master"} {
			if _, e := gitText(root, "rev-parse", "--verify", ref+"^{commit}"); e == nil {
				base = ref
				break
			}
		}
		if base == "" {
			return nil, errors.New("cannot find main/master; supply --base <ref>")
		}
	}
	ref, err := gitText(root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return nil, err
	}
	base, err = gitText(root, "merge-base", "HEAD", ref)
	if err != nil {
		return nil, err
	}
	diff, err := git(root, nil, "diff", "--binary", "--no-ext-diff", "--no-renames", base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := git(root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	if len(diff) == 0 && len(untracked) == 0 {
		return nil, errors.New("no Git changes to verify")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cache, "jaipilot", "verify", uuid())
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	w := &workspace{Root: root, Base: base, RunDir: dir, Baseline: filepath.Join(dir, "baseline"), Candidate: filepath.Join(dir, "candidate"), Known: map[string]bool{}}
	for _, p := range paths(untracked) {
		w.Known[p] = true
	}
	w.Original, err = snapshot(root, w.Known)
	if err != nil {
		return nil, err
	}
	for _, dest := range []string{w.Baseline, w.Candidate} {
		if _, err = git(root, nil, "worktree", "add", "--detach", dest, base); err != nil {
			return nil, err
		}
	}
	if len(diff) > 0 {
		if _, err = git(w.Candidate, diff, "apply", "--binary", "--whitespace=nowarn"); err != nil {
			return nil, err
		}
	}
	for _, p := range paths(untracked) {
		source, e := safePath(root, p)
		if e != nil {
			return nil, e
		}
		data, e := os.ReadFile(source)
		if e != nil {
			return nil, e
		}
		dest, e := safePath(w.Candidate, p)
		if e != nil {
			return nil, e
		}
		info, e := os.Stat(source)
		if e != nil {
			return nil, e
		}
		if e = writeBytes(dest, data, info.Mode().Perm()); e != nil {
			return nil, e
		}
	}
	old, err := snapshot(w.Baseline, w.Known)
	if err != nil {
		return nil, err
	}
	candidate, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return nil, err
	}
	for _, p := range changed(old, candidate) {
		if sourcePath(p) {
			if _, ok := old[p]; ok {
				w.BaselineScope = append(w.BaselineScope, p)
			}
			if _, ok := candidate[p]; ok {
				w.FinalScope = append(w.FinalScope, p)
			}
		}
	}
	now, err := snapshot(root, w.Known)
	if err != nil {
		return nil, err
	}
	if fingerprint(now) != fingerprint(w.Original) {
		return nil, errors.New("checkout changed while creating verification snapshot; retry")
	}
	return w, nil
}
func writeBytes(p string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".verify-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, p)
}
func (w *workspace) diff() (string, error) {
	b, err := git(w.Candidate, nil, "diff", "--no-ext-diff", "--no-renames", w.Base, "--")
	if err != nil {
		return "", err
	}
	current, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.Write(b)
	tracked, err := git(w.Candidate, nil, "ls-files", "-z")
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, p := range paths(tracked) {
		known[p] = true
	}
	names := []string{}
	for p := range current {
		if !known[p] {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	for _, p := range names {
		data, e := os.ReadFile(filepath.Join(w.Candidate, p))
		if e != nil {
			return "", e
		}
		fmt.Fprintf(&out, "\n--- /dev/null\n+++ b/%s\n", p)
		if bytes.IndexByte(data, 0) >= 0 {
			fmt.Fprintf(&out, "Binary file added (%d bytes); inspect the local file directly.\n", len(data))
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			out.WriteByte('+')
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func (w *workspace) reviewDiff(stage, full string) (string, error) {
	path := filepath.Join(w.RunDir, stage+".diff")
	if err := writeBytes(path, []byte(full), 0600); err != nil {
		return "", err
	}
	if len(full) <= 48<<10 {
		return full, nil
	}
	status, err := gitText(w.Candidate, "status", "--short", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if len(status) > 48<<10 {
		status = status[:48<<10] + "\n… additional paths are in the saved diff"
	}
	return fmt.Sprintf("%d-byte diff saved to %s\nReview that file before accepting. Changed paths:\n%s", len(full), path, status), nil
}
func (w *workspace) promote() error {
	now, err := snapshot(w.Root, w.Known)
	if err != nil {
		return err
	}
	if fingerprint(now) != fingerprint(w.Original) {
		return errors.New("original checkout changed during verification; verified worktrees retained, no changes applied")
	}
	candidate, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return err
	}
	names := changed(w.Original, candidate)
	type backup struct {
		path   string
		data   []byte
		mode   os.FileMode
		exists bool
	}
	backups := []backup{}
	rollback := func() {
		for i := len(backups) - 1; i >= 0; i-- {
			b := backups[i]
			if b.exists {
				_ = writeBytes(b.path, b.data, b.mode)
			} else {
				_ = os.Remove(b.path)
			}
		}
	}
	for _, p := range names {
		dest, e := safePath(w.Root, p)
		if e != nil {
			rollback()
			return e
		}
		b := backup{path: dest}
		if v, ok := w.Original[p]; ok {
			b.exists = true
			b.mode = v.Mode
			b.data, e = os.ReadFile(dest)
			if e != nil {
				rollback()
				return e
			}
		}
		backups = append(backups, b)
		if v, ok := candidate[p]; ok {
			var data []byte
			data, e = os.ReadFile(filepath.Join(w.Candidate, p))
			if e == nil {
				e = writeBytes(dest, data, v.Mode)
			}
		} else {
			e = os.Remove(dest)
		}
		if e != nil {
			rollback()
			return e
		}
	}
	return nil
}
