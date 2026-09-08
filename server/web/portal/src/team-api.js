/* Portal v2 team API layer — plain JS (no Babel), loads before any JSX.
   TEAM_API + the agent roster helpers move here verbatim from team.jsx so the
   write surface survives a broken view file. All GETs bypass
   PORTAL_UTIL.fetchJSON (its blind '?t=' cache-bust corrupts query strings). */
(function () {
  async function request(path, options) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 20000);
    try {
      const response = await fetch(path, Object.assign({ credentials: 'same-origin', signal: controller.signal }, options));
      if (response.status === 401 || response.redirected) return { ok: false, error: 'Your session expired. Sign in again, then retry.' };
      if (!response.ok) return { ok: false, error: 'Could not complete the request (' + response.status + '). Please try again.' };
      return { ok: true, value: response.status === 204 ? {} : await response.json() };
    } catch (error) {
      return { ok: false, error: error.name === 'AbortError' ? 'The request timed out. Check the latest state before trying again.' : 'Connection interrupted. Your draft is still here; try again when connected.' };
    } finally { clearTimeout(timer); }
  }
  const TEAM_API = {
    post: function (path, body, method) { return request(path, {method: method || 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body || {})}); },
    get: function (path) { return request(path, {cache: 'no-store'}); }
  };

  /* The taggable-agent roster — fetched once per session; a 404 from an older
     server degrades to "no agents". */
  let TEAM_AGENTS = null;
  function loadAgents() {
    if (TEAM_AGENTS) return Promise.resolve(TEAM_AGENTS);
    return TEAM_API.get('api/team/agents').then(function (r) {
      TEAM_AGENTS = (r.ok && r.value && r.value.agents) ? r.value.agents : [];
      return TEAM_AGENTS;
    });
  }

  /* Each agent expands into its bare token plus one intent-tagged variant per
     persona (agent:kairos::brief) — intent rides the mention, never ownership. */
  function agentOptions(agents, q) {
    const out = [];
    (agents || []).forEach(function (a) {
      out.push({ token: a.id, label: a.name, harness: a.harness });
      (a.personas || []).forEach(function (pi) {
        out.push({ token: a.id + '::' + pi, label: a.name + '::' + pi, harness: a.harness });
      });
    });
    const ql = (q || '').toLowerCase();
    return out.filter(function (o) {
      return !ql || o.label.toLowerCase().indexOf(ql) >= 0 || o.token.toLowerCase().indexOf(ql) >= 0;
    });
  }

  /* v2 lazy per-item fetches */
  function loadPanel(id) {
    return TEAM_API.get('api/team/panel?id=' + encodeURIComponent(id));
  }
  function loadActivity(id) {
    return TEAM_API.get('api/team/activity?item=' + encodeURIComponent(id));
  }
  /* savePlanSections posts ONLY the changed sections; resolves with the last
     panel payload (the server echoes it) or the first error. */
  function savePlanSections(id, sections) {
    const names = Object.keys(sections);
    if (!names.length) return Promise.resolve({ ok: true, none: true });
    let chain = Promise.resolve({ ok: true });
    names.forEach(function (name) {
      chain = chain.then(function (prev) {
        if (!prev.ok) return prev;
        return TEAM_API.post('api/team/plan', { item: id, section: name, text: sections[name] });
      });
    });
    return chain;
  }
  /* planSlug mirrors server/todo_plans.go planSlug: ':'/'/' → '-', 64 cap. */
  function planSlug(id) {
    let s = String(id || '').replace(/[:\/]/g, '-').toLowerCase();
    s = s.replace(/[^a-z0-9-]+/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
    return s.slice(0, 64);
  }

  window.TEAM_API = TEAM_API;
  Object.assign(window, {
    loadAgents: loadAgents,
    agentOptions: agentOptions,
    PORTAL_TEAM: { loadPanel: loadPanel, loadActivity: loadActivity, savePlanSections: savePlanSections, planSlug: planSlug }
  });
})();
