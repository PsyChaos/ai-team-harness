import { append, freshness, issueLink, key, project, routing, snapshot, text, timestamp } from './model.js';

const $ = id => document.getElementById(id);
const tabs = [...document.querySelectorAll('[role="tab"]')];
function select(tab) {
  for (const item of tabs) {
    const active = item === tab;
    item.setAttribute('aria-selected', String(active));
    item.tabIndex = active ? 0 : -1;
    $(item.getAttribute('aria-controls')).hidden = !active;
  }
  document.querySelector('.skip').href = `#${tab.getAttribute('aria-controls')}`;
}
for (const tab of tabs) {
  tab.addEventListener('click', () => select(tab));
  tab.addEventListener('keydown', event => {
    const index = tabs.indexOf(tab);
    const next = { ArrowRight: (index + 1) % tabs.length, ArrowLeft: (index + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault(); select(tabs[next]); tabs[next].focus();
  });
}
// Never insert observation strings as markup or use supplied URLs/HTML.
function element(tag, value, className) {
  const node = document.createElement(tag);
  if (value !== undefined) node.textContent = text(value);
  if (className) node.className = className;
  return node;
}
function fields(values) {
  const list = element('dl');
  for (const [name, value] of Object.entries(values)) list.append(element('dt', name), element('dd', value));
  return list;
}
const date = value => timestamp(value) === null ? 'unknown' : new Date(timestamp(value)).toISOString();
let state = { cursor: 0, events: [] }, identity = {}, mode = false, connection = 'Connecting';
let stream, retry, controller, generation = 0, selectedTask = null, filter = 'ALL';
const patterns = { ALL: /./, ASSIGN: /ASSIGN|READY|WORKER|TASK|AGENT/i, REVIEW: /REVIEW/i, CI: /CI[_.]|VERIF/i, MERGE: /MERGE|PR_|COMPLETED|UNBLOCK/i, HUMAN: /HUMAN/i };
function runtime(data) {
  const fresh = freshness(data);
  if (!mode && connection !== 'Live') return 'stale / unknown · connection unavailable';
  if (fresh !== 'observed') return `${fresh} · last reported: ${text(data.runtime_state)}`;
  return text(data.runtime_state);
}
function agent(data) {
  const card = element('article', undefined, 'agent-card');
  card.append(element('h3', data.agent_id ?? `Assignment #${text(data.issue)}`), element('p', runtime(data), 'state'), fields({
    Repository: data.repo, Project: data.project, Task: data.issue, Role: data.role, Provider: data.provider,
    ...routing(data), 'Observed at': date(data.observed_at), 'Started at': date(data.started_at), 'Finished at': date(data.finished_at),
    'Expires at': date(data.expires_at), Activity: data.activity,
  }));
  return card;
}
function eventRow(event) {
  const d = event.data, row = element('div', undefined, 'event-row');
  row.append(element('span', date(d.observed_at ?? d.created_at)), element('span', event.type),
    element('span', `${text(d.repo)} · #${text(d.issue)} · ${text(d.message ?? d.activity ?? d.reason ?? d.status)}`));
  return row;
}
function taskButton(task) {
  const button = element('button', `#${text(task.issue)} · ${text(task.title)}`);
  button.type = 'button'; button.dataset.focus = key(task);
  button.addEventListener('click', () => { selectedTask = key(task); renderDetail(); $('task-detail').focus(); });
  return button;
}
function renderDetail() {
  $('task-detail').hidden = selectedTask === null;
  if (selectedTask === null) return;
  const view = project(state.events), task = view.tasks.find(t => key(t) === selectedTask);
  const content = $('detail-content'); content.replaceChildren();
  if (!task) { content.append(element('p', 'Unknown — task is outside the retained observation window.')); return; }
  $('detail-title').textContent = `Task #${text(task.issue)} · ${text(task.title)}`;
  content.append(fields({ Repository: task.repo, Project: task.project, Status: task.status, Freshness: freshness(task),
    'Observed at': date(task.observed_at), 'Issue text': task.body, 'PR title': task.pr_title, 'PR text': task.pr_body, ...routing(task) }));
  for (const [label, number, kind] of [['Issue on GitHub', task.issue, 'issues'], ['PR on GitHub', task.pr, 'pull']]) {
    const href = issueLink(task.repo, number, kind);
    if (href) { const link = element('a', label); link.href = href; link.rel = 'noopener noreferrer'; content.append(link, element('br')); }
  }
  for (const a of view.agents.filter(a => key(a) === selectedTask)) content.append(agent(a));
  content.append(element('h3', 'Task activity · latest 20'));
  for (const e of state.events.filter(e => key(e.data) === selectedTask).slice(-20).reverse()) content.append(eventRow(e));
}
$('close-detail').addEventListener('click', () => { selectedTask = null; renderDetail(); tabs.find(t => t.getAttribute('aria-selected') === 'true').focus(); });
function render() {
  const focusedTask = document.activeElement?.dataset?.focus;
  const focusedLink = document.activeElement?.tagName === 'A' ? document.activeElement.getAttribute('href') : null;
  const view = project(state.events);
  $('connection').textContent = mode ? 'Demo · simulated' : connection;
  $('mode-notice').textContent = mode ? 'Demo mode — all displayed observations are simulated.' : 'Live observations — bounded journal window. No execution controls.';
  const repos = [...new Set(state.events.map(e => e.data.repo).filter(v => typeof v === 'string'))];
  const projects = [...new Set(state.events.map(e => e.data.project).filter(v => typeof v === 'string' || typeof v === 'number'))];
  $('identity').textContent = `Installation: ${location.origin} · Repository: ${text(identity.repo)} · Project: ${text(identity.project)} · Observed repositories: ${repos.map(text).join(', ') || 'unknown'} · Observed projects: ${projects.map(text).join(', ') || 'unknown'}`;
  $('journal-state').textContent = `Cursor ${state.cursor} · ${state.events.length} retained events · ${view.brokers.map(b => `${text(b.repo)}: ${freshness(b)}, ${date(b.created_at)}`).join(' · ') || 'source timestamp unknown'}`;
  $('decision').replaceChildren(view.decision ? fields(routing(view.decision)) : element('p', 'Unknown — no routing evidence observed.'));
  const lanes = [['Queued', /^(BACKLOG|PLANNING|READY)$/], ['Implementing', /^(CLAIMED|IN_PROGRESS|IMPLEMENTED)$/], ['Review', /^(REVIEWING|CHANGES_REQUESTED)$/], ['Verification', /^VERIFIED$/], ['Merge', /^(MERGE_READY|MERGING)$/], ['Done', /^DONE$/], ['Blocked / unknown', null]];
  $('task-flow').replaceChildren();
  for (const [label, match] of lanes) {
    const lane = element('article', undefined, 'lane'); lane.append(element('h2', label));
    const tasks = view.tasks.filter(t => match ? match.test(t.status) : !lanes.some(([, re]) => re?.test(t.status)));
    if (!tasks.length) lane.append(element('p', 'No observations'));
    for (const task of tasks) { const card = element('div', undefined, 'task-card'); card.append(taskButton(task), element('p', `${text(task.repo)} · ${text(task.status)} · ${freshness(task)}`)); lane.append(card); }
    $('task-flow').append(lane);
  }
  for (const id of ['agent-list', 'factory-agents']) {
    $(id).replaceChildren(...view.agents.slice(0, id === 'factory-agents' ? 4 : 200).map(agent));
    if (!view.agents.length) $(id).append(element('p', 'Unknown — no agent observations.'));
  }
  const events = [...state.events].reverse().filter(e => patterns[filter].test(e.type));
  $('event-list').replaceChildren(...events.map(eventRow)); $('events-empty').hidden = events.length > 0;
  $('event-rail').replaceChildren(...state.events.slice(-20).reverse().map(eventRow));
  renderDetail();
  if (focusedTask) [...document.querySelectorAll('[data-focus]')].find(n => n.dataset.focus === focusedTask)?.focus();
  if (focusedLink) [...document.querySelectorAll('a')].find(n => n.getAttribute('href') === focusedLink)?.focus();
}
for (const button of document.querySelectorAll('[data-filter]')) button.addEventListener('click', () => {
  filter = button.dataset.filter;
  for (const item of document.querySelectorAll('[data-filter]')) item.setAttribute('aria-pressed', String(item === button));
  render();
});
function stop() {
  generation++; clearTimeout(retry); controller?.abort(); stream?.close(); stream = null;
}
function reconnect(token) {
  if (token !== generation || mode) return;
  stop(); connection = navigator.onLine ? 'Reconnecting · stale / unknown' : 'Offline · stale / unknown'; render();
  retry = setTimeout(connect, 2000);
}
async function connect() {
  stop(); if (mode) return;
  const token = generation;
  const request = new AbortController(); controller = request;
  const timeout = setTimeout(() => request.abort(), 10000);
  try {
    const response = await fetch('/snapshot', { cache: 'no-store', signal: request.signal });
    if (!response.ok) throw Error('Snapshot unavailable');
    const next = snapshot(await response.json());
    if (token !== generation) return;
    state = next; connection = 'Connecting stream · unknown'; render(); loadIdentity();
    stream = new EventSource(`/events?cursor=${state.cursor}`);
    stream.onopen = () => { if (token === generation) { connection = 'Live'; render(); } };
    stream.addEventListener('observation', event => {
      if (token !== generation) return;
      try { state = append(state, JSON.parse(event.data)); render(); } catch { reconnect(token); }
    });
    stream.addEventListener('reset', () => reconnect(token));
    stream.onerror = () => reconnect(token);
  } catch { reconnect(token); } finally { clearTimeout(timeout); }
}
$('demo').checked = false;
$('demo').addEventListener('change', () => {
  mode = $('demo').checked; stop(); selectedTask = null; identity = {};
  state = { cursor: 0, events: [] };
  if (mode) {
    identity = { repo: 'demo/factory', project: 'Demo project' };
    state = snapshot({ version: 1, cursor: 2, events: [
      { id: 1, type: 'dashboard.task.observed', data: { repo: 'demo/factory', project: 'Demo project', issue: 13, title: 'Demo live dashboard', status: 'IN_PROGRESS', role: 'implementer', provider: 'codex', observed_at: Date.now() / 1000 } },
      { id: 2, type: 'dashboard.agent.observed', data: { repo: 'demo/factory', project: 'Demo project', issue: 13, agent_id: 'DEMO-01', role: 'implementer', provider: 'codex', runtime_state: 'simulated running', observed_at: Date.now() / 1000, activity: 'Demo fixture only' } },
    ] });
    render();
  } else { render(); connect(); loadIdentity(); }
});
async function loadIdentity() {
  const token = generation;
  try {
    const response = await fetch('/identity', { cache: 'no-store' });
    if (!response.ok) return;
    const value = await response.json();
    if (!mode && token === generation && value && typeof value === 'object') { identity = value; render(); }
  } catch { /* Identity remains explicitly unknown. */ }
}
window.addEventListener('offline', () => { if (!mode) reconnect(generation); });
window.addEventListener('online', () => { if (!mode) connect(); });
// Age labels advance even when an otherwise healthy stream has no new events.
setInterval(render, 15000);
render(); connect(); loadIdentity();
