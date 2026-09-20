package worker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recorder struct {
	mu                  sync.Mutex
	calls               [][]string
	failStart, failStop bool
}

func (r *recorder) Run(ctx context.Context, name string, args ...string) error {
	if name == "git" {
		return (CommandExecutor{}).Run(ctx, name, args...)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	if args[0] == "start" && r.failStart || args[0] == "stop" && r.failStop {
		return errors.New("injected process failure")
	}
	return nil
}
func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
}
func fixture(t *testing.T) (Config, *recorder) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"repo", "runtime", "secrets", "auth"} {
		if err := os.Mkdir(filepath.Join(root, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	repo := filepath.Join(root, "repo")
	git(t, repo, "init")
	write(t, filepath.Join(repo, "README"), "fixture")
	git(t, repo, "add", "README")
	git(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "fixture")
	write(t, filepath.Join(root, "secrets/token"), "coordinator-secret")
	write(t, filepath.Join(root, "auth/auth.json"), `{"fake":true}`)
	write(t, filepath.Join(root, "adapter"), "")
	write(t, filepath.Join(root, "runner"), "")
	r := &recorder{}
	return Config{Repository: repo, RuntimeRoot: filepath.Join(root, "runtime"), SecretsRoot: filepath.Join(root, "secrets"), CodexHome: filepath.Join(root, "auth"), Adapter: filepath.Join(root, "adapter"), Runner: filepath.Join(root, "runner"), Executor: r}, r
}
func request() Request {
	return Request{Issue: 16, Role: "implementer", Branch: "ai/issue-16-fixture", ModelProfile: "strong", Pack: []byte("task")}
}
func mode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s: %o != %o", path, info.Mode().Perm(), want)
	}
}
func TestLifecycle(t *testing.T) {
	c, r := fixture(t)
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	h, err := m.Start(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{h.dir, h.lock, h.Home, c.SecretsRoot} {
		mode(t, d, 0700)
	}
	for _, p := range []string{h.Result, h.Pack, filepath.Join(h.dir, "job.json"), filepath.Join(h.Home, "auth.json"), filepath.Join(h.Home, "config.toml"), filepath.Join(c.SecretsRoot, "token")} {
		mode(t, p, 0600)
	}
	if err := ValidateClone(h.Clone); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h.Clone, "README"), "worker edit")
	original, _ := os.ReadFile(filepath.Join(c.Repository, "README"))
	if string(original) != "fixture" {
		t.Fatal("clone affected source")
	}
	args := strings.Join(r.calls[0], "\n")
	for _, required := range []string{"--unshare-net", "--unshare-pid", "--property=NoNewPrivileges=yes", "--tmpfs\n" + c.SecretsRoot, "--tmpfs\n" + c.CodexHome, "--tmpfs\n" + c.RuntimeRoot, "--bind\n" + h.Clone + "\n" + h.Clone, "--ro-bind\n" + h.Home + "/auth.json"} {
		if !strings.Contains(args, required) {
			t.Errorf("missing boundary %s", required)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(h.Home, "config.toml"))
	if !strings.Contains(string(cfg), "enabled = false") {
		t.Fatal("Codex network enabled")
	}
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("duplicate worker accepted")
	}
	r.failStop = true
	if err := h.Stop(context.Background()); err == nil {
		t.Fatal("stop failure ignored")
	}
	mode(t, h.Home, 0700)
	r.failStop = false
	if err := h.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{h.Home, h.lock} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("not cleaned: %s", p)
		}
	}
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("retained state overwritten")
	}
	mode(t, h.Result, 0600)
}
func TestRejectSharedMetadata(t *testing.T) {
	for _, kind := range []string{"commondir", "alternates", "gitfile", "symlink", "hardlink", "worktree-config", "include-config"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := fixture(t)
			root := c.Repository
			switch kind {
			case "commondir":
				write(t, filepath.Join(root, ".git/commondir"), "/outside")
			case "alternates":
				write(t, filepath.Join(root, ".git/objects/info/alternates"), "/outside")
			case "gitfile":
				os.Rename(filepath.Join(root, ".git"), filepath.Join(root, "metadata"))
				write(t, filepath.Join(root, ".git"), "gitdir: metadata")
			case "symlink":
				os.Symlink("/outside", filepath.Join(root, ".git/shared"))
			case "hardlink":
				if err := os.Link(filepath.Join(root, "README"), filepath.Join(root, ".git/shared")); err != nil {
					t.Fatal(err)
				}
			case "worktree-config":
				git(t, root, "config", "core.worktree", "/outside")
			case "include-config":
				git(t, root, "config", "include.path", "/outside")
			}
			if err := ValidateClone(root); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}
func TestConcurrentRepositories(t *testing.T) {
	c1, _ := fixture(t)
	c2, _ := fixture(t)
	c2.RuntimeRoot = c1.RuntimeRoot
	var wg sync.WaitGroup
	handles := make([]*Handle, 2)
	for i, c := range []Config{c1, c2} {
		wg.Add(1)
		go func(i int, c Config) {
			defer wg.Done()
			m, err := New(c)
			if err != nil {
				t.Error(err)
				return
			}
			h, err := m.Start(context.Background(), request())
			if err != nil {
				t.Error(err)
				return
			}
			handles[i] = h
		}(i, c)
	}
	wg.Wait()
	a, b := handles[0], handles[1]
	if a == nil || b == nil {
		t.Fatal("worker failed")
	}
	if a.Unit == b.Unit || a.Clone == b.Clone || a.lock == b.lock || a.Result == b.Result || a.Home == b.Home {
		t.Fatal("namespace collision")
	}
	write(t, a.Result, "a")
	write(t, b.Result, "b")
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mode(t, b.Home, 0700)
	data, _ := os.ReadFile(b.Result)
	if string(data) != "b" {
		t.Fatal("sibling changed")
	}
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestLaunchFailureAndPermissions(t *testing.T) {
	c, r := fixture(t)
	os.Chmod(filepath.Join(c.SecretsRoot, "token"), 0644)
	if _, err := New(c); err == nil {
		t.Fatal("public secret accepted")
	}
	os.Chmod(filepath.Join(c.SecretsRoot, "token"), 0600)
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r.failStart = true
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("launch failure ignored")
	}
	entries, err := os.ReadDir(filepath.Join(c.RuntimeRoot, m.namespace))
	if err != nil || len(entries) != 0 {
		t.Fatalf("rollback: %v %v", entries, err)
	}
}

type corruptClone struct {
	recorder
	started bool
}

func (r *corruptClone) Run(ctx context.Context, name string, args ...string) error {
	if name != "git" {
		r.started = true
		return nil
	}
	if err := (CommandExecutor{}).Run(ctx, name, args...); err != nil {
		return err
	}
	if args[0] == "clone" {
		return os.WriteFile(filepath.Join(args[len(args)-1], ".git/commondir"), []byte("/outside"), 0600)
	}
	return nil
}
func TestRejectBeforeLaunch(t *testing.T) {
	c, _ := fixture(t)
	r := &corruptClone{}
	c.Executor = r
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("corrupt clone accepted")
	}
	if r.started {
		t.Fatal("worker launched before metadata validation")
	}
}
func TestUncertainLaunchRetainsClaim(t *testing.T) {
	c, r := fixture(t)
	r.failStart = true
	r.failStop = true
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("failure ignored")
	}
	base := filepath.Join(c.RuntimeRoot, m.namespace)
	mode(t, filepath.Join(base, "issue-16-implementer.lock"), 0700)
	mode(t, filepath.Join(base, "issue-16-implementer/codex"), 0700)
	if _, err := m.Start(context.Background(), request()); err == nil {
		t.Fatal("uncertain unit duplicated")
	}
}

func TestReviewerCloneIsReadOnly(t *testing.T) {
	c, r := fixture(t)
	if err := os.Mkdir(filepath.Join(c.Repository, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(c.Repository, ".codex/config.toml"), "sandbox_mode = \"danger-full-access\"")
	git(t, c.Repository, "add", ".codex")
	git(t, c.Repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "project config")
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	req := request()
	req.Role = "reviewer"
	h, err := m.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(r.calls[0], "\n")
	if !strings.Contains(args, "--ro-bind\n"+h.Clone+"\n"+h.Clone) || strings.Contains(args, "--bind\n"+h.Clone+"\n"+h.Clone) {
		t.Fatal("reviewer has writable clone")
	}
	if !strings.Contains(args, "--tmpfs\n"+filepath.Join(h.Clone, ".codex")) {
		t.Fatal("project config not masked")
	}
	cfg, err := os.ReadFile(filepath.Join(h.Home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "\".\" = \"read\"") {
		t.Fatal("reviewer permission mismatch")
	}
	if err := h.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
