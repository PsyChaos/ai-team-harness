# Invocation fixture provenance

Captured on 2026-09-21 in the issue-10 implementation sandbox, based on source
commit 6455033. No authenticated model requests were made.

Captured stdout files (trailing whitespace stripped from Codex help):
- `codex-version.txt`: `node /home/heisenberg/.nvm/versions/node/v25.8.1/lib/node_modules/@openai/codex/bin/codex.js --version`
- `codex-help.txt`: the same executable with `--help`
- `codex-exec-help.txt`: the same executable with `exec --help`
- `claude-version.txt`: `/usr/bin/claude --version`
- `claude-help.txt`: `/usr/bin/claude --help`

All exited zero. Codex reported 0.154.0. Its stderr separately warned that PATH
aliases could not be created (permission denied); the stdout help was complete.
Claude reported **2.1.84**, not the requested **2.1.235**. Its fixture establishes
only that this older CLI documents the chosen flags. It is NOT a captured
2.1.235 help fixture. Exact-version Claude validation remains an external host
check, and 2.1.84 is deliberately rejected by the adapter.

`capabilities.json` is a manually recorded, documentation-derived support
subset, not raw help, live discovery, or an assertion of account availability.
Its source pages were read on the capture date:
- [GPT-5.4 model reference](https://developers.openai.com/api/docs/models/gpt-5.4)
  documents low, medium, high, xhigh (and none; none is outside our subset).
- [Codex configuration reference](https://developers.openai.com/codex/config-reference/)
  documents the `model_reasoning_effort` config key and the four chosen values.
  Local `exec --help` documents TOML-valued `--config` and `--model`.
- [Claude model configuration](https://code.claude.com/docs/en/model-config)
  documents explicit model names, the effort flag, the four selected effort
  levels for Opus/Sonnet 4.6, and fallback for unsupported levels.
- [Claude effort reference](https://platform.claude.com/docs/en/build-with-claude/effort)
  corroborates model effort support.

These are current documentation snapshots summarized as structured facts, not
versioned historical documentation. The exact Claude 2.1.235 help comparison
must still be supplied by the host. Do not relabel the older capture.

`unsupported.json` is an authored negative test case: Opus 4.6 with xhigh.
The documented CLI fallback would silently lower the effort; this adapter must
instead return the fixture's reason without calling the executor.

Tests compare every supported command's flags to the help captures, exact argv
to the documented encoding, and arguments/stdin received by a fake executable.
The fake never invokes a vendor, uses credentials, or accesses the network.
