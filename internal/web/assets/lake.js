// Progressive enhancement only: forms, navigation and tables work without JS.
// A chart wider than a phone scrolls; start it at the newest bucket.
function showLatest(root) {
  root.querySelectorAll('.chart-scroll').forEach(el => { el.scrollLeft = el.scrollWidth; });
}
showLatest(document);
(() => {
  const button = document.getElementById('refresh');
  const status = document.getElementById('refresh-status');
  const login = document.getElementById('sign-in-again');
  if (!button || !status) return;
  button.hidden = false;
  let polling = document.body.dataset.poll === 'true';
  let timer, paused = false, busy = false;
  function schedule() {
    clearTimeout(timer);
    if (polling && !paused && !document.hidden) timer = setTimeout(() => refresh(false), 25000);
  }
  async function refresh(manual) {
    if (busy || (!manual && document.hidden)) return;
    const live = document.querySelector('[data-live]');
    // Preserve keyboard focus rather than replacing the link being read.
    if (!manual && live.contains(document.activeElement)) { schedule(); return; }
    busy = true;
    button.disabled = true;
    clearTimeout(timer);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const res = await fetch(location.href, {headers: {'X-Lampi-Refresh': '1'}, credentials: 'same-origin', redirect: 'error', signal: controller.signal});
      if (res.status === 401 || res.status === 403) {
        login.hidden = false;
        throw new Error('Your session has ended or no longer has access. Sign in again.');
      }
      if (!res.ok) throw new Error('Refresh failed. Showing the last successful view. Try Refresh now.');
      const parsed = new DOMParser().parseFromString(await res.text(), 'text/html');
      const updated = parsed.querySelector('[data-live]');
      if (!updated) throw new Error('Refresh failed. Showing the last successful view.');
      // Keep a table the reader opened open across the refresh.
      live.querySelectorAll('details[id][open]').forEach(d => { const next = updated.querySelector('#' + d.id); if (next) next.open = true; });
      live.replaceWith(updated);
      polling = parsed.body.dataset.poll === 'true';
      document.body.dataset.poll = String(polling);
      showLatest(updated);
      paused = false;
      status.textContent = polling ? 'Up to date. Refreshes every 25 seconds while this tab is visible.' : 'Up to date. This page stays still while you read.';
    } catch (error) {
      paused = true;
      status.textContent = error.name === 'AbortError' ? 'Refresh timed out. Showing the last successful view. Try Refresh now.' : (error.message || 'Refresh failed. Showing the last successful view.');
    } finally {
      clearTimeout(timeout);
      busy = false;
      button.disabled = false;
      schedule();
    }
  }
  button.addEventListener('click', () => refresh(true));
  document.addEventListener('visibilitychange', schedule);
  schedule();
})();
// A deep link names one event. Move keyboard focus there as well as
// the scroll position, so the next Tab continues from it.
(() => {
  const target = document.querySelector('.event.target') || (location.hash.startsWith('#e-') && document.getElementById(location.hash.slice(1)));
  if (!target) return;
  // A link into a folded run of quiet events unfolds it first; the
  // server does this for ?at=, but not for a bare #e-N.
  const run = target.closest('details');
  if (run) run.open = true;
  target.focus({preventScroll: false});
})();
// Copy-out: select events on a transcript page and copy the span as
// plain text. Without JavaScript the page links its own plain text.
(() => {
  const bar = document.getElementById('excerpt-bar');
  if (!bar) return;
  const events = Array.from(document.querySelectorAll('.event[id^="e-"]'));
  if (!events.length) return;
  const range = document.getElementById('excerpt-range');
  const copy = document.getElementById('excerpt-copy');
  const open = document.getElementById('excerpt-open');
  const {uid, gen} = bar.dataset;
  const boxes = [];
  let last = null;
  events.forEach(li => {
    const pos = Number(li.id.slice(2));
    const label = document.createElement('label');
    label.className = 'select-event';
    const box = document.createElement('input');
    box.type = 'checkbox';
    box.dataset.pos = pos;
    box.setAttribute('aria-label', 'Select event ' + pos);
    label.append(box);
    li.querySelector('.event-head').prepend(label);
    boxes.push(box);
    box.addEventListener('click', e => {
      // Shift-click fills the run from the last box touched.
      if (e.shiftKey && last !== null) {
        const [a, b] = [Math.min(last, pos), Math.max(last, pos)];
        boxes.forEach(x => { const p = Number(x.dataset.pos); if (p >= a && p <= b) x.checked = box.checked; });
      }
      last = pos;
      update();
    });
  });
  bar.hidden = false;
  function span() {
    const picked = boxes.filter(b => b.checked).map(b => Number(b.dataset.pos));
    if (!picked.length) return null;
    const from = Math.min(...picked), to = Math.max(...picked);
    return {from, count: to - from + 1};
  }
  function query(s) {
    return '?gen=' + encodeURIComponent(gen) + '&from=' + s.from + '&count=' + s.count;
  }
  function update() {
    const s = span();
    copy.disabled = !s;
    open.hidden = !s;
    if (!s) { range.textContent = 'Select events to copy them as text.'; return; }
    range.textContent = s.count === 1 ? `Event #${s.from} selected.` : `Events #${s.from} to #${s.from + s.count - 1} selected (${s.count}).`;
    open.href = '/sessions/' + encodeURIComponent(uid) + '/excerpt' + query(s);
  }
  copy.addEventListener('click', async () => {
    const s = span();
    if (!s) return;
    copy.disabled = true;
    let fetched = false;
    try {
      const res = await fetch('/api/web/v1/sessions/' + encodeURIComponent(uid) + '/excerpt' + query(s), {credentials: 'same-origin', redirect: 'error'});
      if (res.status === 409) throw new Error('This transcript changed. Reload the page and select again.');
      if (res.status === 401 || res.status === 403) throw new Error('Your session has ended. Sign in again.');
      if (!res.ok) throw new Error('The excerpt could not be read.');
      const ex = await res.json();
      fetched = true;
      await navigator.clipboard.writeText(ex.text);
      range.textContent = ex.truncated
        ? `Copied events #${ex.from} to #${ex.to - 1}; the rest was over the size limit.`
        : `Copied ${ex.events} event${ex.events === 1 ? '' : 's'} as text.`;
    } catch (error) {
      // A read failure has its own message. A clipboard refusal (no
      // permission, or an insecure origin) leaves the plain page.
      range.textContent = fetched || !error.message ? 'Copy failed. Use Open as text and copy from there.' : error.message;
    } finally {
      copy.disabled = false;
    }
  });
  document.getElementById('excerpt-clear').addEventListener('click', () => { boxes.forEach(b => { b.checked = false; }); last = null; update(); });
})();
// Copy buttons for a freshly minted registration code. Without
// JavaScript, or without clipboard access, the text is there to select.
(() => {
  if (!navigator.clipboard) return;
  for (const button of document.querySelectorAll('button[data-copy]')) {
    const source = document.getElementById(button.dataset.copy);
    if (!source) continue;
    const label = button.textContent;
    button.hidden = false;
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(source.textContent);
        button.textContent = 'Copied';
      } catch {
        button.textContent = 'Copy failed. Select the text instead.';
      }
      setTimeout(() => { button.textContent = label; }, 2500);
    });
  }
})();

