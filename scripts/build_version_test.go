package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var scriptPath = func() string {
	p, err := filepath.Abs("build-version.sh")
	if err != nil {
		panic(err)
	}
	return p
}()

func baseEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{}
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		switch {
		case key == "HOME" || key == "USERPROFILE":
		case strings.HasPrefix(key, "GIT_"):
		case strings.HasPrefix(key, "GITHUB_REF_"):
		case strings.HasPrefix(key, "BUILD_REF_"):
		default:
			env = append(env, kv)
		}
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"GIT_AUTHOR_NAME=Version Test",
		"GIT_AUTHOR_EMAIL=version-test@example.invalid",
		"GIT_COMMITTER_NAME=Version Test",
		"GIT_COMMITTER_EMAIL=version-test@example.invalid",
	)
	return env
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = baseEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	return dir
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-m", "add "+name)
}

func runVersion(t *testing.T, dir string, extraEnv ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(baseEnv(t), extraEnv...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return strings.TrimSpace(string(out)), ee.ExitCode()
		}
		t.Fatalf("failed to run script: %v", err)
	}
	return strings.TrimSpace(string(out)), 0
}

func expectVersion(t *testing.T, dir string, want string, extraEnv ...string) {
	t.Helper()
	got, code := runVersion(t, dir, extraEnv...)
	if code != 0 {
		t.Fatalf("script exited %d, want version %q", code, want)
	}
	if got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
}

func TestBranchSnapshotIsNextMinor(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.5.0")
	expectVersion(t, dir, "v0.6.0-SNAPSHOT")

	commitFile(t, dir, "b.txt", "b")
	expectVersion(t, dir, "v0.6.0-SNAPSHOT")
}

func TestBranchAtNewStableTagYieldsFollowingMinor(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.5.0")
	commitFile(t, dir, "b.txt", "b")
	git(t, dir, "tag", "v0.6.0")
	expectVersion(t, dir, "v0.7.0-SNAPSHOT")
}

func TestDetachedCleanTagResolvesRelease(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.6.0")
	git(t, dir, "checkout", "--detach", "v0.6.0")
	expectVersion(t, dir, "v0.6.0")
}

func TestExplicitTagContextOnNamedBranch(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.6.0")
	expectVersion(t, dir, "v0.6.0", "GITHUB_REF_TYPE=tag", "GITHUB_REF_NAME=v0.6.0")
}

func TestGitHubBranchContextDetachedAtTag(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.6.0")
	git(t, dir, "checkout", "--detach", "v0.6.0")
	expectVersion(t, dir, "v0.7.0-SNAPSHOT", "GITHUB_REF_TYPE=branch", "GITHUB_REF_NAME=main")
}

func TestDirtyExactTagFallsBackToSnapshot(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.6.0")
	git(t, dir, "checkout", "--detach", "v0.6.0")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectVersion(t, dir, "v0.7.0-SNAPSHOT")

	git(t, dir, "checkout", "--", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectVersion(t, dir, "v0.7.0-SNAPSHOT")
}

func TestAnnotatedTagDecimalParse(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "-a", "-m", "release", "v1.9.4")
	expectVersion(t, dir, "v1.10.0-SNAPSHOT")
}

func TestUnreachableTagIgnored(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.5.0")
	git(t, dir, "checkout", "--orphan", "side")
	git(t, dir, "rm", "-rf", ".")
	commitFile(t, dir, "s.txt", "s")
	git(t, dir, "tag", "v9.0.0")
	git(t, dir, "checkout", "main")
	expectVersion(t, dir, "v0.6.0-SNAPSHOT")
}

func TestNonStableTagsIgnoredAsBase(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.5.0")
	git(t, dir, "tag", "deploy-2026")
	git(t, dir, "tag", "v0.6.0-rc.1")
	expectVersion(t, dir, "v0.6.0-SNAPSHOT")

	git(t, dir, "checkout", "--detach", "v0.6.0-rc.1")
	expectVersion(t, dir, "v0.6.0-rc.1")
}

func TestNoStableTags(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	expectVersion(t, dir, "v0.1.0-SNAPSHOT")
}

func TestEmptyRepoAndNonRepo(t *testing.T) {
	expectVersion(t, newRepo(t), "dev-SNAPSHOT")
	expectVersion(t, t.TempDir(), "dev-SNAPSHOT")
}

func TestExplicitTagErrors(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.5.0")
	other := newRepo(t)
	commitFile(t, other, "o.txt", "o")
	git(t, other, "tag", "v1.2.3")

	cases := [][]string{
		{"BUILD_REF_TYPE=tag", "BUILD_REF_NAME=not-a-tag"},
		{"BUILD_REF_TYPE=tag", "BUILD_REF_NAME=v9.9.9"},
		{"BUILD_REF_TYPE=pull", "BUILD_REF_NAME=1"},
	}
	for _, env := range cases {
		got, code := runVersion(t, dir, env...)
		if code == 0 {
			t.Fatalf("env %v: expected failure, got %q", env, got)
		}
		if got != "" {
			t.Fatalf("env %v: unexpected stdout %q", env, got)
		}
	}

	commitFile(t, dir, "b.txt", "b")
	got, code := runVersion(t, dir, "BUILD_REF_TYPE=tag", "BUILD_REF_NAME=v0.5.0")
	if code == 0 || got != "" {
		t.Fatalf("tag resolving other commit: code=%d out=%q", code, got)
	}
}

func TestBuildRefOverridesAmbientGitHub(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a.txt", "a")
	git(t, dir, "tag", "v0.6.0")
	expectVersion(t, dir, "v0.6.0",
		"GITHUB_REF_TYPE=branch", "GITHUB_REF_NAME=main",
		"BUILD_REF_TYPE=tag", "BUILD_REF_NAME=v0.6.0")
}
