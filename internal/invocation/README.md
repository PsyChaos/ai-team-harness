# Provider invocation

Issue #10's adapter consumes an explicit `routing.Tuple`. Use `Build` to inspect
a command or `Invoke` to validate and hand it to an injected executor. Both
return a serializable result even on rejection. The caller supplies a bare
version obtained from trusted CLI discovery.

The supported subset is deliberately closed:

| Provider CLI | Model | Efforts |
| --- | --- | --- |
| codex 0.154.0 | gpt-5.4 | low, medium, high, xhigh |
| Claude Code 2.1.235 | claude-opus-4-6 | low, medium, high, max |
| Claude Code 2.1.235 | claude-sonnet-4-6 | low, medium, high, max |

Other tuples, versions, aliases, and the registry's "CLI default" sentinel fail
before execution. This table is narrower than the vendors' full catalogs.
Adding support requires CLI/config and model capability evidence plus tests;
registry availability alone is insufficient. No fallback or effort translation
is approved. In particular, Claude's documented xhigh-to-high fallback for
Opus 4.6 is rejected before launch.

Commands for representative high-effort tuples (shown with shell quoting for
readability; executors receive separate argv entries):

```sh
codex exec --ephemeral --strict-config --model gpt-5.4 --config 'model_reasoning_effort="high"' -
claude --print --no-session-persistence --output-format text --model claude-opus-4-6 --effort high
```

Prompts travel separately on stdin, never in logged arguments. Results preserve
the whole requested tuple and separately record effective command settings.
`effective_source=command_arguments` means exactly that: it is not provider
telemetry or proof of the model served remotely. Rejections have null effective
settings and command, an explicit reason, and no execution attempt. Process
errors retain command evidence and report failure rather than substitution.

The executor must control environment/config, resolve the executable against
trusted discovery, and provide the worker's permission/isolation policy. The
adapter supplies no production process launcher and does not change the legacy
shell runner. Connecting it to namespaced execution is separate scope. Do not
treat these command arguments alone as a worker security boundary or as proof
against provider-side model switching/organization effort caps.

## Evidence

See [fixture provenance](testdata/README.md) and
[local validation](../../docs/validation/issue-10-invocation.md).
