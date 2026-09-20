#!/usr/bin/env python3
"""Validated planning and resumable GitHub publication; only stdlib dependencies."""
import argparse
import fcntl
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import signal
import shlex
import shutil
import subprocess
import sys
import tempfile
import time

MAX_PLAN = 48000
MAX_BRIEF = 32000
KINDS = {"phase", "feature", "task", "subtask"}
WORK_TYPES = {"Architecture", "Backend", "Frontend", "Mobile", "Database", "DevOps",
              "Test", "Security", "Documentation", "Refactor", "Bug"}
PLAN_BEGIN = "<!-- ai-harness-plan:v1 -->\n"
PLAN_END = "\n<!-- /ai-harness-plan -->"
COMPLETE = "<!-- ai-harness-bootstrap:complete -->"


class BootstrapError(Exception):
    """An actionable, redacted failure safe to show to the operator."""


def terminate_process_group(process):
    """Terminate and reap a timeout-enabled command and all of its descendants."""
    process_group = process.pid
    try:
        os.killpg(process_group, signal.SIGTERM)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        process.poll()  # Reap the group leader without depending on inherited pipes.
        try:
            os.killpg(process_group, 0)
        except ProcessLookupError:
            break
        time.sleep(0.05)
    else:
        try:
            os.killpg(process_group, signal.SIGKILL)
        except ProcessLookupError:
            pass
    process.communicate()


def run(argv, *, stdin=None, cwd=None, env=None, timeout=None):
    """Never print arguments/provider output: either may contain private brief data."""
    try:
        if timeout is None:
            result = subprocess.run(argv, input=stdin, cwd=cwd, env=env, text=True,
                                    capture_output=True, check=False)
        else:
            process = subprocess.Popen(argv, stdin=subprocess.PIPE if stdin is not None else None,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=cwd,
                                       env=env, text=True, start_new_session=True)
            try:
                stdout, stderr = process.communicate(input=stdin, timeout=timeout)
            except subprocess.TimeoutExpired:
                terminate_process_group(process)
                raise BootstrapError(
                    f"{Path(argv[0]).name} planner timed out after {timeout} seconds; check CLI login/network "
                    "or increase HARNESS_PLANNER_TIMEOUT_SECONDS, then rerun the same brief. "
                    "No GitHub issues or signing key were created.") from None
            except BaseException:
                terminate_process_group(process)
                raise
            result = subprocess.CompletedProcess(argv, process.returncode, stdout, stderr)
    except OSError as exc:
        raise BootstrapError(f"Cannot execute {Path(argv[0]).name}: {exc.strerror}") from exc
    if result.returncode:
        raise BootstrapError(f"{Path(argv[0]).name} failed (exit {result.returncode}); "
                             "check CLI login, permissions and configuration, then rerun the same brief.")
    return result.stdout.strip()


def gh(*args):
    # GitHub CLI uses its own credential store. Never forward harness signing
    # keys or the optional routing API key to an unprivileged child process.
    blocked = {"HARNESS_BOOTSTRAP_HMAC_KEY", "HARNESS_BROKER_HMAC_KEY",
               "TYPESAFE_API_KEY", "GH_TOKEN", "GITHUB_TOKEN"}
    environment = {name: value for name, value in os.environ.items() if name not in blocked}
    return run(["gh", *map(str, args)], env=environment)


def gh_json(*args):
    try:
        return json.loads(gh(*args))
    except ValueError as exc:
        raise BootstrapError("GitHub CLI returned invalid JSON") from exc


def text(value, name, limit=6000):
    if not isinstance(value, str) or not value.strip() or len(value) > limit or "\x00" in value:
        raise BootstrapError(f"Invalid {name}")


