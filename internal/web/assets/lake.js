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
  const polling = document.body.dataset.poll === 'true';
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
      live.replaceWith(updated);
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
  if (target) target.focus({preventScroll: false});
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
