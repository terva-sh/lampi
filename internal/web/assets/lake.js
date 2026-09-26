// Progressive enhancement only: forms, navigation and tables work without JS.
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
