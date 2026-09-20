# Security Review Policy

Focus on credible code paths rather than generic checklists.

Relevant areas include authentication, authorization, tenant isolation, secrets, input validation, injection, SSRF, path traversal, unsafe deserialization, file upload, cryptography, session/token lifecycle, rate limiting, sensitive logging, dependency/security configuration, privilege escalation and fail-open/fail-closed behavior.

A blocking security finding must show a credible path from input/state to affected code/security boundary and impact, with evidence and required mitigation.
