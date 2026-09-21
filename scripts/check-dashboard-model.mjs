// Dependency-free projection/security checks. Full DOM behavior is checked by
// check-dashboard.mjs in the provisioned browser environment.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const source = await readFile(new URL('../internal/dashboard/assets/model.js', import.meta.url), 'utf8');
const { append, freshness, issueLink, project, routing, snapshot, text } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const attack = '<img src=x onerror="window.pwned=1"><script>alert(1)</script>';
assert.equal(text(attack), attack); // Preserve as text; never interpret as markup.
assert.equal(text(''), 'unknown');
assert.equal(text({ html: attack }), 'unknown');
assert.equal(issueLink('owner/repo', 13), 'https://github.com/owner/repo/issues/13');
assert.equal(issueLink('owner/repo', 4, 'pull'), 'https://github.com/owner/repo/pull/4');
for (const repo of ['javascript:alert(1)', '//evil.test/a', 'owner/repo?x=1', '../repo', 'owner/..', attack]) assert.equal(issueLink(repo, 1), null);
assert.equal(issueLink('owner/repo', '1/../../evil'), null);
assert.equal(issueLink('owner/repo', 1, 'files'), null);
const now = 1700000000000;
assert.equal(freshness({}, now), 'unknown');
assert.equal(freshness({ observed_at: now / 1000 }, now), 'observed');
assert.equal(freshness({ observed_at: now / 1000 - 121 }, now), 'stale');
assert.equal(freshness({ expires_at: now / 1000 - 1 }, now), 'stale');
assert.equal(freshness({ observed_at: now / 1000 + 40 }, now), 'unknown');
assert.equal(freshness({ observed_at: 'malicious' }, now), 'unknown');
const task = { repo: 'owner/repo', project: 'owner/7', issue: 13, title: attack, status: 'IN_PROGRESS' };
const initial = { id: 1, type: 'pybridge.task.observed', data: task };
let state = snapshot({ version: 1, cursor: 1, events: [initial] });
assert.equal(project(state.events).agents[0].runtime_state, null);
assert.equal(project(state.events).tasks[0].title, attack);
const decision = { ...task, selected: { model: 'requested-model', effort: 'high' }, decision_source: 'rules', reason: 'policy choice', policy_version: 'v1' };
state = append(state, { id: 2, type: 'routing.decided', data: decision });
assert.equal(routing(project(state.events).agents[0])['Requested model'], 'requested-model');
assert.equal(routing(project(state.events).agents[0])['Effective model'], undefined);
state = append(state, { id: 3, type: 'dashboard.agent.observed', data: { ...task, agent_id: 'agent', runtime_state: 'running', observed_at: now / 1000 } });
assert.equal(project(state.events).agents.length, 1);
assert.equal(project(state.events).agents[0].runtime_state, 'running');
assert.equal(routing(project(state.events).agents[0])['Effective model'], undefined);
assert.equal(routing(project(state.events).agents[0])['Requested model'], undefined); // No routing attribution across agents/retries.
assert.equal(append(state, initial), state);
assert.throws(() => append(state, { ...initial, id: 5 }), /gap/);
assert.throws(() => snapshot({ version: 1, cursor: 2, events: [initial] }), /sequence/);
assert.throws(() => snapshot({ version: 1, cursor: 2, events: [] }), /window/);
for (let id = 4; id <= 220; id++) state = append(state, { ...initial, id });
assert.equal(state.events.length, 200);
assert.equal(state.events[0].id, 21);
const isolation = project([initial, { ...initial, id: 2, data: { ...task, repo: 'other/repo' } }, { ...initial, id: 3, data: { ...task, project: 'other/7' } }]);
assert.equal(isolation.tasks.length, 3);
assert.equal(isolation.agents.length, 3);
console.log('PASS: projection, routing provenance, unknown/stale, cursor recovery, bounds and constrained links');
