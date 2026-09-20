#!/usr/bin/env python3
"""Offline CLI double for bootstrap integration tests, never used by the harness."""
import json
import os
from pathlib import Path
import sys

args = sys.argv[1:]
fixture_root = Path(sys.argv[0]).parent.parent
path = Path(os.environ.get("STUB_STATE", fixture_root / "github.json"))
state = json.loads(path.read_text())
tool = Path(sys.argv[0]).name
state.setdefault("calls", []).append([tool, *args])


def save():
    path.write_text(json.dumps(state))


def value(flag):
    return args[args.index(flag) + 1]


def issue(ref):
    return next(i for i in state["issues"] if i["number"] == int(ref.rsplit("/", 1)[-1]))


def output(data):
    save()
    print(json.dumps(data))
    raise SystemExit(0)


if tool in {"claude", "codex", "gemini"}:
    state["provider_calls"] = state.get("provider_calls", 0) + 1
    state["provider_stdin"] = sys.stdin.read()
    state["provider_env_keys"] = list(os.environ)
    state["provider_cwd"] = os.getcwd()
    state["provider_cwd_mode"] = Path.cwd().stat().st_mode & 0o777
    state["gemini_private_home"] = os.environ.get("GEMINI_CLI_HOME")
    save()
    if os.environ.get("STUB_AUTH_FAIL") or state.get("auth_fail"):
        print("secret provider detail", file=sys.stderr)
        raise SystemExit(17)
    plan = Path(os.environ.get("STUB_PLAN", fixture_root / "plan.json")).read_text()
    if tool == "codex" and "--output-last-message" in args:
        Path(value("--output-last-message")).write_text(plan)
    elif tool == "gemini":
        print(json.dumps({"response": plan}))
    else:
        print(plan)
    raise SystemExit(0)

if tool == "editor":
    Path(args[-1]).write_text(os.environ["STUB_EDITOR_BRIEF"])
    save()
    raise SystemExit(0)

if args[:2] == ["repo", "view"]:
    output({"nameWithOwner": "test/repo"})
if args[0] == "api":
    if args[1] == "user":
        output({"login": "publisher"})
    output([state["issues"]])
if args[:2] == ["project", "list"]:
    output({"projects": state["projects"]})
if args[:2] == ["project", "create"]:
    p = {"id": "P1", "number": 1, "title": value("--title"), "closed": False}
    state["projects"].append(p)
    output(p)
if args[:2] == ["project", "view"]:
    output(next(p for p in state["projects"] if str(p["number"]) == args[2]))
if args[:2] == ["project", "field-list"]:
    if "--jq" in args:
        save()
        print("\n".join(f["name"] for f in state["fields"]))
        raise SystemExit(0)
    output({"fields": state["fields"]})
if args[:2] == ["project", "item-add"]:
    number = str(issue(value("--url"))["number"])
    state["items"].setdefault(number, {})
    output({"id": "I" + number})
if args[:2] == ["project", "item-edit"]:
    number = value("--id")[1:]
    field = next(f for f in state["fields"] if f["id"] == value("--field-id"))
    if "--single-select-option-id" in args:
        val = next(o["name"] for o in field["options"] if o["id"] == value("--single-select-option-id"))
    else:
        val = value("--number" if "--number" in args else "--text")
    state["items"][number][field["name"]] = val
    state.setdefault("field_history", []).append([number, field["name"], val])
    output({})
if args[:2] == ["issue", "create"]:
    number = len(state["issues"]) + 1
    if os.environ.get("STUB_FAIL_CREATE") == str(number) and not state.get("failed"):
        state["failed"] = True
        save()
        raise SystemExit(19)
    url = f"https://github.com/test/repo/issues/{number}"
    item = {"number": number, "url": url, "html_url": url, "state": "open", "title": value("--title"),
            "body": Path(value("--body-file")).read_text(), "parent": None, "blockedBy": [],
            "author": {"login": "publisher"}, "user": {"login": "publisher"}}
    state["issues"].append(item)
    if os.environ.get("STUB_FAIL_EDGE") == str(number) and not state.get("failed_edge"):
        state["failed_edge"] = True
        save()
        raise SystemExit(21)
    if "--parent" in args:
        item["parent"] = {"number": issue(value("--parent"))["number"]}
    item["blockedBy"] = [{"number": issue(args[i+1])["number"]} for i, a in enumerate(args) if a == "--blocked-by"]
    save()
    print(url)
    raise SystemExit(0)
if args[:2] == ["issue", "view"]:
    result = dict(issue(args[2]))
    def ref_url(ref):
        return {**ref, "url": f"https://github.com/test/repo/issues/{ref['number']}"}
    if result["parent"]:
        result["parent"] = ref_url(result["parent"])
    result["blockedBy"] = {"nodes": [ref_url(ref) for ref in result["blockedBy"]], "totalCount": len(result["blockedBy"])}
    output(result)
if args[:2] == ["issue", "edit"]:
    item = issue(args[2])
    if "--body-file" in args:
        item["body"] = Path(value("--body-file")).read_text()
    if "--parent" in args:
        item["parent"] = {"number": issue(value("--parent"))["number"]}
    if "--add-blocked-by" in args:
        dep = {"number": int(value("--add-blocked-by"))}
        if dep not in item["blockedBy"]:
            item["blockedBy"].append(dep)
    output({})
save()
print("Unsupported stub call: " + repr(args), file=sys.stderr)
raise SystemExit(99)
