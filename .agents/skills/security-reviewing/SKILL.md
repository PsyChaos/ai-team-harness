---
name: security-reviewing
description: Performs evidence-based independent security review for high-risk or security-sensitive changes, focusing on realistic code paths and impact. Use when Risk is HIGH/CRITICAL or work touches authentication, authorization, secrets, tenant isolation, network boundaries, or sensitive data.
---

# Security Reviewing

Read `.ai-team/policies/SECURITY.md` and review only relevant attack surfaces.

Do not output a generic vulnerability checklist as findings.

For every blocking finding show reachable input/state, affected code path, security boundary crossed, impact, evidence and required mitigation.

Use the normal review severity scheme and produce a structured final result.