def validate(plan):
    if not isinstance(plan, dict) or set(plan) != {"title", "summary", "nodes"}:
        raise BootstrapError("Plan requires title, summary, nodes")
    text(plan["title"], "plan title", 200)
    text(plan["summary"], "plan summary")
    if len(json.dumps(plan, ensure_ascii=False).encode()) > MAX_PLAN:
        raise BootstrapError("Plan exceeds 48000 bytes")
    nodes = plan["nodes"]
    if not isinstance(nodes, list) or not 1 <= len(nodes) <= 80:
        raise BootstrapError("Plan must contain 1–80 nodes")
    # Reserve space for durable identity bindings before publishing anything.
    if len(json.dumps(plan, ensure_ascii=False)) + len(nodes) * 350 + len(plan["summary"]) + 1500 > 60000:
        raise BootstrapError("Plan leaves insufficient room for durable issue bindings in the tracking issue")
    index = {}
    fields = {"id", "kind", "parent", "title", "description", "acceptance", "validation",
              "depends_on", "risk", "priority", "work_type"}
    for node in nodes:
        if not isinstance(node, dict) or set(node) != fields:
            raise BootstrapError("Invalid node schema")
        node_id = node["id"]
        if not isinstance(node_id, str) or not re.fullmatch(r"[a-z][a-z0-9-]{0,47}", node_id):
            raise BootstrapError("Invalid node ID")
        if node_id in index:
            raise BootstrapError("Duplicate node ID")
        index[node_id] = node
        for key, allowed in (("kind", KINDS), ("risk", {"LOW", "MEDIUM", "HIGH", "CRITICAL"}),
                             ("priority", {"P0", "P1", "P2", "P3"}), ("work_type", WORK_TYPES)):
            if not isinstance(node[key], str) or node[key] not in allowed:
                raise BootstrapError(f"Invalid node {key}")
        text(node["title"], "node title", 200)
        text(node["description"], "node description")
        for key in ("acceptance", "validation", "depends_on"):
            if not isinstance(node[key], list) or len(node[key]) > 80:
                raise BootstrapError(f"Invalid {key}")
            for value in node[key]:
                text(value, key, 1000)
        if len(set(node["depends_on"])) != len(node["depends_on"]):
            raise BootstrapError("Duplicate dependency")
        if node["parent"] is not None and not isinstance(node["parent"], str):
            raise BootstrapError("Invalid parent")
    parents = {n["parent"] for n in nodes if n["parent"] is not None}
    leaves = set(index) - parents
    blocking_counts = {node_id: 0 for node_id in index}
    for node in nodes:
        refs = node["depends_on"] + ([node["parent"]] if node["parent"] is not None else [])
        if any(ref not in index or ref == node["id"] for ref in refs):
            raise BootstrapError("Unknown or self reference")
        if node["id"] in leaves:
            if node["kind"] not in {"task", "subtask"} or not node["acceptance"] or not node["validation"]:
                raise BootstrapError("Executable leaves require task/subtask kind, acceptance and validation")
        elif node["depends_on"]:
            raise BootstrapError("Container dependencies must be expressed between executable leaves")
        if any(dep not in leaves for dep in node["depends_on"]):
            raise BootstrapError("Dependencies must reference executable leaves")
        if len(node["depends_on"]) > 50:
            raise BootstrapError("GitHub limits each issue to 50 blocked-by dependencies")
        for dep in node["depends_on"]:
            blocking_counts[dep] += 1
            if blocking_counts[dep] > 50:
                raise BootstrapError("GitHub limits each issue to blocking 50 other issues")
    order, visited, active = [], set(), set()

    def visit(node_id):
        if node_id in active:
            raise BootstrapError("Cyclic parent/dependency graph")
        if node_id in visited:
            return
        active.add(node_id)
        node = index[node_id]
        for ref in node["depends_on"] + ([node["parent"]] if node["parent"] is not None else []):
            visit(ref)
        active.remove(node_id)
        visited.add(node_id)
        order.append(node)

    for node_id in index:
        visit(node_id)
        depth, cursor = 0, node_id
        while cursor is not None:
            depth += 1
            cursor = index[cursor]["parent"]
        if depth > 6:
            raise BootstrapError("Parent hierarchy exceeds 6 levels")
    return order, leaves


class Project:
    def __init__(self, owner, number):
        if not isinstance(owner, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]{0,99}", owner):
            raise BootstrapError("Invalid GitHub Project owner")
        if not str(number).isdigit() or int(number) < 1:
            raise BootstrapError("Invalid GitHub Project number")
        self.owner, self.number = owner, str(number)
        data = gh_json("project", "view", self.number, "--owner", owner, "--format", "json")
        self.id = data["id"]
        self.fields = None

    def item(self, url):
        return gh_json("project", "item-add", self.number, "--owner", self.owner,
                       "--url", url, "--format", "json")["id"]

    def set(self, item_id, name, value):
        if self.fields is None:
            self.fields = gh_json("project", "field-list", self.number, "--owner", self.owner,
                                  "--limit", "1000", "--format", "json")["fields"]
        matches = [f for f in self.fields if f["name"] == name]
        if len(matches) != 1:
            raise BootstrapError(f"Project field missing or ambiguous: {name}; run setup-github-project")
        field = matches[0]
        args = ["project", "item-edit", "--id", item_id, "--project-id", self.id, "--field-id", field["id"]]
        if "options" in field:
            options = [o for o in field["options"] if o["name"] == value]
            if len(options) != 1:
                raise BootstrapError(f"Invalid option for {name}")
            args += ["--single-select-option-id", options[0]["id"]]
        elif name == "Retry Count":
            if not re.fullmatch(r"[0-9]+", value):
                raise BootstrapError("Retry Count must be a nonnegative integer")
            args += ["--number", value]
        else:
            args += ["--text", value]
        gh(*args)


