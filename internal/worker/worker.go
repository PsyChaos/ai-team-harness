// Package worker manages isolated native workers. The coordinator owns dispatch
// authorization; this package never loads publication credentials or pushes Git.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Executor is the process boundary, replaceable by an offline launch recorder.
type Executor interface {
	Run(context.Context, string, ...string) error
}
type CommandExecutor struct{}

func (CommandExecutor) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Do not inherit Git routing variables or coordinator credentials.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}

type Config struct {
	Repository  string // canonical coordinator checkout, also the clone source
	RuntimeRoot string // dedicated external private directory, never inside a checkout
	SecretsRoot string // external private directory containing 0600 secret files
	CodexHome   string // external authentication source; never exposed to the worker
	Adapter     string // trusted worker-process adapter
	Runner      string // trusted run-provider-agent script
	Executor    Executor
}
type Request struct {
	Issue                      int
	Role, Branch, ModelProfile string
	Pack                       []byte
}
type Manager struct {
	config    Config
	namespace string
}
type Job struct {
	Unit   string `json:"unit"`
	Issue  int    `json:"issue"`
	Role   string `json:"role"`
	Clone  string `json:"clone"`
	Home   string `json:"provider_home"`
	Result string `json:"result"`
	Pack   string `json:"pack"`
}
type Handle struct {
	Job
	manager   *Manager
	dir, lock string
	mu        sync.Mutex
	closed    bool
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if real != abs {
		return "", fmt.Errorf("symlinked path: %s", path)
	}
	return real, nil
}
func inside(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}
func privateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("directory must have mode 0700: %s", path)
	}
	return nil
}
func privateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return fmt.Errorf("file must be regular with mode 0600: %s", path)
	}
	return nil
}
func New(c Config) (*Manager, error) {
	paths := []*string{&c.Repository, &c.RuntimeRoot, &c.SecretsRoot, &c.CodexHome, &c.Adapter, &c.Runner}
	for _, p := range paths {
		v, err := canonical(*p)
		if err != nil {
			return nil, err
		}
		*p = v
	}
	for _, p := range []string{c.RuntimeRoot, c.SecretsRoot, c.CodexHome} {
		if inside(p, c.Repository) || inside(c.Repository, p) {
			return nil, fmt.Errorf("private state must be outside repository: %s", p)
		}
		if err := privateDir(p); err != nil {
			return nil, err
		}
	}
	for _, p := range []string{c.SecretsRoot, c.CodexHome} {
		if inside(p, c.RuntimeRoot) || inside(c.RuntimeRoot, p) {
			return nil, errors.New("runtime and credential roots must be disjoint")
		}
	}
	if err := filepath.WalkDir(c.SecretsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return privateDir(p)
		}
		return privateFile(p)
	}); err != nil {
		return nil, err
	}
	if err := privateFile(filepath.Join(c.CodexHome, "auth.json")); err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(c.CodexHome, "auth.json")); err != nil || info.Size() > 1048576 {
		return nil, errors.New("authentication file is unavailable or too large")
	}
	if c.Executor == nil {
		c.Executor = CommandExecutor{}
	}
	sum := sha256.Sum256([]byte(c.Repository))
	return &Manager{config: c, namespace: hex.EncodeToString(sum[:16])}, nil
}

// ValidateClone rejects shared metadata, symlinks, object alternates and hard
// links, including unsafe metadata nested anywhere beneath .git.
func ValidateClone(root string) error {
	real, err := canonical(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(real, ".git"))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("clone requires its own .git directory")
	}
	err = filepath.WalkDir(filepath.Join(real, ".git"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked metadata: %s", path)
		}
		rel, _ := filepath.Rel(filepath.Join(real, ".git"), path)
		switch rel {
		case "commondir", "gitdir", "objects/info/alternates", "objects/info/http-alternates", "worktrees", "config.worktree":
			return fmt.Errorf("shared metadata: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe metadata: %s", path)
		}
		if linked(info) {
			return fmt.Errorf("hard-linked metadata: %s", path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "config", "--file", filepath.Join(real, ".git/config"), "--no-includes", "--null", "--list")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if (key == "core.bare" && value != "false") || key == "core.worktree" || strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") {
			return fmt.Errorf("unsafe Git configuration: %s", key)
		}
	}
	return nil
}

