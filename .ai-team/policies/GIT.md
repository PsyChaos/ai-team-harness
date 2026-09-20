# Git Policy

## Branch

```text
ai/issue-<number>-<short-name>
```

## Isolation

One implementation issue = one independent `--no-local` clone. Git metadata is
never shared with the coordinator checkout or another task.

## Commits

Focused, reviewable, traceable to the issue, without unrelated formatting/generated noise.

## Pull requests

Reference the issue and include summary, acceptance mapping, validation evidence, risks and follow-ups.

## Publishing

Implementers commit locally. Coordinator pushes and opens/updates PRs. This centralizes GitHub credentials/publication authority.

## Merge

Implementer cannot merge. Coordinator merges only after configured review + CI gates.