def issues(repo):
    pages = gh_json("api", f"repos/{repo}/issues?state=all&per_page=100", "--paginate", "--slurp")
    return [issue for page in pages for issue in page if "pull_request" not in issue]


def find_marker(existing, marker):
    matches = [i for i in existing if (i.get("body") or "").splitlines()[:1] == [marker]]
    if len(matches) > 1:
        raise BootstrapError("Duplicate durable bootstrap marker; reconcile GitHub issues before retrying")
    return matches[0] if matches else None


def issue_url(issue):
    return issue.get("html_url", issue.get("url"))


def issue_author(issue):
    return (issue.get("author") or issue.get("user") or {}).get("login", "")


def content_digest(title, body):
    return hashlib.sha256((title + "\x00" + body).encode("utf-8")).hexdigest()


def verify_identity(issue, binding, publisher, title=None, body=None):
    if issue_author(issue).lower() != publisher.lower():
        raise BootstrapError("Bootstrap marker candidate is not authored by the authenticated publisher")
    actual = content_digest(issue["title"], issue["body"])
    if title is not None and actual != content_digest(title, body):
        raise BootstrapError("Bootstrap issue content differs from the canonical plan; refusing to adopt or dispatch it")
    if binding and (binding.get("number") != issue["number"] or
                    binding.get("url", "").lower() != issue_url(issue).lower() or
                    binding.get("digest") != actual):
        raise BootstrapError("Bootstrap issue identity/content does not match its durable binding")
    return {"number": issue["number"], "url": issue_url(issue), "digest": actual}


def create_issue(repo, title, body, parent=None, dependencies=()):
    # A private temporary body file avoids both argv size limits and shell interpolation.
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as handle:
        handle.write(body)
        handle.flush()
        args = ["issue", "create", "--repo", repo, "--title", title, "--body-file", handle.name]
        if parent:
            args += ["--parent", parent]
        for dep in dependencies:
            args += ["--blocked-by", dep]
        url = gh(*args)
    result = gh_json("issue", "view", url, "--repo", repo, "--json", "number,url,title,body,state,author")
    return result


def edit_body(repo, root, body):
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as handle:
        handle.write(body)
        handle.flush()
        gh("issue", "edit", str(root["number"]), "--repo", repo, "--body-file", handle.name)


def signing_key():
    value = os.environ.get("HARNESS_BOOTSTRAP_HMAC_KEY", "")
    if not value:
        raise BootstrapError("Bootstrap signing key is missing. Restore HARNESS_BOOTSTRAP_HMAC_KEY from your private runtime.env backup; existing signed roots cannot be recovered with a new key.")
    if not re.fullmatch(r"[a-f0-9]{64}", value):
        raise BootstrapError("HARNESS_BOOTSTRAP_HMAC_KEY must be exactly 64 lowercase hexadecimal characters")
    return bytes.fromhex(value)


def envelope_signature(envelope):
    unsigned = {key: value for key, value in envelope.items() if key != "signature"}
    serialized = json.dumps(unsigned, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    return hmac.new(signing_key(), serialized, hashlib.sha256).hexdigest()


def save_envelope(repo, tracking, envelope, *, complete=False):
    if complete:
        envelope["complete"] = True
    envelope["signature"] = envelope_signature(envelope)
    before, rest = tracking["body"].split(PLAN_BEGIN, 1)
    _, after = rest.split(PLAN_END, 1)
    body = before + PLAN_BEGIN + json.dumps(envelope, ensure_ascii=False) + PLAN_END + after
    if complete:
        body += "\n\n" + COMPLETE + "\n"
    edit_body(repo, tracking, body)
    tracking["body"] = body


def read_envelope(body):
    try:
        envelope = json.loads(body.split(PLAN_BEGIN, 1)[1].split(PLAN_END, 1)[0])
        if not isinstance(envelope, dict):
            raise BootstrapError("Invalid durable bootstrap envelope")
        signature = envelope.get("signature")
        if (not isinstance(signature, str) or not re.fullmatch(r"[a-f0-9]{64}", signature)
                or not hmac.compare_digest(signature, envelope_signature(envelope))):
            raise BootstrapError("Bootstrap envelope signature is invalid; restore the original signing key or untampered tracking issue")
        if not isinstance(envelope.get("complete"), bool):
            raise BootstrapError("Invalid signed bootstrap completion state")
        if not isinstance(envelope, dict) or not isinstance(envelope.get("publisher"), str):
            raise BootstrapError("Invalid durable bootstrap publisher")
        if not isinstance(envelope.get("nodes"), dict):
            raise BootstrapError("Invalid durable bootstrap bindings")
        for node_id, binding in envelope["nodes"].items():
            if (not isinstance(binding, dict) or not isinstance(binding.get("number"), int)
                    or binding["number"] < 1 or not isinstance(binding.get("url"), str)
                    or not isinstance(binding.get("digest"), str)
                    or not re.fullmatch(r"[a-f0-9]{64}", binding["digest"])):
                raise BootstrapError("Invalid durable bootstrap identity binding")
        validate(envelope["plan"])
        return envelope
    except (ValueError, KeyError, IndexError) as exc:
        raise BootstrapError("Tracking issue contains an invalid durable plan") from exc


def write_runtime_values(root, values):
    path = root / ".ai-team/runtime/runtime.env"
    lines = path.read_text().splitlines()
    pattern = re.compile(r"^(?:export\s+)?(" + "|".join(values) + r")=")
    lines = [line for line in lines if not pattern.match(line)]
    lines.extend(f"{key}={shlex.quote(value)}" for key, value in values.items())
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as handle:
        temporary = Path(handle.name)
        handle.write("\n".join(lines) + "\n")
        handle.flush()
        os.fsync(handle.fileno())
    try:
        temporary.chmod(0o600)
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)
    os.environ.update(values)