var branchPattern = regexp.MustCompile(`^ai/issue-([1-9][0-9]*)-[a-z0-9][a-z0-9-]{0,39}$`)

func (m *Manager) Start(ctx context.Context, r Request) (_ *Handle, err error) {
	match := branchPattern.FindStringSubmatch(r.Branch)
	if r.Issue <= 0 || match == nil || match[1] != strconv.Itoa(r.Issue) {
		return nil, errors.New("branch must match assigned issue")
	}
	switch r.Role {
	case "implementer", "reviewer", "security-reviewer":
	default:
		return nil, errors.New("invalid role")
	}
	switch r.ModelProfile {
	case "fast", "balanced", "strong":
	default:
		return nil, errors.New("invalid model profile")
	}
	if len(r.Pack) == 0 {
		return nil, errors.New("empty task pack")
	}
	base := filepath.Join(m.config.RuntimeRoot, m.namespace)
	if err = os.Mkdir(base, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if _, err = canonical(base); err != nil {
		return nil, err
	}
	if err = privateDir(base); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("issue-%d-%s", r.Issue, r.Role)
	lock := filepath.Join(base, key+".lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return nil, fmt.Errorf("worker already claimed or lock unavailable: %w", err)
	}
	h := &Handle{manager: m, lock: lock, dir: filepath.Join(base, key)}
	owned := false
	defer func() {
		if err != nil {
			if owned && h.dir != "" {
				if cleanupErr := os.RemoveAll(h.dir); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
					return // Preserve the claim if private data could not be removed.
				}
			}
			if h.lock != "" {
				err = errors.Join(err, os.Remove(h.lock))
			}
		}
	}()
	// Never replace an existing clone/result, even after an interrupted owner.
	if _, e := os.Lstat(h.dir); !os.IsNotExist(e) {
		return nil, errors.New("existing worker state requires recovery")
	}
	if err = os.Mkdir(h.dir, 0700); err != nil {
		return nil, err
	}
	owned = true
	h.Job = Job{Unit: "ai-harness-" + m.namespace + "-" + key, Issue: r.Issue, Role: r.Role, Clone: filepath.Join(h.dir, "clone"), Home: filepath.Join(h.dir, "codex"), Result: filepath.Join(h.dir, "result.md"), Pack: filepath.Join(h.dir, "pack.md")}
	run := m.config.Executor.Run
	if err = run(ctx, "git", "clone", "--no-local", "--", m.config.Repository, h.Clone); err != nil {
		return nil, err
	}
	if err = ValidateClone(h.Clone); err != nil {
		return nil, err
	}
	if err = run(ctx, "git", "-C", h.Clone, "checkout", "-B", r.Branch); err != nil {
		return nil, err
	}
	if info, e := os.Lstat(filepath.Join(h.Clone, ".codex")); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("unsafe project Codex configuration")
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	if err = os.Mkdir(h.Home, 0700); err != nil {
		return nil, err
	}
	auth, e := os.ReadFile(filepath.Join(m.config.CodexHome, "auth.json"))
	if e != nil {
		return nil, e
	}
	access := "read"
	if r.Role == "implementer" {
		access = "write"
	}
	cfg := fmt.Sprintf("approval_policy = \"never\"\ndefault_permissions = \"harness-worker\"\n[permissions.harness-worker.filesystem]\n\":minimal\" = \"read\"\n\":tmpdir\" = \"write\"\n\":slash_tmp\" = \"write\"\n%s = \"deny\"\n[permissions.harness-worker.filesystem.\":workspace_roots\"]\n\".\" = %q\n\".git\" = %q\n[permissions.harness-worker.network]\nenabled = false\n", strconv.Quote(h.Home), access, access)
	for path, data := range map[string][]byte{filepath.Join(h.Home, "auth.json"): auth, filepath.Join(h.Home, "config.toml"): []byte(cfg), h.Pack: r.Pack, h.Result: nil} {
		if err = os.WriteFile(path, data, 0600); err != nil {
			return nil, err
		}
	}
	job, _ := json.MarshalIndent(h.Job, "", "  ")
	if err = os.WriteFile(filepath.Join(h.dir, "job.json"), job, 0600); err != nil {
		return nil, err
	}
	if err = run(ctx, m.config.Adapter, append([]string{"start"}, m.launchArgs(h, r)...)...); err != nil {
		// A transport error may occur after systemd accepted the unit. Do not remove
		// mounts or release the claim unless termination is confirmed.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if stopErr := run(stopCtx, m.config.Adapter, "stop", h.Unit); stopErr != nil {
			err = errors.Join(err, stopErr)
			h.dir = ""
			h.lock = ""
		}
		return nil, err
	}
	return h, nil
}

