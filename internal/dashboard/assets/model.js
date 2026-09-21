// Read projection only. All strings remain untrusted until assigned as DOM text.
export const LIMIT = 200;
export const text = value => (typeof value === 'string' && value !== '') || typeof value === 'number' ? String(value).slice(0, 4096) : 'unknown';
export function timestamp(value) {
  if (value === null || value === undefined || value === '') return null;
  const time = typeof value === 'number' ? value * 1000 : Date.parse(value);
  return Number.isFinite(time) && time > 0 && time <= 8640000000000000 ? time : null;
}
export function freshness(data, now = Date.now()) {
  const observed = timestamp(data.observed_at ?? data.created_at);
  const expires = timestamp(data.expires_at);
  if ((expires !== null && expires <= now) || (observed !== null && now - observed > 120000)) return 'stale';
  if (observed === null || observed > now + 30000) return 'unknown';
  return 'observed';
}
export function issueLink(repo, issue, kind = 'issues') {
  if (typeof repo !== 'string' || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo) || repo.split('/').some(p => p === '.' || p === '..')) return null;
  if (!Number.isSafeInteger(issue) || issue < 1 || !['issues', 'pull'].includes(kind)) return null;
  return `https://github.com/${repo}/${kind}/${issue}`;
}
export function validEvent(event) {
  return event && Number.isSafeInteger(event.id) && event.id > 0 && typeof event.type === 'string' && event.data && typeof event.data === 'object' && !Array.isArray(event.data);
}
export function snapshot(value) {
  if (value?.version !== 1 || !Number.isSafeInteger(value.cursor) || value.cursor < 0 || !Array.isArray(value.events)) throw Error('Invalid snapshot');
  let previous = value.cursor - value.events.length;
  if (previous < 0 || (value.cursor > 0 && !value.events.length)) throw Error('Invalid snapshot window');
  for (const event of value.events) {
    if (!validEvent(event) || event.id !== ++previous) throw Error('Invalid observation sequence');
  }
  return { cursor: value.cursor, events: value.events.slice(-LIMIT) };
}
export function append(state, event) {
  if (!validEvent(event)) throw Error('Invalid observation');
  if (event.id <= state.cursor) return state;
  if (event.id !== state.cursor + 1) throw Error('Observation gap');
  return { cursor: event.id, events: [...state.events, event].slice(-LIMIT) };
}
export const key = data => JSON.stringify([text(data.repo), text(data.project), text(data.issue)]);
export function project(events) {
  const tasks = new Map(), agents = new Map(), brokers = new Map(), decisions = new Map();
  let decision = null;
  for (const event of events) {
    const d = event.data;
    if (event.type === 'pybridge.broker.observed') brokers.set(text(d.repo), d);
    if (['pybridge.task.observed', 'dashboard.task.observed'].includes(event.type)) tasks.set(key(d), d);
    if (event.type === 'dashboard.agent.observed') agents.set(JSON.stringify([key(d), text(d.agent_id)]), d);
    if (event.type === 'routing.decided') { decision = d; decisions.set(key(d), d); }
  }
  // Task routing decisions do not establish execution evidence for every agent
  // or retry on that task. Explicit agents carry their own routing observation.
  for (const values of [tasks]) {
    for (const [id, data] of values) {
      const evidence = decisions.get(key(data));
      if (evidence) values.set(id, { ...data, routing: evidence });
    }
  }
  // Bridge assignments are useful, but never imply active/successful workers.
  for (const [id, task] of tasks) {
    if (![...agents.values()].some(a => key(a) === id)) agents.set(id, { ...task, runtime_state: null });
  }
  return { tasks: [...tasks.values()], agents: [...agents.values()], brokers: [...brokers.values()], decision };
}
export function routing(data) {
  const r = data.routing ?? data;
  return {
    'Requested model': r.requested?.model ?? r.selected?.model,
    'Requested effort': r.requested?.effort ?? r.selected?.effort,
    'Effective model': r.effective?.model,
    'Effective effort': r.effective?.effort,
    'Decision source': r.decision_source,
    'Decision reason': r.reason,
    'Policy version': r.policy_version,
  };
}