def persist_runtime(root, repo, owner, number):
    write_runtime_values(root, {"HARNESS_REPO": repo, "HARNESS_PROJECT_OWNER": owner, "HARNESS_PROJECT_NUMBER": str(number)})


def ensure_signing_key(root, existing, publisher):
    if os.environ.get("HARNESS_BOOTSTRAP_HMAC_KEY"):
        signing_key()
        return
    raise BootstrapError(
        "Bootstrap signing key is missing. Run .ai-team/bin/migrate-secrets for a new project, "
        "or restore the original external secret store when signed roots already exist.")


def planner_timeout_seconds():
    raw = os.environ.get("HARNESS_PLANNER_TIMEOUT_SECONDS", "300")
    if not re.fullmatch(r"[1-9][0-9]*", raw or ""):
        raise BootstrapError("HARNESS_PLANNER_TIMEOUT_SECONDS must be an integer from 1 to 3600")
    value = int(raw)
    if value > 3600:
        raise BootstrapError("HARNESS_PLANNER_TIMEOUT_SECONDS must be an integer from 1 to 3600")
    return value


def plan_with_provider(root, brief, timeout):
    provider = os.environ.get("HARNESS_PLANNER_PROVIDER") or os.environ.get("HARNESS_COORDINATOR_PROVIDER", "claude")
    if provider != "claude":
        raise BootstrapError("Bootstrap planning currently requires Claude's tool-free mode. Set HARNESS_PLANNER_PROVIDER=claude; Codex/Gemini remain available for implementation and review.")
    if os.environ.get(f"HARNESS_ENABLE_{provider.upper()}", "0") != "1":
        raise BootstrapError("Planner provider is disabled")
    prefix = f"HARNESS_{provider.upper()}_MODEL"
    model = os.environ.get("HARNESS_PLANNER_MODEL") or os.environ.get(prefix + "_STRONG") or os.environ.get(prefix)
    prompt = (root / ".ai-team/prompts/bootstrap-project.md").read_text() + "\nPRODUCT BRIEF (JSON string):\n" + json.dumps(brief)
    args = ["claude", "--safe-mode", "-p", "--output-format", "text", "--tools", "",
            "--permission-mode", "plan", "--no-session-persistence", "--max-turns", "1"]
    if model:
        args += ["--model", model]
    # The model has zero tools. Safe mode disables hooks, MCP, plugins and project
    # instructions; a private empty cwd and allowlisted environment provide no
    # repository context or exported harness/GitHub/decision-engine credentials.
    allowed = {"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "TERM"}
    planner_env = {name: value for name, value in os.environ.items() if name in allowed}
    print(f"Planning with Claude (timeout: {timeout}s)...", file=sys.stderr, flush=True)
    with tempfile.TemporaryDirectory(prefix="harness-planning-") as isolated:
        output = run(args, stdin=prompt, cwd=isolated, env=planner_env, timeout=timeout)
    try:
        plan = json.loads(output)
    except ValueError as exc:
        raise BootstrapError("Planner must return a JSON plan; no GitHub issues were created") from exc
    validate(plan)
    return plan


def select_project(repo, title):
    owner = os.environ.get("HARNESS_PROJECT_OWNER", "")
    if not owner or owner == "YOUR_GITHUB_USER_OR_ORG":
        owner = repo.split("/")[0]
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]{0,99}", owner):
        raise BootstrapError("Invalid GitHub Project owner")
    number = os.environ.get("HARNESS_PROJECT_NUMBER", "")
    # The example's number=1 is not a selection when the owner remains a placeholder.
    if os.environ.get("HARNESS_PROJECT_OWNER") == "YOUR_GITHUB_USER_OR_ORG":
        number = ""
    if number:
        if not number.isdigit() or int(number) < 1:
            raise BootstrapError("Invalid GitHub Project number")
        # Prove the configured Project exists before recording its identity in
        # the durable root. A typo must not strand an otherwise resumable plan.
        Project(owner, number)
        return owner, number
    projects = gh_json("project", "list", "--owner", owner, "--limit", "1000", "--format", "json")["projects"]
    matches = [p for p in projects if p["title"] == title and not p.get("closed", False)]
    if len(matches) > 1:
        raise BootstrapError("Multiple matching Projects; set HARNESS_PROJECT_NUMBER explicitly")
    project = matches[0] if matches else gh_json("project", "create", "--owner", owner, "--title", title, "--format", "json")
    return owner, str(project["number"])


