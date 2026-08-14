/* Portal utility helpers — pure JS, loaded before the JSX components.
   Exposes window.PORTAL_UTIL (fetch / parse / goal-graph helpers plus
   quarter/date helpers for the timeline board + drawer). */

(function () {
  'use strict';

  /* ─── fetch (never throws; ?t= cache-bust, the /dd pattern) ─── */

  function fetchJSON(path) {
    return fetch(path + '?t=' + Date.now())
      .then(function (r) {
        if (!r.ok) return { ok: false, error: 'missing' };
        return r.json()
          .then(function (v) { return { ok: true, value: v }; })
          .catch(function () { return { ok: false, error: 'drift' }; });
      })
      .catch(function () { return { ok: false, error: 'missing' }; });
  }

  function fetchText(path) {
    return fetch(path + '?t=' + Date.now())
      .then(function (r) {
        if (!r.ok) return { ok: false, error: 'missing' };
        return r.text()
          .then(function (v) { return { ok: true, value: v }; })
          .catch(function () { return { ok: false, error: 'drift' }; });
      })
      .catch(function () { return { ok: false, error: 'missing' }; });
  }

  /* ─── content/*.md — Obsidian-style inline fields [key:: value] ─── */

  var FIELD_RE = /\[(\w+)::\s*([^\]]*)\]/g;

  function parseInlineFields(line) {
    var fields = {};
    var rest = line.replace(FIELD_RE, function (_, k, v) {
      fields[k] = v.trim();
      return '';
    }).replace(/\s{2,}/g, ' ').trim();
    return { fields: fields, rest: rest };
  }

  // Parse an entire md file into rows: skip blanks and # headers; each
  // remaining line becomes { fields, rest }. Never throws.
  function parseFieldFile(text) {
    var rows = [];
    String(text).split('\n').forEach(function (line) {
      var t = line.trim();
      if (!t || t.charAt(0) === '#') return;
      t = t.replace(/^[-*]\s+/, '');
      rows.push(parseInlineFields(t));
    });
    return rows;
  }

  /* ─── inline [label](url) links → React nodes (dd.js setTextWithLinks) ─── */

  function renderInline(text) {
    var linkRe = /\[([^\]]+)\]\(([^)]+)\)/g;
    var out = [];
    var lastIdx = 0;
    var match;
    var key = 0;
    while ((match = linkRe.exec(text)) !== null) {
      if (match.index > lastIdx) out.push(text.slice(lastIdx, match.index));
      out.push(React.createElement('a', {
        key: 'l' + (key++), href: match[2], target: '_blank', rel: 'noopener'
      }, match[1]));
      lastIdx = linkRe.lastIndex;
    }
    if (lastIdx < text.length) out.push(text.slice(lastIdx));
    return out.length ? out : [text];
  }

  /* ─── goal graph (goals.json) ─── */

  function slugify(s) {
    return String(s || '').toLowerCase().trim()
      .replace(/[\s_]+/g, '-').replace(/[^a-z0-9/-]/g, '');
  }

  function buildGoalIndex(goalsDoc) {
    var goals = (goalsDoc && Array.isArray(goalsDoc.goals)) ? goalsDoc.goals : [];
    var byId = {};
    var bySlug = {};
    goals.forEach(function (g) {
      if (!g || !g.id) return;
      byId[g.id] = g;
      bySlug[slugify(g.id)] = g.id;
      bySlug[slugify(g.id.replace(/^aion\//, ''))] = g.id;
    });
    // optional per-goal alias lists (manifest-published) — real ids win over
    // aliases, first goal wins a contested alias
    goals.forEach(function (g) {
      if (!g || !g.id) return;
      (g.aliases || []).forEach(function (a) {
        var s = slugify(a);
        if (s && !bySlug[s]) bySlug[s] = g.id;
      });
    });
    // Parent linkage is 1:many — a rock can serve several 1-year goals.
    // Prefer `serves_all` (full list), fall back to `serves` (first entry,
    // kept for back-compat), and merge in parents derived from `children`
    // arrays (manifest exports omit serves on 30-day goals).
    var parentsOf = {};
    var addParent = function (child, parent) {
      if (!byId[child] || !byId[parent] || child === parent) return;
      var arr = parentsOf[child] = parentsOf[child] || [];
      if (arr.indexOf(parent) === -1) arr.push(parent);
    };
    goals.forEach(function (g) {
      var listed = Array.isArray(g.serves_all) && g.serves_all.length
        ? g.serves_all
        : (g.serves ? [g.serves] : []);
      listed.forEach(function (p) { addParent(g.id, p); });
    });
    goals.forEach(function (g) {
      (g.children || []).forEach(function (k) { addParent(k, g.id); });
    });
    var childrenOf = {};
    var addChild = function (parent, child) {
      if (!byId[parent] || !byId[child] || parent === child) return;
      var arr = childrenOf[parent] = childrenOf[parent] || [];
      if (arr.indexOf(child) === -1) arr.push(child);
    };
    goals.forEach(function (g) {
      (g.children || []).forEach(function (k) { addChild(g.id, k); });
    });
    Object.keys(parentsOf).forEach(function (child) {
      parentsOf[child].forEach(function (p) { addChild(p, child); });
    });
    return {
      goals: goals,
      byId: byId,
      childrenOf: childrenOf,
      get: function (id) { return byId[id] || null; },
      // live backlog rocks are free slugs — match exact, aion/-prefixed, or
      // slug-normalized; anything else stays unmatched (→ unanchored group)
      matchRock: function (rockSlug) {
        if (!rockSlug) return null;
        if (byId[rockSlug]) return rockSlug;
        var s = slugify(rockSlug);
        if (bySlug[s]) return bySlug[s];
        if (bySlug['aion/' + s]) return bySlug['aion/' + s];
        return null;
      },
      parentsOf: function (id) { return parentsOf[id] || []; },
      parentOf: function (id) { return (parentsOf[id] || [])[0] || null; },
      childrenIds: function (id) { return childrenOf[id] || []; },
      // every parentless ancestor reachable from a goal (a multi-serving
      // rock has several roots)
      rootsOf: function (id) {
        if (!byId[id]) return [];
        var roots = [], seen = {}, queue = [id];
        while (queue.length) {
          var cur = queue.shift();
          if (seen[cur]) continue;
          seen[cur] = true;
          var ps = parentsOf[cur] || [];
          if (!ps.length) roots.push(cur);
          else ps.forEach(function (p) { queue.push(p); });
        }
        return roots;
      },
      // ancestors (all parent paths) + descendants of a goal
      chainOf: function (id) {
        var chain = {};
        var up = [id];
        while (up.length) {
          var cur = up.shift();
          if (!byId[cur] || chain[cur]) continue;
          chain[cur] = true;
          (parentsOf[cur] || []).forEach(function (p) { up.push(p); });
        }
        var walk = function (gid) {
          (childrenOf[gid] || []).forEach(function (k) {
            if (!chain[k]) { chain[k] = true; walk(k); }
          });
        };
        walk(id);
        return chain;
      },
      // goal (or null) visible under the pinned rock filter: true when the
      // goal is the filter itself or inside its subtree (any parent path)
      inFilter: function (id, filterId) {
        if (!filterId) return true;
        if (!id || !byId[id]) return false;
        var seen = {}, queue = [id];
        while (queue.length) {
          var cur = queue.shift();
          if (seen[cur] || !byId[cur]) continue;
          seen[cur] = true;
          if (cur === filterId) return true;
          (parentsOf[cur] || []).forEach(function (p) { queue.push(p); });
        }
        var hit = false;
        var walk = function (gid) {
          (childrenOf[gid] || []).forEach(function (k) {
            if (k === id) hit = true;
            if (!hit) walk(k);
          });
        };
        walk(filterId);
        return hit;
      }
    };
  }

  var STATUS_MARK = { open: '○', in_progress: '◐', done: '●', decided: '●' };
  function statusMark(status) { return STATUS_MARK[status] || '○'; }

  /* ─── dates / ISO weeks ─── */

  function todayISO() {
    var d = new Date();
    var mm = String(d.getMonth() + 1).padStart(2, '0');
    var dd = String(d.getDate()).padStart(2, '0');
    return d.getFullYear() + '-' + mm + '-' + dd;
  }

  // Monday of the ISO week containing dateStr → "YYYY-MM-DD" (sortable key)
  function isoWeekKey(dateStr) {
    var d = new Date(dateStr + 'T00:00:00');
    if (isNaN(d)) return null;
    var day = (d.getDay() + 6) % 7; // Mon=0
    d.setDate(d.getDate() - day);
    var mm = String(d.getMonth() + 1).padStart(2, '0');
    var dd = String(d.getDate()).padStart(2, '0');
    return d.getFullYear() + '-' + mm + '-' + dd;
  }

  function weekLabel(mondayKey) {
    return 'week of ' + mondayKey.slice(5);
  }

  function addDays(iso, n) {
    var d = new Date(iso + 'T00:00:00');
    d.setDate(d.getDate() + n);
    var mm = String(d.getMonth() + 1).padStart(2, '0');
    var dd = String(d.getDate()).padStart(2, '0');
    return d.getFullYear() + '-' + mm + '-' + dd;
  }

  function fmtMoney(n, currency) {
    if (typeof n !== 'number') return String(n);
    var s = '$' + n.toLocaleString('en-US');
    return currency && currency !== 'USD' ? s + ' ' + currency : s;
  }

  window.PORTAL_UTIL = {
    fetchJSON: fetchJSON,
    fetchText: fetchText,
    parseInlineFields: parseInlineFields,
    parseFieldFile: parseFieldFile,
    renderInline: renderInline,
    slugify: slugify,
    buildGoalIndex: buildGoalIndex,
    statusMark: statusMark,
    todayISO: todayISO,
    isoWeekKey: isoWeekKey,
    weekLabel: weekLabel,
    addDays: addDays,
    fmtMoney: fmtMoney
  };

  /* ═══ timeline date helpers — pure date math, no dataset dependency.
     The timeline derives everything from manifest exports (goals/backlog/
     vto); month positioning ported from the old /roadmap scale. ═══ */

  function parseDate(s) {
    if (!s) return null;
    if (s.length === 7) return new Date(s + '-01T00:00:00');
    var d = new Date(s + 'T00:00:00');
    return isNaN(d) ? null : d;
  }

  // "2026-Q3" → { start: "2026-07-01", end: "2026-09-30" }
  function parseQuarter(q) {
    var m = /^(\d{4})-Q([1-4])$/.exec(q || '');
    if (!m) return null;
    var y = +m[1], qi = +m[2];
    var sm = (qi - 1) * 3; // 0-based start month
    var endD = new Date(y, sm + 3, 0);
    var pad = function (n) { return String(n).padStart(2, '0'); };
    return {
      start: y + '-' + pad(sm + 1) + '-01',
      end: y + '-' + pad(endD.getMonth() + 1) + '-' + pad(endD.getDate())
    };
  }

  var MONTH_NAMES = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

  function fmtDateShort(s) {
    var d = parseDate(s);
    if (!d) return '—';
    return MONTH_NAMES[d.getMonth()] + ' ' + d.getDate();
  }

  // people.json wins (set by data-load as window.PORTAL_PEOPLE)
  function personName(initials) {
    if (!initials) return '—';
    var p = (window.PORTAL_PEOPLE || {})[initials];
    return p ? p.name : initials;
  }

  window.PORTAL_UTIL.parseDate = parseDate;
  window.PORTAL_UTIL.parseQuarter = parseQuarter;
  window.PORTAL_UTIL.fmtDateShort = fmtDateShort;
  window.PORTAL_UTIL.personName = personName;
})();
