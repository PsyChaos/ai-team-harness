#!/usr/bin/env python3
"""
AI Team Harness decision engine.

Backends:
- rules: deterministic fallback/default
- jev: TypeSafe System One / Jev via native HTTP API

The decision engine provides judgment only. It never decides deterministic facts
such as CI success, dependency completion, worker liveness, or merge eligibility.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from typing import Any

VALID_PROVIDERS = ("codex", "claude", "gemini")
VALID_WORK_TYPES = (
    "Architecture", "Backend", "Frontend", "Mobile", "Database", "DevOps",
    "Test", "Security", "Documentation", "Refactor", "Bug",
)

ACTION_DESCRIPTIONS = {
    "plan": "Decompose or refine work because execution is not ready.",
    "dispatch_implementer": "Start an implementation worker for ready executable work.",
    "dispatch_reviewer": "Start an independent reviewer for implemented work.",
    "retry": "Retry the same bounded work after a recoverable failure.",
    "replan": "Return to planning because the task definition or approach is inadequate.",
    "split_task": "Split work because the current task is too broad or internally independent.",
    "wait_human": "Escalate because a product/security/authority decision requires a human.",
    "wait_provider": "Wait because provider availability/quota is the blocker.",
}


def env_float(name: str, default: float) -> float:
    try:
        return float(os.getenv(name, str(default)))
    except ValueError:
        return default


def csv_env(name: str, default: str) -> list[str]:
    return [x.strip() for x in os.getenv(name, default).split(",") if x.strip()]


def read_state(path: str | None) -> dict[str, Any]:
    if path:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
    else:
        raw = sys.stdin.read()
        data = json.loads(raw) if raw.strip() else {}
    if not isinstance(data, dict):
        raise ValueError("state must be a JSON object")
    return data


def available_providers(state: dict[str, Any]) -> list[str]:
    supplied = state.get("available_providers")
    if isinstance(supplied, list):
        result = [str(x).lower() for x in supplied if str(x).lower() in VALID_PROVIDERS]
        if result:
            return result

    order = csv_env("HARNESS_IMPLEMENTER_ORDER", "codex,claude,gemini")
    enabled = {
        "claude": os.getenv("HARNESS_ENABLE_CLAUDE", "1") == "1",
        "codex": os.getenv("HARNESS_ENABLE_CODEX", "1") == "1",
        "gemini": os.getenv("HARNESS_ENABLE_GEMINI", "1") == "1",
    }
    return [p for p in order if p in VALID_PROVIDERS and enabled.get(p, False)]


def fallback_provider(state: dict[str, Any], *, exclude: str | None = None) -> str | None:
    for provider in available_providers(state):
        if provider != exclude:
            return provider
    providers = available_providers(state)
    return providers[0] if providers else None


def risk(state: dict[str, Any]) -> str:
    return str(state.get("risk") or "MEDIUM").upper()


def work_type_hint(state: dict[str, Any]) -> str | None:
    value = str(state.get("work_type") or "").strip()
    for item in VALID_WORK_TYPES:
        if value.lower() == item.lower():
            return item
    return None


def rule_task(state: dict[str, Any]) -> dict[str, Any]:
    wt = work_type_hint(state)
    r = risk(state)
    provider = fallback_provider(state)

    if r in {"HIGH", "CRITICAL"} or wt in {"Architecture", "Security", "Database"}:
        model_profile = "strong"
    elif wt in {"Documentation", "Refactor", "Test"} and r == "LOW":
        model_profile = "fast"
    else:
        model_profile = "balanced"

    if r == "CRITICAL":
        review_mode = "dual"
    elif r == "HIGH" or wt == "Security":
        review_mode = "security"
    else:
        review_mode = "standard"

    return {
        "work_type": wt,
        "provider": provider,
        "model_profile": model_profile,
        "review_mode": review_mode,
    }


def rule_retry(state: dict[str, Any]) -> dict[str, Any]:
    attempts = int(state.get("attempts") or 0)
    failure_kind = str(state.get("failure_kind") or "").lower()
    previous_provider = str(state.get("previous_provider") or "").lower() or None

    if failure_kind in {"provider_unavailable", "rate_limit", "quota"}:
        strategy = "switch_provider"
    elif failure_kind in {"scope_too_large", "task_too_large"}:
        strategy = "split_task"
    elif failure_kind in {"ambiguous_requirement", "product_decision"}:
        strategy = "human"
    elif failure_kind in {"wrong_plan", "architecture_mismatch"}:
        strategy = "replan"
    elif attempts >= 1:
        strategy = "switch_provider"
    else:
        strategy = "same_provider"

    return {
        "strategy": strategy,
        "provider": fallback_provider(state, exclude=previous_provider)
        if strategy == "switch_provider"
        else previous_provider or fallback_provider(state),
    }


def rule_action(state: dict[str, Any]) -> dict[str, Any]:
    allowed = [str(x) for x in state.get("allowed_actions", []) if str(x)]
    if not allowed:
        raise ValueError("action routing requires allowed_actions")

    # Deterministic preference only among actions already declared valid by the coordinator.
    preferred_order = (
        "dispatch_reviewer", "retry", "replan", "split_task",
        "dispatch_implementer", "plan", "wait_provider", "wait_human",
    )
    for item in preferred_order:
        if item in allowed:
            return {"action": item}
    return {"action": allowed[0]}


def typesafe_call(state: dict[str, Any], questions: dict[str, Any]) -> dict[str, Any]:
    api_key = os.getenv("TYPESAFE_API_KEY", "").strip()
    if not api_key:
        raise RuntimeError("TYPESAFE_API_KEY is not configured")

    base_url = os.getenv("TYPESAFE_BASE_URL", "https://api.typesafe.ai").rstrip("/")
    model = os.getenv("TYPESAFE_MODEL", "jev-latest")
    payload = {
        "model": model,
        "state": state,
        "questions": questions,
    }

    request = urllib.request.Request(
        f"{base_url}/v1/systemone",
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "ai-team-harness/1.1.0",
        },
        method="POST",
    )

    timeout = float(os.getenv("HARNESS_JEV_TIMEOUT_SECONDS", "30"))
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"TypeSafe HTTP {exc.code}: {detail[:1000]}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"TypeSafe connection failed: {exc.reason}") from exc

    decoded = json.loads(body)
    if not isinstance(decoded, dict) or not isinstance(decoded.get("answers"), dict):
        raise RuntimeError("TypeSafe response does not contain an answers object")
    return decoded


def choice_question(instructions: str, criteria: dict[str, str | None]) -> dict[str, Any]:
    return {
        "type": "choice",
        "instructions": instructions,
        "criteria": criteria,
    }


def choice_answer(response: dict[str, Any], key: str) -> tuple[str | None, float, dict[str, float]]:
    answer = response.get("answers", {}).get(key, {})
    if not isinstance(answer, dict):
        return None, 0.0, {}
    value = answer.get("choice")
    confidence = answer.get("confidence")
    probs = answer.get("probabilities")
    try:
        conf = float(confidence)
    except (TypeError, ValueError):
        conf = 0.0
    return (
        str(value) if value is not None else None,
        conf,
        probs if isinstance(probs, dict) else {},
    )


def gated(value: Any, confidence: float, threshold: float, fallback: Any, probs: dict[str, Any]) -> dict[str, Any]:
    if value is not None and confidence >= threshold:
        return {
            "value": value,
            "source": "jev",
            "confidence": confidence,
            "threshold": threshold,
            "probabilities": probs,
        }
    return {
        "value": fallback,
        "source": "rules-fallback",
        "confidence": confidence,
        "threshold": threshold,
        "probabilities": probs,
    }



def wrapped_rules(values: dict[str, Any], source: str = "rules") -> dict[str, Any]:
    return {
        key: {
            "value": value,
            "source": source,
            "confidence": None,
            "threshold": None,
            "probabilities": {},
        }
        for key, value in values.items()
    }

def jev_task(state: dict[str, Any]) -> dict[str, Any]:
    rules = rule_task(state)
    providers = available_providers(state)
    if not providers:
        raise RuntimeError("no enabled/available providers supplied")

    questions: dict[str, Any] = {}

    if os.getenv("HARNESS_JEV_TASK_CLASSIFICATION", "1") == "1":
        questions["work_type"] = choice_question(
            "Classify the primary engineering work type for this task.",
            {x: None for x in VALID_WORK_TYPES},
        )

    if os.getenv("HARNESS_JEV_PROVIDER_ROUTING", "1") == "1":
        provider_descriptions = {
            "codex": "Coding agent suited to implementation, repository editing, tests and code review.",
            "claude": "General reasoning/coding agent suited to architecture, implementation and review.",
            "gemini": "General coding/reasoning agent available as an alternate provider.",
        }
        questions["provider"] = choice_question(
            "Which available provider is the best fit for implementing this task? Judge the task, risk and required reasoning. Availability is already filtered by the caller.",
            {p: provider_descriptions.get(p) for p in providers},
        )

    if os.getenv("HARNESS_JEV_MODEL_ROUTING", "1") == "1":
        questions["model_profile"] = choice_question(
            "What capability/cost profile should the implementation model use?",
            {
                "fast": "Routine, low-risk, mechanically bounded work where speed/cost matter most.",
                "balanced": "Normal engineering work needing solid coding and reasoning.",
                "strong": "Complex, high-risk, architectural, concurrency, security, data-integrity or difficult debugging work.",
            },
        )

    if os.getenv("HARNESS_JEV_REVIEW_ROUTING", "1") == "1":
        questions["review_mode"] = choice_question(
            "What independent review level should this task require after implementation?",
            {
                "standard": "One independent engineering review.",
                "security": "Engineering review plus security-focused review.",
                "dual": "Two independent reviews, with security review when relevant.",
            },
        )

    if not questions:
        return {"engine": "rules", "kind": "task", "decisions": wrapped_rules(rules)}

    response = typesafe_call(state, questions)
    decisions: dict[str, Any] = {}

    if "work_type" in questions:
        value, conf, probs = choice_answer(response, "work_type")
        decisions["work_type"] = gated(
            value, conf, env_float("HARNESS_JEV_WORK_TYPE_CONFIDENCE", 0.70),
            rules["work_type"], probs,
        )
    else:
        decisions["work_type"] = {"value": rules["work_type"], "source": "rules"}

    if "provider" in questions:
        value, conf, probs = choice_answer(response, "provider")
        decisions["provider"] = gated(
            value, conf, env_float("HARNESS_JEV_PROVIDER_CONFIDENCE", 0.75),
            rules["provider"], probs,
        )
    else:
        decisions["provider"] = {"value": rules["provider"], "source": "rules"}

    if "model_profile" in questions:
        value, conf, probs = choice_answer(response, "model_profile")
        decisions["model_profile"] = gated(
            value, conf, env_float("HARNESS_JEV_MODEL_CONFIDENCE", 0.75),
            rules["model_profile"], probs,
        )
    else:
        decisions["model_profile"] = {"value": rules["model_profile"], "source": "rules"}

    if "review_mode" in questions:
        value, conf, probs = choice_answer(response, "review_mode")
        decisions["review_mode"] = gated(
            value, conf, env_float("HARNESS_JEV_REVIEW_CONFIDENCE", 0.80),
            rules["review_mode"], probs,
        )
    else:
        decisions["review_mode"] = {"value": rules["review_mode"], "source": "rules"}

    return {
        "engine": "jev",
        "model": response.get("model") or os.getenv("TYPESAFE_MODEL", "jev-latest"),
        "kind": "task",
        "decisions": decisions,
    }


def jev_retry(state: dict[str, Any]) -> dict[str, Any]:
    rules = rule_retry(state)
    providers = available_providers(state)
    questions = {
        "strategy": choice_question(
            "Given the failed attempt and evidence, what should the harness do next?",
            {
                "same_provider": "Retry the bounded task in a fresh session with the same provider.",
                "switch_provider": "Retry with a different available provider/model.",
                "replan": "Return to planning because the approach/task definition is inadequate.",
                "split_task": "Split the task because scope is too broad or internally separable.",
                "human": "Escalate because an explicit human product/security/authority decision is required.",
            },
        )
    }
    if len(providers) > 1:
        questions["provider"] = choice_question(
            "If a provider switch is needed, which available provider is the best next attempt?",
            {p: None for p in providers},
        )

    response = typesafe_call(state, questions)
    strategy, conf, probs = choice_answer(response, "strategy")
    strategy_decision = gated(
        strategy, conf, env_float("HARNESS_JEV_RETRY_CONFIDENCE", 0.80),
        rules["strategy"], probs,
    )

    provider_decision: dict[str, Any] = {"value": rules["provider"], "source": "rules"}
    if "provider" in questions:
        p, pconf, pprobs = choice_answer(response, "provider")
        provider_decision = gated(
            p, pconf, env_float("HARNESS_JEV_PROVIDER_CONFIDENCE", 0.75),
            rules["provider"], pprobs,
        )

    return {
        "engine": "jev",
        "model": response.get("model") or os.getenv("TYPESAFE_MODEL", "jev-latest"),
        "kind": "retry",
        "decisions": {
            "strategy": strategy_decision,
            "provider": provider_decision,
        },
    }


def jev_action(state: dict[str, Any]) -> dict[str, Any]:
    allowed = [str(x) for x in state.get("allowed_actions", []) if str(x)]
    if not allowed:
        raise ValueError("action routing requires allowed_actions")
    rules = rule_action(state)
    criteria = {a: ACTION_DESCRIPTIONS.get(a, f"Select action {a}.") for a in allowed}
    response = typesafe_call(
        state,
        {"action": choice_question(
            "Choose the best next action among actions that the deterministic coordinator has already declared valid. Do not invent an action.",
            criteria,
        )},
    )
    value, conf, probs = choice_answer(response, "action")
    return {
        "engine": "jev",
        "model": response.get("model") or os.getenv("TYPESAFE_MODEL", "jev-latest"),
        "kind": "action",
        "decisions": {
            "action": gated(
                value, conf, env_float("HARNESS_JEV_ACTION_CONFIDENCE", 0.85),
                rules["action"], probs,
            )
        },
    }


def smoke_test() -> dict[str, Any]:
    response = typesafe_call(
        {"text": "Fix a small typo in developer documentation.", "risk": "LOW"},
        {
            "kind": choice_question(
                "What kind of work is this?",
                {
                    "documentation": "Documentation-only work.",
                    "security": "Security-sensitive engineering work.",
                    "database": "Database/schema work.",
                },
            )
        },
    )
    value, confidence, probabilities = choice_answer(response, "kind")
    return {
        "ok": True,
        "engine": "jev",
        "model": response.get("model") or os.getenv("TYPESAFE_MODEL", "jev-latest"),
        "sample": {
            "choice": value,
            "confidence": confidence,
            "probabilities": probabilities,
        },
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("kind", choices=["task", "retry", "action", "smoke-test"])
    parser.add_argument("--state-file")
    args = parser.parse_args()

    backend = os.getenv("HARNESS_DECISION_ENGINE", "rules").lower()
    fallback = os.getenv("HARNESS_DECISION_FALLBACK", "rules").lower()

    if args.kind == "smoke-test":
        if backend != "jev":
            print(json.dumps({"ok": True, "engine": "rules", "message": "Jev disabled"}, indent=2))
            return 0
        try:
            print(json.dumps(smoke_test(), indent=2, sort_keys=True))
            return 0
        except Exception as exc:
            print(json.dumps({"ok": False, "engine": "jev", "error": str(exc)}, indent=2))
            return 1

    state = read_state(args.state_file)

    if backend == "rules":
        if args.kind == "task":
            result = {"engine": "rules", "kind": "task", "decisions": wrapped_rules(rule_task(state))}
        elif args.kind == "retry":
            result = {"engine": "rules", "kind": "retry", "decisions": wrapped_rules(rule_retry(state))}
        else:
            result = {"engine": "rules", "kind": "action", "decisions": wrapped_rules(rule_action(state))}
        print(json.dumps(result, indent=2, sort_keys=True))
        return 0

    if backend != "jev":
        print(json.dumps({"error": f"unsupported decision engine: {backend}"}, indent=2))
        return 2

    try:
        if args.kind == "task":
            result = jev_task(state)
        elif args.kind == "retry":
            if os.getenv("HARNESS_JEV_RETRY_ROUTING", "1") != "1":
                result = {"engine": "rules", "kind": "retry", "decisions": wrapped_rules(rule_retry(state))}
            else:
                result = jev_retry(state)
        else:
            if os.getenv("HARNESS_JEV_ACTION_ROUTING", "1") != "1":
                result = {"engine": "rules", "kind": "action", "decisions": wrapped_rules(rule_action(state))}
            else:
                result = jev_action(state)
        print(json.dumps(result, indent=2, sort_keys=True))
        return 0
    except Exception as exc:
        if fallback == "rules":
            if args.kind == "task":
                decisions = rule_task(state)
            elif args.kind == "retry":
                decisions = rule_retry(state)
            else:
                decisions = rule_action(state)
            print(json.dumps({
                "engine": "rules-fallback",
                "kind": args.kind,
                "fallback_reason": str(exc),
                "decisions": wrapped_rules(decisions, source="rules-fallback"),
            }, indent=2, sort_keys=True))
            return 0

        print(json.dumps({"engine": "jev", "kind": args.kind, "error": str(exc)}, indent=2))
        return 3


if __name__ == "__main__":
    raise SystemExit(main())