def node_body(node, marker, root, urls, executable):
    body = f"{marker}\n<!-- ai-harness-node:{'executable' if executable else 'container'} -->\n"
    body += f"Bootstrap root: {issue_url(root)}\n\n## Goal and Scope\n\n{node['description']}\n"
    for heading, key in (("Acceptance Criteria", "acceptance"), ("Validation", "validation")):
        body += f"\n## {heading}\n\n" + "\n".join(f"- {value}" for value in node[key]) + "\n"
    body += "\n## Dependencies\n\n" + ("\n".join(f"- {urls[d]}" for d in node["depends_on"]) or "None")
    return body + f"\n\n## Risk\n\n{node['risk']}\n"


def route_node(root, node, issue_number):
    available = [p for p in os.environ.get("HARNESS_IMPLEMENTER_ORDER", "codex,claude,gemini").split(",")
                 if p in {"codex", "claude", "gemini"}
                 and os.environ.get(f"HARNESS_ENABLE_{p.upper()}", "0") == "1" and shutil.which(p)]
    if not available:
        raise BootstrapError("No enabled implementation provider is installed")
    state = {"issue": issue_number, "title": node["title"], "body": node["description"],
             "risk": node["risk"], "work_type": node["work_type"], "available_providers": available}
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as handle:
        json.dump(state, handle)
        handle.flush()
        result = json.loads(run([str(root / ".ai-team/bin/decide"), "task", "--state-file", handle.name]))
    choices = result["decisions"]
    provider, profile = choices["provider"]["value"], choices["model_profile"]["value"]
    if provider not in available or profile not in {"fast", "balanced", "strong"}:
        raise BootstrapError("Decision engine returned an invalid provider/model profile")
    model = os.environ.get(f"HARNESS_{provider.upper()}_MODEL_{profile.upper()}") or os.environ.get(f"HARNESS_{provider.upper()}_MODEL")
    return {"Provider": {"claude": "Claude", "codex": "OpenAI", "gemini": "Gemini"}[provider],
            "Model": model or f"profile:{profile} (CLI default)", "Retry Count": "0"}


def dependency_urls(data):
    connection = data.get("blockedBy")
    if not isinstance(connection, dict) or not isinstance(connection.get("nodes"), list):
        raise BootstrapError("Invalid GitHub dependency response")
    if connection.get("totalCount") != len(connection["nodes"]):
        raise BootstrapError("GitHub dependency response was truncated; graph remains incomplete")
    return {dep["url"].lower() for dep in connection["nodes"]}


def verify_relationships(repo, node, issue, parent, dependencies):
    data = gh_json("issue", "view", str(issue["number"]), "--repo", repo, "--json", "parent,blockedBy")
    if not data.get("parent") or data["parent"]["url"].lower() != issue_url(parent).lower():
        raise BootstrapError("Native parent relationship mismatch; graph remains incomplete")
    actual = dependency_urls(data)
    if actual != {issue_url(dep).lower() for dep in dependencies}:
        raise BootstrapError("Native dependency relationship mismatch; graph remains incomplete")


def repair_relationships(repo, issue, parent, dependencies):
    data = gh_json("issue", "view", str(issue["number"]), "--repo", repo, "--json", "parent,blockedBy")
    if data.get("parent") and data["parent"]["url"].lower() != issue_url(parent).lower():
        raise BootstrapError("Existing node has a different parent; reconcile it before resuming")
    if not data.get("parent"):
        gh("issue", "edit", str(issue["number"]), "--repo", repo, "--parent", issue_url(parent))
    actual = dependency_urls(data)
    for dependency in dependencies:
        if issue_url(dependency).lower() not in actual:
            gh("issue", "edit", str(issue["number"]), "--repo", repo,
               "--add-blocked-by", str(dependency["number"]))


