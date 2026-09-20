// Presentation only: all content is a fixed Demo fixture embedded in the binary.
const tabs = [...document.querySelectorAll('[role="tab"]')];
function select(tab) {
  for (const item of tabs) {
    const active = item === tab;
    item.setAttribute('aria-selected', String(active));
    item.tabIndex = active ? 0 : -1;
    document.getElementById(item.getAttribute('aria-controls')).hidden = !active;
  }
  document.querySelector('.skip').href = `#${tab.getAttribute('aria-controls')}`;
}
for (const tab of tabs) {
  tab.addEventListener('click', () => select(tab));
  tab.addEventListener('keydown', event => {
    const index = tabs.indexOf(tab);
    const next = { ArrowRight: (index + 1) % tabs.length,
      ArrowLeft: (index + tabs.length - 1) % tabs.length,
      Home: 0, End: tabs.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    select(tabs[next]);
    tabs[next].focus();
  });
}
const filters = [...document.querySelectorAll('[data-filter]')];
const patterns = { ALL: /./, ASSIGN: /ASSIGNED|READY|WORKER/, REVIEW: /REVIEW/,
  CI: /CI_/, MERGE: /MERGE|PR_|COMPLETED|UNBLOCK/, HUMAN: /HUMAN/ };
const empty = document.createElement('p');
empty.textContent = 'No Demo events in this category.';
empty.hidden = true;
empty.setAttribute('role', 'status');
document.getElementById('events').append(empty);
function filterEvents(button) {
  for (const item of filters) item.setAttribute('aria-pressed', String(item === button));
  let visible = 0;
  for (const row of document.querySelectorAll('#events .event-row')) {
    // The header has no event kind and remains visible.
    const kind = row.children[1]?.textContent.trim();
    if (kind === 'EVENT') continue;
    row.hidden = !patterns[button.textContent.trim()].test(kind);
    if (!row.hidden) visible++;
  }
  empty.hidden = visible > 0;
}
for (const button of filters) button.addEventListener('click', () => filterEvents(button));
filterEvents(filters[0]);
