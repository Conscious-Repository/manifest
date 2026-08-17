/* Portal v2 view-model derivation — plain JS (no Babel), pure functions ported
   from the prototype's renderVals/extraVals. Every function takes explicit
   inputs and returns plain data; JSX components render what comes back. No
   optional chaining anywhere (older-Safari hard-fails on Babel-standalone). */
(function () {
  const U = function () { return window.PORTAL_UTIL; };

  function isOpen(it) {
    return it.status === 'open' || it.status === 'in_progress' || !it.status;
  }
  function mark(status) {
    return status === 'done' || status === 'decided' ? '●' : status === 'in_progress' ? '◐' : '○';
  }
  function markColor(status) {
    return status === 'done' || status === 'decided' ? 'var(--good,#6fb8dd)'
      : status === 'in_progress' ? 'var(--accent,#0091ea)' : 'var(--ink-faint,#888)';
  }
  function hLabel(h) {
    return h === 'rock' ? '90-day rock' : h === '30' ? '30-day' : h === '1yr' ? '1-year' : '';
  }
  function kindColor(k) {
    return k === 'decision' ? 'var(--ink-dim,#b0b0b0)'
      : k === 'artifact' || k === 'paper' ? 'var(--m4,#9fd8f5)'
      : k === 'heuristic' || k === 'rock closed' ? 'var(--accent,#0091ea)'
      : 'var(--ink-faint,#888)';
  }

  /* Team overlay: published backlog + team/ items + field overrides. */
  function mergedBacklog(backlog, team) {
    if (!backlog) return backlog;
    let items = backlog.items.slice();
    if (team && Array.isArray(team.items)) items = items.concat(team.items);
    if (team && team.overrides) {
      items = items.map(function (it) {
        const ov = team.overrides[it.id];
        return ov && ov.fields ? Object.assign({}, it, ov.fields) : it;
      });
    }
    return Object.assign({}, backlog, { items: items });
  }

  function commentsOf(team, id) {
    return (team && team.comments && team.comments[id]) || [];
  }

  function rockOf(idx, it) { return idx ? idx.matchRock(it.rock) : null; }
  function visible(idx, it, filter) { return idx ? idx.inFilter(rockOf(idx, it), filter) : true; }

  /* One row shape, shared by work/week/subtree lists (extraVals row()). */
  function row(it, team, today, select) {
    const u = U();
    const name = it.owner ? u.personName(it.owner) : '';
    const parts = name ? name.split(' ') : [];
    const due = it.due || it.needed_by || '';
    const n = commentsOf(team, it.id).length;
    return {
      id: it.id,
      title: it.title,
      badge: it.team ? 'team/' : '',
      ownerName: it.owner ? (parts[0] + ' ' + (parts[1] || '').slice(0, 1)) : 'unowned',
      owner: it.owner || '—',
      comments: n ? '· ' + n : '',
      due: due ? u.fmtDateShort(due) : '—',
      dueColor: (due && due < today) ? 'var(--warn,#a44)' : 'var(--ink-faint,#888)',
      mark: it.kind === 'decision' ? '◇' : mark(it.status),
      markColor: it.kind === 'decision' ? 'var(--ink-dim,#b0b0b0)' : markColor(it.status),
      open: function () { select('item', it.id); }
    };
  }

  const byDue = function (a, b) {
    const ad = a.due || a.needed_by, bd = b.due || b.needed_by;
    return (ad ? 0 : 1) - (bd ? 0 : 1) || (ad < bd ? -1 : 1);
  };

  /* nav counts (renderVals): work = open+visible, goals = non-done goals. */
  function navCounts(items, idx, filter) {
    const work = items.filter(function (it) { return isOpen(it) && visible(idx, it, filter); }).length;
    const goals = idx ? idx.goals.filter(function (g) { return g.status !== 'done'; }).length : 0;
    return { work: String(work), goals: String(goals) };
  }

  /* YOURS TO DECIDE (field key column). */
  function myDecisions(items, idx, filter, meInitials, today, select, goDecisions) {
    const u = U();
    const openDecs = items.filter(function (it) {
      return it.kind === 'decision' && isOpen(it) && visible(idx, it, filter);
    });
    const byDate = function (a, b) {
      return (a.needed_by ? 0 : 1) - (b.needed_by ? 0 : 1) || (a.needed_by < b.needed_by ? -1 : 1);
    };
    const mine = openDecs.filter(function (d) { return d.owner === meInitials; }).sort(byDate);
    const theirs = openDecs.length - mine.length;
    return {
      rows: mine.slice(0, 4).map(function (d) {
        const late = d.needed_by && d.needed_by < today;
        const soon = d.needed_by && !late &&
          (new Date(d.needed_by + 'T00:00:00') - new Date(today + 'T00:00:00')) <= 14 * 864e5;
        return {
          title: d.title.length > 78 ? d.title.slice(0, 76) + '…' : d.title,
          by: d.needed_by ? (late ? 'overdue · ' + u.fmtDateShort(d.needed_by) : 'by ' + u.fmtDateShort(d.needed_by)) : 'no date set',
          byColor: late ? 'var(--warn,#a44)' : 'var(--ink-faint,#777)',
          edge: late ? 'var(--warn,#a44)' : soon ? 'var(--accent,#0091ea)' : 'var(--line,#3a3a3a)',
          open: function () { select('item', d.id); }
        };
      }),
      empty: mine.length === 0,
      othersShown: theirs > 0 || mine.length > 4,
      othersLine: (mine.length > 4 ? (mine.length - 4) + ' more yours · ' : '') + theirs + ' with others →',
      goDecisions: goDecisions
    };
  }

  /* THIS WEEK: open tasks due within 7 days (overdue included), ascending. */
  function thisWeek(items, idx, filter, today, team, select) {
    const inWindow = function (d) {
      if (!d) return false;
      const t = new Date(today + 'T00:00:00');
      const x = new Date(d + 'T00:00:00');
      return x - t <= 7 * 864e5;
    };
    const open = items.filter(function (it) {
      return it.kind === 'task' && isOpen(it) && visible(idx, it, filter);
    });
    const week = open.filter(function (it) { return inWindow(it.due); })
      .sort(function (a, b) { return a.due < b.due ? -1 : 1; });
    return { rows: week.map(function (it) { return row(it, team, today, select); }), empty: week.length === 0 };
  }

  /* WORK: one merged list, yours first, then rock groups (extraVals). */
  function workSections(items, idx, filter, meInitials, team, today, select, pin) {
    const open = items.filter(function (it) { return isOpen(it) && visible(idx, it, filter); });
    const mine = open.filter(function (it) { return it.owner === meInitials; });
    const rest = open.filter(function (it) { return it.owner !== meInitials; });
    const sections = [];
    const mkRow = function (it) { return row(it, team, today, select); };
    if (mine.length) {
      sections.push({
        key: 'yours', title: 'yours', horizon: 'assigned to you',
        color: 'var(--ink,#d4d4d4)', count: mine.length + ' open',
        pin: function () {}, rows: mine.slice().sort(byDue).map(mkRow)
      });
    }
    const gmap = {};
    rest.forEach(function (it) {
      const key = rockOf(idx, it) || '__unanchored';
      if (filter && key === '__unanchored') return;
      (gmap[key] = gmap[key] || []).push(it);
    });
    Object.keys(gmap).sort(function (a, b) {
      return a === '__unanchored' ? 1 : b === '__unanchored' ? -1 : a.localeCompare(b);
    }).forEach(function (key) {
      const g = key === '__unanchored' ? null : idx.get(key);
      sections.push({
        key: key,
        title: g ? g.title : 'unanchored',
        horizon: g ? hLabel(g.horizon) : 'no rock resolved',
        color: filter === key ? 'var(--accent,#0091ea)' : 'var(--ink,#d4d4d4)',
        count: gmap[key].length + ' open',
        pin: function () { if (g) pin(g.id); },
        rows: gmap[key].slice().sort(byDue).map(mkRow)
      });
    });
    const decisions = open.filter(function (i) { return i.kind === 'decision'; }).length;
    return {
      sections: sections,
      empty: sections.length === 0,
      countLine: open.length + ' open · ' + decisions + ' decisions' +
        (filter ? ' · scoped' : '') + (mine.length ? ' · ' + mine.length + ' yours' : '')
    };
  }

  /* ARTIFACTS derived from comment attachments (real source; the prototype's
     hard-coded ARTIFACTS array maps to these + agent output). */
  function artifactEntries(items, idx, team) {
    const out = [];
    if (!team || !team.comments) return out;
    const byId = {};
    items.forEach(function (it) { byId[it.id] = it; });
    Object.keys(team.comments).forEach(function (id) {
      const it = byId[id];
      team.comments[id].forEach(function (c) {
        (c.files || []).forEach(function (f) {
          out.push({
            date: String(c.at || '').slice(0, 10),
            title: f.name,
            provenance: 'attached by ' + (c.author_name || c.author) + (it ? ' · ' + it.title : ''),
            goal: it ? rockOf(idx, it) : null,
            hash: f.hash,
            itemId: id
          });
        });
      });
    });
    return out;
  }

  /* PAPERS from content/references.md rows (no dates in contract v1 — the
     archive carries the design's own flag note for that). */
  function paperEntries(referencesMd) {
    const u = U();
    if (!referencesMd) return [];
    return u.parseFieldFile(referencesMd).map(function (r) {
      return { title: r.rest, url: r.fields.url || '', source: r.fields.source || '', date: r.fields.date || '', note: r.fields.note || '' };
    }).filter(function (p) { return p.title; });
  }

  /* ARCHIVE — one record, one grammar (extraVals arch). */
  function archiveRows(items, idx, filter, team, heuristics, referencesMd, tab, today, hooks) {
    const u = U();
    const papers = paperEntries(referencesMd);
    const heur = (heuristics && heuristics.heuristics) || [];
    const arts = artifactEntries(items, idx, team);
    const all = []
      .concat(items.filter(function (i) { return i.kind === 'task' && i.status === 'done'; }).map(function (t) {
        return { f: 'work', date: t.done_on || t.captured, kind: 'done', title: t.title, detail: '',
          provenance: (t.owner ? u.personName(t.owner) : 'unowned') + ' · ' + (t.source || ''),
          goal: rockOf(idx, t), open: function () { hooks.select('item', t.id); } };
      }))
      .concat(items.filter(function (i) { return i.kind === 'decision' && i.status === 'decided'; }).map(function (d) {
        return { f: 'decisions', date: d.decided, kind: 'decision', title: d.title, detail: '→ ' + (d.outcome || ''),
          provenance: u.personName(d.owner) + ' · ' + (d.source || ''),
          goal: rockOf(idx, d), open: function () { hooks.select('item', d.id); } };
      }))
      .concat((idx ? idx.goals : []).filter(function (g) { return g.status === 'done' && g.closed; }).map(function (g) {
        return { f: 'work', date: g.closed, kind: 'rock closed', title: g.title, detail: '',
          provenance: g.quarter || '', goal: g.id, open: function () { hooks.openGoal(g.id); } };
      }))
      .concat(arts.map(function (a) {
        return { f: 'artifacts', date: a.date, kind: 'artifact', title: a.title, detail: '',
          provenance: a.provenance, goal: a.goal,
          open: function () { hooks.openFile(a.hash); } };
      }))
      .concat(papers.map(function (p) {
        return { f: 'papers', date: p.date, kind: 'paper', title: p.title, detail: p.note,
          provenance: p.source, goal: null,
          open: function () { if (p.url) window.open(p.url, '_blank'); } };
      }))
      .concat(heur.map(function (h) {
        const n = (h.reinforcements || []).length;
        return { f: 'heuristics', date: h.first, kind: 'heuristic', title: h.statement,
          detail: n > 1 ? 'reinforced ×' + n : '', provenance: 'first ' + (h.first || '—'),
          goal: null, open: function () {} };
      }))
      .filter(function (e) { return e.date || e.f === 'papers'; })
      .filter(function (e) { return idx.inFilter(e.goal, filter); });
    const counts = {};
    ['all', 'work', 'decisions', 'artifacts', 'papers', 'heuristics'].forEach(function (f) {
      counts[f] = String(f === 'all' ? all.length : all.filter(function (e) { return e.f === f; }).length);
    });
    const rows = all
      .filter(function (e) { return tab === 'all' || e.f === tab; })
      .sort(function (a, b) { return a.date < b.date ? 1 : -1; })
      .map(function (e) {
        return { date: e.date ? u.fmtDateShort(e.date) : '—', kind: e.kind, kindColor: kindColor(e.kind),
          title: e.title, detail: e.detail, provenance: e.provenance, open: e.open };
      });
    return { rows: rows, counts: counts };
  }

  /* GOALS ladder (renderVals): serves_all rocks render once, under the primary. */
  function ladder(vto, idx, filter, pin) {
    const u = U();
    if (!vto || !vto.one_year_plan || !idx) return [];
    const oneYear = (vto.one_year_plan.goals || []).map(function (id) { return idx.get(id); }).filter(Boolean);
    const parentsOfRock = function (g) {
      return (g.serves_all && g.serves_all.length) ? g.serves_all : (g.serves ? [g.serves] : []);
    };
    const primaryOf = function (g) { return g.serves || parentsOfRock(g)[0] || null; };
    const liveRocks = idx.goals.filter(function (g) { return g.horizon === 'rock' && g.status !== 'done'; });
    const oneYearIds = oneYear.map(function (y) { return y.id; });
    const mkRock = function (r) {
      return {
        title: r.title,
        color: filter === r.id ? 'var(--accent,#0091ea)' : 'var(--ink,#d4d4d4)',
        owner: r.owner ? u.personName(r.owner) : 'unowned',
        quarter: r.quarter || '',
        serves: (function () {
          const names = parentsOfRock(r).map(function (p) { const pg = idx.get(p); return pg ? pg.title : p; });
          if (!names.length) return 'serves no 1-year goal yet';
          return (names.length > 1 ? 'serves all ' + names.length + ' · ' : 'serves · ') + names.join(' · ');
        })(),
        pin: function () { pin(r.id); },
        children: (r.children || []).map(function (cid) { return idx.get(cid); }).filter(Boolean).map(function (c, i, arr) {
          return { tree: i === arr.length - 1 ? '└──' : '├──', title: c.title, owner: c.owner || '',
            pin: function () { pin(c.id); } };
        })
      };
    };
    const out = oneYear.map(function (y) {
      const rocks = liveRocks.filter(function (g) { return primaryOf(g) === y.id; });
      return {
        title: y.title,
        color: filter === y.id ? 'var(--accent,#0091ea)' : 'var(--ink,#d4d4d4)',
        measurable: y.status === 'open' ? 'live' : (y.status || 'live'),
        pin: function () { pin(y.id); },
        rocks: rocks.filter(function (r) { return idx.inFilter(r.id, filter); }).map(mkRock)
      };
    });
    const orphans = liveRocks.filter(function (g) { return oneYearIds.indexOf(primaryOf(g)) === -1; });
    if (orphans.length) {
      out.push({
        title: 'not serving a 1-year goal',
        color: 'var(--ink-faint,#888)',
        measurable: 'live rocks with no parent in the plan',
        pin: function () {},
        rocks: orphans.filter(function (r) { return idx.inFilter(r.id, filter); }).map(mkRock)
      });
    }
    return out;
  }

  /* TIMELINE (quarter gantt). Quarter label + months derive from vto.quarter. */
  function quarterInfo(vto) {
    const q = vto && vto.quarter;
    if (!q || !q.start) return { label: '', months: [] };
    const d = new Date(q.start + 'T00:00:00');
    const label = d.getFullYear() + '-Q' + (Math.floor(d.getMonth() / 3) + 1);
    const names = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
    const months = [names[d.getMonth()], names[(d.getMonth() + 1) % 12], names[(d.getMonth() + 2) % 12]];
    return { label: label, months: months };
  }
  function timelineRows(vto, idx, filter, pin) {
    if (!vto || !vto.quarter || !idx) return [];
    const q = quarterInfo(vto);
    const qStart = new Date(vto.quarter.start + 'T00:00:00');
    const qEnd = new Date(vto.quarter.end + 'T00:00:00');
    const span = qEnd - qStart;
    const pct = function (d) {
      return Math.max(0, Math.min(100, ((new Date(d + 'T00:00:00') - qStart) / span) * 100));
    };
    return idx.goals.filter(function (g) {
      return g.horizon === 'rock' && g.quarter === q.label && idx.inFilter(g.id, filter);
    }).map(function (g) {
      const l = pct(g.start || vto.quarter.start);
      const r = pct(g.due || g.closed || vto.quarter.end);
      const done = g.status === 'done';
      return {
        title: g.title,
        color: filter === g.id ? 'var(--accent,#0091ea)' : 'var(--ink,#d4d4d4)',
        left: l.toFixed(1) + '%',
        width: Math.max(2, r - l).toFixed(1) + '%',
        fill: done ? 'var(--good,#6fb8dd)' : 'var(--accent,#0091ea)',
        fillOpacity: done ? 0.42 : 0.22,
        stroke: done ? 'var(--good,#6fb8dd)' : 'var(--accent,#0091ea)',
        pin: function () { pin(g.id); }
      };
    });
  }

  /* filterExplain (rail scope chip). */
  function filterExplain(items, idx, filter) {
    if (!filter) return '';
    const openAll = items.filter(isOpen).length;
    const shown = items.filter(function (it) { return isOpen(it) && visible(idx, it, filter); }).length;
    return 'work, archive and the timeline are hiding everything outside this goal and its children — ' +
      shown + ' of ' + openAll + ' open items shown';
  }

  /* Item STREAM: comments (said) + activity trail (changed) + agent turns —
     one chronological record. Agent comments (author agent:*) are the turns. */
  function mergeStream(item, team, activity, me, today, handlers) {
    const u = U();
    const out = [];
    const none = function () {};
    commentsOf(team, item.id).forEach(function (c, i) {
      const isAgent = String(c.author || '').indexOf('agent:') === 0;
      const mineC = !isAgent && (c.author === me.email || me.admin);
      const at = String(c.at || '').slice(0, 10);
      out.push({
        key: 'c-' + (c.id || i),
        sort: at + '-' + String(isAgent ? 3000 + i : 1000 + i),
        at: u.fmtDateShort(at), label: isAgent ? 'agent' : 'said',
        glyphColor: isAgent ? 'var(--accent-bright,#5ec8f5)' : 'var(--ink-faint,#888)',
        who: isAgent ? '@' + String(c.author).slice(6).split('::')[0] : (c.author_name || c.author),
        whoColor: isAgent ? 'var(--accent-bright,#5ec8f5)' : 'var(--ink,#d4d4d4)',
        state: '', stateColor: 'var(--ink-mute,#666)',
        textColor: 'var(--ink,#d4d4d4)',
        text: c.text,
        meta: (c.files || []).length
          ? 'attached · ' + c.files.map(function (f) { return f.name; }).join(', ')
          : ((c.mentions || []).length ? '↳ ' + c.mentions.join(' ') : ''),
        files: c.files || [],
        hasSteps: false, hasEdit: false,
        delLabel: mineC ? 'delete' : '', delCursor: mineC ? 'pointer' : 'default',
        del: mineC ? function () { handlers.deleteComment(c); } : none
      });
    });
    (activity || []).forEach(function (e, i) {
      if (e.action === 'comment' || e.action === 'delete-comment') return; // comments render themselves
      const at = String(e.ts || '').slice(0, 10);
      let text = '';
      const pl = e.payload || {};
      if (e.action === 'patch') {
        const fields = pl.fields || {};
        text = Object.keys(fields).map(function (k) { return 'set ' + k + ' → ' + fields[k]; }).join(' · ');
      } else if (e.action === 'agent-assign') {
        text = 'assigned ' + (pl.owner || 'the agent');
      } else if (e.action === 'agent-fire') {
        text = 'fired the plan — the agent is executing';
      } else if (e.action === 'decide-proposal') {
        text = 'proposal ' + (pl.approved ? 'approved' : 'decided');
      } else {
        text = e.action;
      }
      if (!text) return;
      out.push({
        key: 'a-' + i,
        sort: at + '-' + String(2000 + i),
        at: u.fmtDateShort(at), label: 'changed', glyphColor: 'var(--accent,#0091ea)',
        who: String(e.actor || '').split('@')[0], whoColor: 'var(--ink-faint,#888)',
        state: '', stateColor: 'var(--ink-mute,#666)', textColor: 'var(--ink-dim,#aaa)',
        text: text, meta: '', files: [],
        hasSteps: false, hasEdit: false, delLabel: '', delCursor: 'default', del: none
      });
    });
    out.sort(function (a, b) { return a.sort < b.sort ? -1 : 1; });
    return out;
  }

  window.PORTAL_DERIVE = {
    isOpen: isOpen, mark: mark, markColor: markColor, hLabel: hLabel, kindColor: kindColor,
    mergedBacklog: mergedBacklog, commentsOf: commentsOf, rockOf: rockOf, visible: visible,
    row: row, byDue: byDue, navCounts: navCounts, myDecisions: myDecisions, thisWeek: thisWeek,
    workSections: workSections, archiveRows: archiveRows, artifactEntries: artifactEntries,
    paperEntries: paperEntries, ladder: ladder, quarterInfo: quarterInfo, timelineRows: timelineRows,
    filterExplain: filterExplain, mergeStream: mergeStream
  };
})();
