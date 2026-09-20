# Provider Adapters

The harness uses provider CLIs rather than SDK code.

## Shared skills

Canonical skills live in `.agents/skills/`. Codex and Gemini can use this interoperable repository location. Claude Code loads project skills from `.claude/skills/`; `install.sh` creates symlinks from there to the canonical skill directories.

## Worker contract

`.ai-team/bin/spawn-agent` + `run-provider-agent` enforce a common contract:

- consume Task/Review Pack
- run in supplied working directory
- emit structured final response
- wrapper captures final output as result evidence
- exit non-zero on provider/process failure
- never own durable GitHub state

## Coordinator

`coordinator-cycle` is provider-independent and deterministic. Claude, Codex and
Gemini are supported only as isolated implementers/reviewers; direct model
coordinator modes have been removed.
