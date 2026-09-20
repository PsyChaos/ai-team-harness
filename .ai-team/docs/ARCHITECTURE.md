# Architecture

```text
                     GitHub Project
                  durable work state
                          |
                          v
                 Coordinator Cycle
                    fresh session
                          |
                +---------+---------+
                | Decision Layer    |
                |                   |
                | rules (default)   |
                | Jev (optional)    |
                +---------+---------+
                          |
          +---------------+----------------+
          |               |                |
      provider        model profile     review mode
          |               |                |
          +---------------+----------------+
                          |
                    Policy/Capacity
                          |
                          v
                    systemd worker
                          |
                 isolated Git clone
                          |
                    local commit
                          |
                          v
                     Coordinator
                     publishes PR
                          |
                  independent review
                          |
                 deterministic CI/gates
                          |
                         merge
```

## Durable vs disposable

Durable:

- GitHub Project
- Issues/dependencies
- PRs
- CI
- Git commits
- review evidence

Disposable:

- LLM sessions
- `.ai-team/runtime/jobs`
- temporary routing state
- provider process state

## Decision engine

Jev is a judgment layer, not workflow authority.

The coordinator first determines which actions are valid using deterministic
state. Jev may then rank/classify among fuzzy choices.

If Jev disappears, rules mode keeps the harness operational.

## No orchestration server

v1.1 still intentionally has no:

- PostgreSQL
- Redis
- custom queue
- custom daemon/server
- custom web UI

systemd + GitHub + Git + provider CLIs remain the runtime substrate.