// Theme toggle: system, light, dark, in turn. The choice is kept in
// localStorage and applied before paint by theme.js. Without scripts
// the button stays hidden and the page follows the system.
(function () {
  var button = document.getElementById("theme");
  if (!button) return;
  var order = ["system", "light", "dark"];
  var root = document.documentElement;
  function current() {
    return root.dataset.theme === "light" || root.dataset.theme === "dark" ? root.dataset.theme : "system";
  }
  function show() {
    var t = current();
    button.textContent = "Theme: " + t;
    button.setAttribute("aria-label", "Theme: " + t + ". Change theme");
  }
  button.addEventListener("click", function () {
    var next = order[(order.indexOf(current()) + 1) % order.length];
    try {
      if (next === "system") localStorage.removeItem("lampi-theme");
      else localStorage.setItem("lampi-theme", next);
    } catch (e) {}
    if (next === "system") delete root.dataset.theme;
    else root.dataset.theme = next;
    show();
  });
  show();
  button.hidden = false;
})();
// Select all: a header checkbox ticks every row checkbox in its table
// bound to its form. The review queue holds one table per group, and
// ticking one group's header leaves the others alone.
document.querySelectorAll('input[data-select-all]').forEach(all => {
  const scope = all.closest('table') || document;
  const rows = () => scope.querySelectorAll(`input[type=checkbox][name=key][form="${all.dataset.selectAll}"]`);
  all.hidden = false;
  all.addEventListener('change', () => rows().forEach(box => { box.checked = all.checked; }));
});
// Rule filter: narrows a profile editor's rule list to the rules whose
// values hold the text. Group headers hide while a filter is set, since
// the rules under one may be hidden.
document.querySelectorAll('[data-rule-tools]').forEach(tools => {
  const list = tools.nextElementSibling;
  const box = tools.querySelector('[data-rule-filter]');
  const count = tools.querySelector('[data-rule-count]');
  if (!list || !box) return;
  const apply = () => {
    const q = box.value.trim().toLowerCase();
    let shown = 0, total = 0;
    list.querySelectorAll('[data-rule]').forEach(row => {
      const text = Array.from(row.querySelectorAll('input[name$=".value"]')).map(i => i.value.toLowerCase()).join(' ');
      const blank = row.classList.contains('rule-blank');
      const hit = !q || blank || text.includes(q);
      row.hidden = !hit;
      if (!blank) { total++; if (hit) shown++; }
    });
    list.querySelectorAll('[data-rule-group]').forEach(g => { g.hidden = q !== ''; });
    count.textContent = q ? `${shown} of ${total}` : '';
  };
  box.addEventListener('input', apply);
  tools.hidden = false;
});
// Drag to move: a rule's handle moves it within its list, and the rows'
// field names are numbered again in their new order, which the next
// preview keeps. Group headers hide, since they no longer describe the
// rows below them until the preview draws them again. The doctor's fix
// buttons name rules by their place in the order the page was drawn
// with, so they are disabled until the preview offers them again.
document.querySelectorAll('.rule-list').forEach(list => {
  const sort = list.parentElement.querySelector('[data-rule-moved]');
  const form = list.closest('form');
  let dragged = null;
  const renumber = () => {
    list.querySelectorAll('[data-rule]').forEach((row, i) => {
      row.querySelectorAll('[name]').forEach(el => { el.name = el.name.replace(/^(allow|deny)\.\d+\./, `$1.${i}.`); });
      row.querySelectorAll('button[name="remove"], button[name="move"], input[name="pick"]').forEach(el => { el.value = el.value.replace(/^(allow|deny)\.\d+(?=\.|$)/, `$1.${i}`); });
    });
    if (form) {
      form.querySelectorAll('button[name="narrow"], button[name="fold"], button[name="folder"], button[name="doctor"]').forEach(b => {
        b.disabled = true;
        b.title = 'Preview first: the rules moved';
      });
    }
    list.querySelectorAll('[data-rule-group]').forEach(g => { g.hidden = true; });
    if (sort) sort.hidden = false;
  };
  list.addEventListener('dragstart', e => {
    const handle = e.target.closest && e.target.closest('.rule-handle');
    if (!handle) return;
    dragged = handle.closest('[data-rule]');
    dragged.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', '');
  });
  list.addEventListener('dragover', e => {
    const row = e.target.closest && e.target.closest('[data-rule]');
    if (!dragged || !row || row === dragged) return;
    e.preventDefault();
    list.querySelectorAll('.drop-before').forEach(r => r.classList.remove('drop-before'));
    row.classList.add('drop-before');
  });
  list.addEventListener('drop', e => {
    const row = e.target.closest && e.target.closest('[data-rule]');
    if (!dragged || !row || row === dragged) return;
    e.preventDefault();
    row.before(dragged);
    renumber();
  });
  list.addEventListener('dragend', () => {
    list.querySelectorAll('.drop-before').forEach(r => r.classList.remove('drop-before'));
    if (dragged) dragged.classList.remove('dragging');
    dragged = null;
  });
  list.classList.add('can-drag');
});
// Enter in the merge panel's prefix checks the prefix, as its button
// does, rather than previewing the form unchanged.
document.querySelectorAll('[data-enter="merge-check"]').forEach(box => {
  const check = box.form && box.form.querySelector('[data-merge-check]');
  if (!check) return;
  box.addEventListener('keydown', e => {
    if (e.key === 'Enter') { e.preventDefault(); check.click(); }
  });
});
