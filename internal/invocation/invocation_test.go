package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PsyChaos/ai-team-harness/internal/routing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeCLI crosses a real process boundary, with no vendor CLI, credentials,
// network, or shell interpolation of arguments. It records argv and stdin.
type fakeCLI struct {
	path, output string
	calls        int
}

func (f *fakeCLI) Run(ctx context.Context, command Command, prompt io.Reader) error {
	f.calls++
	cmd := exec.CommandContext(ctx, f.path, append([]string{command.Executable}, command.Args...)...)
	cmd.Stdin = prompt
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	f.output = string(out)
	return err
}

func TestSupportedInvocations(t *testing.T) {
	var capabilities struct {
		CodexConfigKey string `json:"codex_config_key"`
		Supported      []struct {
			Provider, Version, Model string
			Efforts                  []string
		}
	}
	if err := json.Unmarshal(fixture(t, "capabilities.json"), &capabilities); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\nprintf 'PROMPT\\n'\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, c := range capabilities.Supported {
		for _, effort := range c.Efforts {
			t.Run(c.Provider+"/"+c.Model+"/"+effort, func(t *testing.T) {
				tuple := routing.Tuple{WorkID: "issue-10", Provider: c.Provider, Model: c.Model, Effort: effort, Profile: "strong"}
				fake := &fakeCLI{path: script}
				prompt := "task text $(touch never-run); --model malicious\n"
				r, err := Invoke(context.Background(), tuple, c.Version, strings.NewReader(prompt), fake)
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				var help string
				if c.Provider == "codex" {
					want = []string{"exec", "--ephemeral", "--strict-config", "--model", c.Model, "--config", capabilities.CodexConfigKey + "=\"" + effort + "\"", "-"}
					help = string(fixture(t, "codex-exec-help.txt"))
				} else {
					want = []string{"--print", "--no-session-persistence", "--output-format", "text", "--model", c.Model, "--effort", effort}
					help = string(fixture(t, "claude-help.txt"))
				}
				if !reflect.DeepEqual(r.Command.Args, want) {
					t.Fatalf("args: %q want %q", r.Command.Args, want)
				}
				for _, arg := range want {
					if strings.HasPrefix(arg, "--") && !strings.Contains(help, arg) {
						t.Fatalf("flag %s absent from recorded help", arg)
					}
				}
				expectedOutput := strings.Join(append([]string{c.Provider}, want...), "\n") + "\nPROMPT\n" + prompt
				if fake.output != expectedOutput || fake.calls != 1 {
					t.Fatalf("process received %q", fake.output)
				}
				if r.Requested != tuple || r.Effective == nil || *r.Effective != (Settings{c.Provider, c.Model, effort}) || r.EffectiveSource != "command_arguments" || r.Status != "completed" || !r.ExecutionAttempted {
					t.Fatalf("result: %+v", r)
				}
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				var roundTrip Result
				if err := json.Unmarshal(b, &roundTrip); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(r, roundTrip) {
					t.Fatalf("record loses settings: %s", b)
				}
			})
		}
	}
}

func TestUnsupportedFixtureNeverExecutes(t *testing.T) {
	var f struct {
		Version   string
		Requested routing.Tuple
		Reason    string
	}
	if err := json.Unmarshal(fixture(t, "unsupported.json"), &f); err != nil {
		t.Fatal(err)
	}
	executor := &fakeCLI{}
	r, err := Invoke(context.Background(), f.Requested, f.Version, strings.NewReader("task"), executor)
	if err == nil || r.Reason != f.Reason || r.Requested != f.Requested || r.Effective != nil || r.Command != nil || r.Status != "rejected" || r.ExecutionAttempted || executor.calls != 0 {
		t.Fatalf("rejection: %+v, error %v, calls %d", r, err, executor.calls)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"effective":null`) || !strings.Contains(string(b), `"reason":`) {
		t.Fatalf("incomplete rejection record: %s", b)
	}
}

func TestFailClosed(t *testing.T) {
	base := routing.Tuple{Provider: "codex", Model: "gpt-5.4", Effort: "high"}
	for _, tc := range []struct {
		name, version string
		edit          func(*routing.Tuple)
	}{
		{"version", "0.153.0", func(*routing.Tuple) {}},
		{"claude-version", "2.1.84", func(t *routing.Tuple) { t.Provider = "claude"; t.Model = "claude-opus-4-6" }},
		{"provider", "0.154.0", func(t *routing.Tuple) { t.Provider = "gemini" }},
		{"unknown-model", "0.154.0", func(t *routing.Tuple) { t.Model = "gpt-future" }},
		{"default-model", "0.154.0", func(t *routing.Tuple) { t.Model = "CLI default" }},
		{"alias", "2.1.235", func(t *routing.Tuple) { t.Provider = "claude"; t.Model = "opus" }},
		{"empty-effort", "0.154.0", func(t *routing.Tuple) { t.Effort = "" }},
		{"effort-injection", "0.154.0", func(t *routing.Tuple) { t.Effort = "high\"\nmodel=\"other" }},
		{"model-injection", "0.154.0", func(t *routing.Tuple) { t.Model = "--help" }},
		{"codex-max", "0.154.0", func(t *routing.Tuple) { t.Effort = "max" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tuple := base
			tc.edit(&tuple)
			executor := &fakeCLI{}
			r, err := Invoke(context.Background(), tuple, tc.version, nil, executor)
			if err == nil || r.Reason == "" || r.Requested != tuple || r.Command != nil || r.Effective != nil || executor.calls != 0 {
				t.Fatalf("not rejected: %+v %v", r, err)
			}
		})
	}
}

type executorFunc func(context.Context, Command, io.Reader) error

func (f executorFunc) Run(ctx context.Context, c Command, p io.Reader) error { return f(ctx, c, p) }

func TestExecutionFailures(t *testing.T) {
	tuple := routing.Tuple{Provider: "codex", Model: "gpt-5.4", Effort: "high"}
	failure := errors.New("fake CLI exited 7")
	r, err := Invoke(context.Background(), tuple, "0.154.0", nil, executorFunc(func(_ context.Context, c Command, _ io.Reader) error {
		c.Args[0] = "mutated"
		return failure
	}))
	if !errors.Is(err, failure) || r.Status != "execution_failed" || r.Reason != failure.Error() || !r.ExecutionAttempted || r.Command.Args[0] != "exec" || r.Effective == nil {
		t.Fatalf("failure record: %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := &fakeCLI{}
	r, err = Invoke(ctx, tuple, "0.154.0", nil, executor)
	if !errors.Is(err, context.Canceled) || r.ExecutionAttempted || executor.calls != 0 {
		t.Fatalf("cancellation: %+v %v", r, err)
	}
	r, err = Invoke(context.Background(), tuple, "0.154.0", nil, nil)
	if err == nil || r.ExecutionAttempted {
		t.Fatalf("nil executor: %+v %v", r, err)
	}
}