def bootstrap(args, root, planner_timeout):
    if args.brief == "-":
        brief = sys.stdin.read(MAX_BRIEF + 1)
    elif args.brief:
        with open(args.brief, encoding="utf-8") as handle:
            brief = handle.read(MAX_BRIEF + 1)
    else:
        editor = shlex.split(os.environ.get("VISUAL") or os.environ.get("EDITOR") or "vi")
        with tempfile.NamedTemporaryFile(suffix=".md") as handle:
            if subprocess.run([*editor, handle.name], check=False).returncode:
                raise BootstrapError("Brief editor failed")
            brief = Path(handle.name).read_text()
    text(brief, "brief", MAX_BRIEF)
    if len(brief.encode("utf-8")) > MAX_BRIEF:
        raise BootstrapError("Brief exceeds 32000 UTF-8 bytes")
    repo = os.environ.get("HARNESS_REPO", "")
    if not repo or repo == "owner/repository":
        repo = gh_json("repo", "view", "--json", "nameWithOwner")["nameWithOwner"]
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise BootstrapError("Invalid HARNESS_REPO")
    repo = gh_json("repo", "view", repo, "--json", "nameWithOwner")["nameWithOwner"]
    if not isinstance(repo, str) or not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise BootstrapError("GitHub returned an invalid repository name")
    # Canonical trailing whitespace permits file/stdin use of the same brief.
    key = hashlib.sha256((repo.lower() + "\n" + brief.strip()).encode()).hexdigest()[:24]
    marker = f"<!-- ai-harness-bootstrap:v1:{key} -->"
    publisher = gh_json("api", "user")["login"]
    existing = issues(repo)
    has_roots = any(issue_author(issue).lower() == publisher.lower()
                    and (issue.get("body") or "").startswith("<!-- ai-harness-bootstrap:v1:")
                    for issue in existing)
    if os.environ.get("HARNESS_BOOTSTRAP_HMAC_KEY") or has_roots:
        ensure_signing_key(root, existing, publisher)
    tracking = find_marker(existing, marker)
    if tracking:
        if issue_author(tracking).lower() != publisher.lower():
            raise BootstrapError("Bootstrap root is not authored by the authenticated publisher")
        try:
            envelope = read_envelope(tracking["body"])
            plan = envelope["plan"]
            owner, number = envelope["owner"], envelope["number"]
            if envelope.get("bootstrap_id") != key or envelope.get("repo", "").lower() != repo.lower():
                raise BootstrapError("Signed bootstrap identity does not match this brief/repository")
            if envelope["publisher"].lower() != publisher.lower() or not isinstance(envelope["nodes"], dict):
                raise BootstrapError("Invalid durable bootstrap publisher/bindings")
        except (ValueError, KeyError, IndexError) as exc:
            raise BootstrapError("Tracking issue contains an invalid durable plan") from exc
        order, leaves = validate(plan)
    else:
        plan = plan_with_provider(root, brief, planner_timeout)
        order, leaves = validate(plan)
        title = args.title or plan["title"]
        text(title, "Project title", 200)
        owner, number = select_project(repo, title)
        # Do not switch a legacy checkout to signed-only mode on a planner/auth/
        # validation failure. Generate the key only immediately before publication.
        ensure_signing_key(root, existing, publisher)
        envelope = {"owner": owner, "number": number, "plan": plan, "publisher": publisher,
                    "nodes": {}, "complete": False, "bootstrap_id": key, "repo": repo}
        envelope["signature"] = envelope_signature(envelope)
        body = marker + "\n<!-- ai-harness-node:container -->\n\n"
        body += PLAN_BEGIN + json.dumps(envelope, ensure_ascii=False) + PLAN_END
        body += "\n\n" + plan["summary"]
        body += "\n\nPublication incomplete. Do not dispatch descendants until the completion marker exists.\n\n"
        tracking = create_issue(repo, f"[Project] {title}", body)
    project = Project(owner, number)
    persist_runtime(root, repo, owner, number)
    if envelope["complete"] and tracking["body"].endswith("\n\n" + COMPLETE + "\n"):
        print(f"Bootstrap already complete: {issue_url(tracking)}")
        return
    run([str(root / ".ai-team/bin/setup-github-project")])
    project.set(project.item(issue_url(tracking)), "Harness Status", "BACKLOG")
    known, urls, item_ids = {}, {}, {}
    for node in order:
        node_id = node["id"]
        node_marker = f"<!-- ai-harness-bootstrap-node:v1:{key}:{node_id} -->"
        parent = known[node["parent"]] if node["parent"] else tracking
        dependencies = [known[dep] for dep in node["depends_on"]]
        expected_body = node_body(node, node_marker, tracking, urls, node_id in leaves)
        binding = envelope["nodes"].get(node_id)
        if binding:
            issue = next((candidate for candidate in existing if candidate["number"] == binding["number"]), None)
            if not issue:
                raise BootstrapError("Durably bound issue is missing; refusing to replace its identity")
        else:
            issue = find_marker(existing, node_marker)
        if not issue:
            issue = create_issue(repo, node["title"], expected_body,
                                 issue_url(parent), [issue_url(dep) for dep in dependencies])
        else:
            verify_identity(issue, binding, publisher, node["title"], expected_body)
            # gh issue create can create the issue and fail while attaching edges.
            # Reapplying a parent and existing blocked-by edges is idempotent.
            repair_relationships(repo, issue, parent, dependencies)
        bound = verify_identity(issue, binding, publisher, node["title"], expected_body)
        if binding is None:
            envelope["nodes"][node_id] = bound
            save_envelope(repo, tracking, envelope)
        known[node_id] = issue
        urls[node_id] = issue_url(issue)
        item = project.item(urls[node_id])
        item_ids[node_id] = item
        project.set(item, "Harness Status", "BLOCKED" if node_id in leaves else "BACKLOG")
        for name, value in (("Risk", node["risk"]), ("Priority", node["priority"]),
                            ("Work Type", node["work_type"]), ("Agent Role", "Implementer" if node_id in leaves else "Planner")):
            project.set(item, name, value)
        if node_id in leaves:
            for name, value in route_node(root, node, issue["number"]).items():
                project.set(item, name, value)
        verify_relationships(repo, node, issue, parent, dependencies)
    # Re-read all edges before releasing anything. The root completion marker is the
    # dispatch gate, including across an interrupted sequence of READY updates.
    for node in order:
        current = gh_json("issue", "view", str(known[node["id"]]["number"]), "--repo", repo,
                          "--json", "number,url,title,body,author")
        verify_identity(current, envelope["nodes"][node["id"]], publisher)
        verify_relationships(repo, node, known[node["id"]], known[node["parent"]] if node["parent"] else tracking,
                             [known[dep] for dep in node["depends_on"]])
    for node in order:
        if node["id"] in leaves and not node["depends_on"]:
            project.set(item_ids[node["id"]], "Harness Status", "READY")
    save_envelope(repo, tracking, envelope, complete=True)
    print(f"Bootstrap complete: {issue_url(tracking)} ({len(order)} issues).")
    print(f"GitHub Project: {owner} / {number}")


