// Package invocation translates routing tuples into version-pinned provider
// commands. It does not authorize dispatch or provide worker isolation.
package invocation

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/PsyChaos/ai-team-harness/internal/routing"
)

// Settings are the settings encoded in the command, not an observation of a
// remote model. Model aliases and CLI defaults cannot establish these settings.
type Settings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type Command struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

type Result struct {
	Requested          routing.Tuple `json:"requested"`
	Effective          *Settings     `json:"effective"`
	EffectiveSource    string        `json:"effective_source,omitempty"`
	Version            string        `json:"version"`
	Command            *Command      `json:"command"`
	Status             string        `json:"status"`
	Reason             string        `json:"reason,omitempty"`
	ExecutionAttempted bool          `json:"execution_attempted"`
}

// Executor is supplied by the isolated worker boundary. Implementations must
// execute argv directly (never through a shell), feed the prompt on stdin, and
// control inherited config/environment. This package never accesses credentials.
type Executor interface {
	Run(context.Context, Command, io.Reader) error
}

// Build fails closed for versions and tuples outside the documented support
// table. version is the bare version from trusted CLI discovery, not task text.
// No substitutions are approved by this adapter.
func Build(tuple routing.Tuple, version string) (Result, error) {
	r := Result{Requested: tuple, Version: version, Status: "rejected"}
	reject := func(reason string) (Result, error) {
		r.Reason = reason
		return r, fmt.Errorf("invocation rejected: %s", reason)
	}
	var efforts []string
	switch tuple.Provider {
	case "codex":
		if version != "0.154.0" {
			return reject("codex requires CLI version 0.154.0")
		}
		if tuple.Model == "gpt-5.4" {
			efforts = []string{"low", "medium", "high", "xhigh"}
		}
	case "claude":
		if version != "2.1.235" {
			return reject("claude requires CLI version 2.1.235")
		}
		if tuple.Model == "claude-opus-4-6" || tuple.Model == "claude-sonnet-4-6" {
			efforts = []string{"low", "medium", "high", "max"}
		}
	default:
		return reject(fmt.Sprintf("unsupported provider %q", tuple.Provider))
	}
	if len(efforts) == 0 {
		return reject(fmt.Sprintf("model %q has no verified invocation support for %s", tuple.Model, tuple.Provider))
	}
	if !slices.Contains(efforts, tuple.Effort) {
		return reject(fmt.Sprintf("unsupported effort %q for %s/%s", tuple.Effort, tuple.Provider, tuple.Model))
	}
	var args []string
	if tuple.Provider == "codex" {
		args = []string{"exec", "--ephemeral", "--strict-config", "--model", tuple.Model,
			"--config", fmt.Sprintf("model_reasoning_effort=%q", tuple.Effort), "-"}
	} else {
		args = []string{"--print", "--no-session-persistence", "--output-format", "text",
			"--model", tuple.Model, "--effort", tuple.Effort}
	}
	r.Command = &Command{Executable: tuple.Provider, Args: args}
	r.Effective = &Settings{Provider: tuple.Provider, Model: tuple.Model, Effort: tuple.Effort}
	r.EffectiveSource = "command_arguments"
	r.Status = "built"
	return r, nil
}

// Invoke builds and validates before crossing the execution boundary. A rejected
// result records the requested tuple and reason with no effective settings.
func Invoke(ctx context.Context, tuple routing.Tuple, version string, prompt io.Reader, executor Executor) (Result, error) {
	r, err := Build(tuple, version)
	if err != nil {
		return r, err
	}
	if executor == nil {
		r.Status, r.Reason = "execution_failed", "executor is required"
		return r, fmt.Errorf("%s", r.Reason)
	}
	if err := ctx.Err(); err != nil {
		r.Status, r.Reason = "execution_failed", err.Error()
		return r, err
	}
	r.ExecutionAttempted = true
	command := *r.Command
	command.Args = slices.Clone(command.Args)
	if err := executor.Run(ctx, command, prompt); err != nil {
		r.Status, r.Reason = "execution_failed", err.Error()
		return r, err
	}
	r.Status = "completed"
	return r, nil
}