// Stop terminates the unit before deleting credentials and releasing its claim.
// The clone, pack and result remain available for publication and review.
func (h *Handle) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if err := h.manager.config.Executor.Run(ctx, h.manager.config.Adapter, "stop", h.Unit); err != nil {
		return err
	}
	if err := os.RemoveAll(h.Home); err != nil {
		return err
	}
	if err := os.Remove(h.lock); err != nil {
		return err
	}
	h.closed = true
	return nil
}

func (m *Manager) launchArgs(h *Handle, r Request) []string {
	c := m.config
	args := []string{"--user", "--collect", "--unit=" + h.Unit, "--property=NoNewPrivileges=yes", "--property=WorkingDirectory=" + h.Clone, "--property=UnsetEnvironment=HARNESS_BOOTSTRAP_HMAC_KEY HARNESS_BROKER_HMAC_KEY TYPESAFE_API_KEY GH_TOKEN GITHUB_TOKEN DBUS_SESSION_BUS_ADDRESS XDG_RUNTIME_DIR"}
	env := map[string]string{"HARNESS_ROOT": h.Clone, "HARNESS_AGENT_ROLE": r.Role, "HARNESS_AGENT_PROVIDER": "codex", "HARNESS_AGENT_MODEL_PROFILE": r.ModelProfile, "HARNESS_AGENT_ISSUE": strconv.Itoa(r.Issue), "HARNESS_AGENT_PACK": h.Pack, "HARNESS_AGENT_RESULT": h.Result, "CODEX_HOME": h.Home, "CODEX_SQLITE_HOME": filepath.Join(h.Home, "sqlite")}
	for k, v := range env {
		args = append(args, "--setenv="+k+"="+v)
	}
	args = append(args, "/usr/bin/env", "-i", "PATH="+os.Getenv("PATH"), "HOME="+os.Getenv("HOME"))
	for k, v := range env {
		args = append(args, k+"="+v)
	}
	args = append(args, "bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run/user", "--tmpfs", c.RuntimeRoot, "--tmpfs", c.SecretsRoot, "--tmpfs", c.CodexHome)
	for _, p := range []string{filepath.Join(os.Getenv("HOME"), ".ssh"), filepath.Join(os.Getenv("HOME"), ".gnupg"), filepath.Join(os.Getenv("HOME"), ".config/gh"), filepath.Join(os.Getenv("HOME"), ".codex")} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--tmpfs", p)
		}
	}
	for _, p := range []string{filepath.Join(os.Getenv("HOME"), ".git-credentials"), filepath.Join(os.Getenv("HOME"), ".config/git/credentials")} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	bind := "--ro-bind"
	if r.Role == "implementer" {
		bind = "--bind"
	}
	args = append(args, bind, h.Clone, h.Clone)
	if _, err := os.Lstat(filepath.Join(h.Clone, ".codex")); err == nil {
		args = append(args, "--tmpfs", filepath.Join(h.Clone, ".codex"))
	}
	args = append(args, "--bind", h.Home, h.Home, "--ro-bind", filepath.Join(h.Home, "auth.json"), filepath.Join(h.Home, "auth.json"), "--ro-bind", filepath.Join(h.Home, "config.toml"), filepath.Join(h.Home, "config.toml"), "--bind", h.Result, h.Result, "--ro-bind", h.Pack, h.Pack, "--ro-bind", c.Runner, c.Runner, "--unshare-net", "--unshare-pid", "--unshare-uts", "--unshare-ipc", "--proc", "/proc", "--new-session", "--die-with-parent", c.Runner)
	return args
}