def check_dispatch(repo, reference):
    """Fail closed for bootstrap containers and any incompletely published graph."""
    issue = gh_json("issue", "view", reference, "--repo", repo, "--json", "number,url,title,body,author")
    body = issue.get("body") or ""
    first = body.splitlines()[0] if body else ""
    if first.startswith("<!-- ai-harness-bootstrap:v1:") or "<!-- ai-harness-node:container -->" in body.splitlines():
        raise BootstrapError("Bootstrap root/container cannot be dispatched to an implementer")
    match = re.fullmatch(r"<!-- ai-harness-bootstrap-node:v1:([a-f0-9]{24}):([a-z][a-z0-9-]{0,47}) -->", first)
    if not match:
        if first.startswith("<!-- ai-harness-bootstrap"):
            raise BootstrapError("Malformed bootstrap marker")
        if os.environ.get("HARNESS_BOOTSTRAP_HMAC_KEY"):
            signing_key()
            raise BootstrapError("Bootstrap-managed projects require signed task identities; unsigned/manual implementer dispatch is disabled")
        # A marker is a locator, never the source of identity. A mapped issue
        # cannot turn into ordinary unverified work by deleting its first line.
        publisher = gh_json("api", "user")["login"]
        for candidate in issues(repo):
            candidate_body = candidate.get("body") or ""
            if (issue_author(candidate).lower() != publisher.lower()
                    or not candidate_body.startswith("<!-- ai-harness-bootstrap:v1:")):
                continue
            envelope = read_envelope(candidate_body)
            if envelope["publisher"].lower() != publisher.lower():
                raise BootstrapError("Durable bootstrap publisher mismatch")
            if envelope.get("repo", "").lower() != repo.lower():
                continue
            if candidate["number"] == issue["number"] or any(
                    binding["number"] == issue["number"] for binding in envelope["nodes"].values()):
                raise BootstrapError("Durably bound bootstrap issue has lost its canonical marker")
        return
    root_match = re.search(r"^Bootstrap root: https://github\.com/" + re.escape(repo) + r"/issues/([0-9]+)$", body, re.M)
    if not root_match:
        raise BootstrapError("Bootstrap issue is missing its root reference")
    root_issue = gh_json("issue", "view", root_match[1], "--repo", repo, "--json", "body,author")
    root_body = root_issue.get("body") or ""
    if not root_body.startswith(f"<!-- ai-harness-bootstrap:v1:{match[1]} -->\n") or not root_body.endswith("\n\n" + COMPLETE + "\n"):
        raise BootstrapError("Bootstrap graph is incomplete; resume bootstrap-project before dispatch")
    try:
        envelope = read_envelope(root_body)
        plan = envelope["plan"]
        _, leaves = validate(plan)
    except (ValueError, KeyError, IndexError) as exc:
        raise BootstrapError("Invalid bootstrap root plan") from exc
    if match[2] not in leaves:
        raise BootstrapError("Only executable bootstrap leaves may be dispatched")
    if not envelope["complete"] or set(envelope["nodes"]) != {node["id"] for node in plan["nodes"]}:
        raise BootstrapError("Signed bootstrap graph is incomplete")
    if envelope.get("bootstrap_id") != match[1] or envelope.get("repo", "").lower() != repo.lower():
        raise BootstrapError("Signed bootstrap identity does not match this issue/repository")
    publisher = gh_json("api", "user")["login"]
    if issue_author(root_issue).lower() != publisher.lower() or envelope.get("publisher", "").lower() != publisher.lower():
        raise BootstrapError("Bootstrap root publisher does not match the authenticated account")
    binding = envelope.get("nodes", {}).get(match[2])
    if not binding:
        raise BootstrapError("Bootstrap issue has no durable identity binding")
    verify_identity(issue, binding, publisher)
    node = next(n for n in plan["nodes"] if n["id"] == match[2])
    urls = {node_id: bound["url"] for node_id, bound in envelope["nodes"].items()}
    root_issue["url"] = f"https://github.com/{repo}/issues/{root_match[1]}"
    expected = node_body(node, first, root_issue, urls, True)
    verify_identity(issue, binding, publisher, node["title"], expected)
    parent = envelope["nodes"][node["parent"]] if node["parent"] else {"url": root_issue["url"]}
    dependencies = [envelope["nodes"][dep] for dep in node["depends_on"]]
    verify_relationships(repo, node, issue, parent, dependencies)


def main():
    parser = argparse.ArgumentParser(description="Plan a project brief and publish a resumable GitHub work graph.")
    parser.add_argument("--brief", help="UTF-8 brief file; '-' reads stdin; omitted opens VISUAL/EDITOR")
    parser.add_argument("--title", help="GitHub Project title (default: planner's title)")
    parser.add_argument("--start", action="store_true", help="run one coordinator cycle after publication")
    parser.add_argument("--start-timer", action="store_true", help="reserved; currently fails safely (global timer is not repo-scoped)")
    parser.add_argument("--set-field", nargs=3, metavar=("URL", "FIELD", "VALUE"), help=argparse.SUPPRESS)
    parser.add_argument("--check-dispatch", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.start_timer:
        raise BootstrapError("--start-timer is unavailable: install-systemd uses a shared unit name. Use --start and rerun coordinator-cycle; do not overwrite another project's timer.")
    if args.set_field:
        project = Project(os.environ["HARNESS_PROJECT_OWNER"], os.environ["HARNESS_PROJECT_NUMBER"])
        url, name, value = args.set_field
        project.set(project.item(url), name, value)
        return
    if args.check_dispatch:
        check_dispatch(os.environ["HARNESS_REPO"], args.check_dispatch)
        return
    planner_timeout = planner_timeout_seconds()
    root = Path(os.environ["HARNESS_ROOT"])
    lock_path = root / ".ai-team/runtime/bootstrap.lock"
    with lock_path.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise BootstrapError("Another bootstrap is running for this checkout") from exc
        bootstrap(args, root, planner_timeout)
    if args.start:
        # Stream normal coordinator progress, but never log the private planning prompt.
        raise SystemExit(subprocess.run([str(root / ".ai-team/bin/coordinator-cycle")], check=False).returncode)


if __name__ == "__main__":
    try:
        main()
    except (BootstrapError, OSError, ValueError, KeyError) as exc:
        message = str(exc) if isinstance(exc, BootstrapError) else "Invalid configuration or response; inspect your local CLI setup and durable tracking issue."
        print(f"bootstrap-project: {message}", file=sys.stderr)
        sys.exit(1)
