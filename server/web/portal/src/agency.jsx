/* Agency field — the collective/goal/person cone visualization, ported from
   the standalone handoff component (aion-agency-field-component.html).
   The upper cone holds hand-tuned goal/person slots; the past (lower) cone
   is data-driven: completed rocks banded by quarter, recent decided
   decisions clustered around the rock they fed, threaded by a chronological
   spine and rock→goal relations. Emits `aion-agency-select` on the root
   section: detail = { type: 'person'|'goal'|'decision'|'rock'|'clear', id, item }. */

/* Hand-tuned geometry slots from the handoff component — display lines stay
   short by design; titles/horizons/links come from portal data. */
/* labels sit in the clear space above each goal button, kicker-first
   (horizon over title, same convention as the left horizon block),
   centered on the button x */
const AAF_GOAL_SLOTS = [
  { portal: 'aion/series-a-15m',      lines: ['SERIES A · $15M'],        labelX: 365, labelY: 213, metaY: 200, x: 365, y: 242, cx: 365, cy: 392, rx: 76, ry: 27, bottom: 514 },
  { portal: 'aion/mouse-to-pig',      lines: ['MOUSE → PIG'],            labelX: 462, labelY: 213, metaY: 200, x: 462, y: 242, cx: 462, cy: 354, rx: 82, ry: 29, bottom: 505 },
  { portal: 'aion/cell-state-control', lines: ['CELL-STATE', 'CONTROL'], labelX: 560, labelY: 139, metaY: 126, x: 570, y: 170, cx: 570, cy: 397, rx: 84, ry: 30, bottom: 530 },
  { portal: 'aion/mri-prototype',     lines: ['HUMAN-SCALE', 'MRI'],     labelX: 662, labelY: 139, metaY: 126, x: 650, y: 170, cx: 650, cy: 350, rx: 78, ry: 28, bottom: 485 }
];
/* aliases: first entry that resolves in people.json wins; the rest cover
   initials drift in goals/backlog (YS vs Y, MO vs MM) */
const AAF_PERSON_SLOTS = [
  { aliases: ['BA'],       x: 414, y: 390, authority: 3 },
  { aliases: ['RT'],       x: 548, y: 395, authority: 3 },
  { aliases: ['HZ'],       x: 492, y: 354, authority: 2 },
  { aliases: ['HG'],       x: 654, y: 350, authority: 2 },
  { aliases: ['EL'],       x: 460, y: 376, authority: 2 },
  { aliases: ['Y', 'YS'],  x: 700, y: 360, authority: 1 },
  { aliases: ['MM', 'MO'], x: 600, y: 392, authority: 1 },
  { aliases: ['JR'],       x: 338, y: 402, authority: 1 },
  { aliases: ['ME'],       x: 675, y: 386, authority: 1 },
  { aliases: ['NM'],       x: 385, y: 368, authority: 1 }
];

/* Past-cone layout: the collective past triangle is (138,374)-(902,374)-(520,704).
   Person past-tips reach ~y 433, so rocks/decisions live in y 468–672. All
   placement is seeded from item ids so the layout is stable across renders. */
const AAF_PAST_DECISIONS = 30;
const AAF_PAST_ZONE = { top: 468, bottom: 672 };
const aafHash = str => {
  let h = 5381;
  for (let i = 0; i < str.length; i++) h = ((h << 5) + h + str.charCodeAt(i)) >>> 0;
  return h;
};
const aafConeHalfWidth = y => 382 * (1 - (y - 374) / 330);
const aafClampX = (x, y, margin) => {
  const hw = Math.max(aafConeHalfWidth(y) - margin, 16);
  return Math.max(520 - hw, Math.min(520 + hw, x));
};

function buildAgencyModel(data, goalsIndex) {
  if (!goalsIndex) return null;
  const horizonLabel = h => h === 'rock' ? '90 DAY' : h === '30' ? '30 DAY' : '1 YEAR';

  const goals = AAF_GOAL_SLOTS
    .map(s => {
      const g = goalsIndex.get(s.portal);
      if (!g) return null;
      return { ...s, id: s.portal, title: g.title, horizon: horizonLabel(g.horizon), portal: g };
    })
    .filter(Boolean);
  if (!goals.length) return null;

  const items = (data.backlog && data.backlog.items) || [];
  const openTasks = items.filter(t => t.kind === 'task' && (t.status === 'open' || t.status === 'in_progress'));
  const inSub = (gid, fieldId) => !!gid && goalsIndex.inFilter(gid, fieldId);

  const peopleData = (data.people && data.people.people) || [];
  const people = AAF_PERSON_SLOTS.map(s => {
    const rec = peopleData.find(p => s.aliases.includes(p.initials));
    const goalIds = goals.filter(F =>
      goalsIndex.goals.some(g => s.aliases.includes(g.owner) && inSub(g.id, F.id)) ||
      openTasks.some(t => s.aliases.includes(t.owner) && inSub(goalsIndex.matchRock(t.rock), F.id))
    ).map(F => F.id);
    return {
      ...s,
      id: rec ? rec.initials : s.aliases[0],
      name: rec ? rec.name : s.aliases[0],
      role: rec ? rec.role : '',
      goals: goalIds
    };
  });

  /* completed rocks — banded by quarter, newest quarter shallowest, rows of
     three spread across the cone width at that depth */
  const coneIds = new Set(AAF_GOAL_SLOTS.map(s => s.portal));
  const doneRocks = goalsIndex.goals.filter(g =>
    g.horizon === 'rock' && g.status === 'done' && g.closed && g.quarter && !coneIds.has(g.id));
  const quarters = [...new Set(doneRocks.map(g => g.quarter))].sort().reverse();
  const rocks = [];
  const bands = [];
  let yCursor = AAF_PAST_ZONE.top;
  quarters.forEach(q => {
    const members = doneRocks.filter(g => g.quarter === q)
      .sort((a, b) => a.closed === b.closed ? (a.id < b.id ? -1 : 1) : (a.closed < b.closed ? 1 : -1));
    const bandTop = yCursor;
    for (let r = 0; r * 3 < members.length; r++) {
      const rowMembers = members.slice(r * 3, r * 3 + 3);
      const rowY = yCursor + 21;
      const hw = Math.max(aafConeHalfWidth(rowY) - 46, 30);
      rowMembers.forEach((g, i) => {
        const seed = aafHash(g.id);
        const x = aafClampX(520 - hw + ((i + 0.5) / rowMembers.length) * 2 * hw + (seed % 25) - 12, rowY, 34);
        const y = rowY + ((seed >> 8) % 11) - 5;
        const short = g.title.split(':')[0];
        rocks.push({
          id: g.id, x, y, quarter: q, closed: g.closed,
          labelDy: 23 + (i % 2) * 11,
          short: short.length > 22 ? short.slice(0, 20) + '…' : short,
          goals: goals.filter(F => inSub(g.id, F.id)).map(F => F.id),
          portal: g
        });
      });
      yCursor += 42;
    }
    bands.push({ quarter: q, top: bandTop, bottom: yCursor });
    yCursor += 14;
  });
  const rockById = Object.fromEntries(rocks.map(r => [r.id, r]));

  /* recent decided decisions — clustered around the rock they fed (or below
     their live goal cone), golden-angle rings, deterministic collision nudge */
  const placed = rocks.map(r => ({ x: r.x, y: r.y }));
  const anchors = {};
  const decisions = items
    .filter(i => i.kind === 'decision' && i.status === 'decided' && i.decided)
    .sort((a, b) => (a.decided < b.decided ? 1 : -1))
    .slice(0, AAF_PAST_DECISIONS)
    .map(d => {
      const gid = goalsIndex.matchRock(d.rock);
      if (!gid) return null;
      const rock = rockById[gid] || null;
      const cone = rock ? null : goals.find(F => inSub(gid, F.id));
      let ax, ay, key;
      if (rock) { ax = rock.x; ay = rock.y; key = rock.id; }
      else if (cone) { ax = cone.cx; ay = cone.bottom + 26; key = 'g:' + cone.id; }
      else { ax = 520; ay = 496; key = 'field'; }
      const anchor = anchors[key] || (anchors[key] = { n: 0 });
      const ring = Math.floor(anchor.n / 5);
      let angle = (aafHash(key) % 360) + anchor.n * 137.5;
      anchor.n += 1;
      let x = ax, y = ay;
      for (let attempt = 0; attempt < 4; attempt++) {
        const rad = angle * Math.PI / 180;
        y = Math.max(AAF_PAST_ZONE.top, Math.min(AAF_PAST_ZONE.bottom, ay + (16 + 7 * ring) * 0.7 * Math.sin(rad)));
        x = aafClampX(ax + (16 + 7 * ring) * Math.cos(rad), y, 12);
        if (!placed.some(p => (p.x - x) * (p.x - x) + (p.y - y) * (p.y - y) < 144)) break;
        angle += 137.5;
      }
      placed.push({ x, y });
      return {
        x, y,
        id: d.id,
        title: d.title.length > 42 ? d.title.slice(0, 40) + '…' : d.title,
        date: d.decided.slice(5),
        owner: d.owner || '',
        portal: d,
        rockId: rock ? rock.id : null,
        liveGoalId: cone ? cone.id : null,
        goals: goals.filter(F => inSub(gid, F.id)).map(F => F.id)
      };
    })
    .filter(Boolean);

  people.forEach(p => {
    const mine = decisions.filter(d => p.aliases.includes(d.portal.owner));
    p.decisions = mine.map(d => d.id);
    p.rocks = [...new Set(mine.filter(d => d.rockId).map(d => d.rockId))];
    p.peers = people
      .filter(q => q.id !== p.id && q.goals.some(g => p.goals.includes(g)))
      .map(q => q.id);
  });

  /* chronological spine — oldest/deepest rock first, ending under NOW */
  const spine = rocks.slice()
    .sort((a, b) => a.closed === b.closed ? (a.id < b.id ? -1 : 1) : (a.closed < b.closed ? -1 : 1))
    .map(r => ({ x: r.x, y: r.y }));
  if (spine.length) spine.push({ x: 520, y: 470 });

  return { goals, people, rocks, decisions, spine, bands };
}

/* The handoff component's builder: operates on a passed root + model and
   returns a cleanup function for React. */
function buildAgencyField(root, model) {
  const { goals, people, rocks, decisions, spine, bands } = model;
  const NS = 'http://www.w3.org/2000/svg';
  const make = (tag, attrs = {}, text = '') => {
    const node = document.createElementNS(NS, tag);
    Object.entries(attrs).forEach(([key, value]) => node.setAttribute(key, value));
    if (text) node.textContent = text;
    return node;
  };

  const goalLayer = root.querySelector('[data-aaf-goals]');
  const personLayer = root.querySelector('[data-aaf-people]');
  const decisionLayer = root.querySelector('[data-aaf-decisions]');
  const rockLayer = root.querySelector('[data-aaf-rocks]');
  const relationLayer = root.querySelector('[data-aaf-relations]');
  const status = root.querySelector('[data-aaf-status]');
  const layers = [goalLayer, personLayer, decisionLayer, rockLayer, relationLayer];
  layers.forEach(l => { l.innerHTML = ''; });
  const goalById = Object.fromEntries(goals.map(item => [item.id, item]));
  const personById = Object.fromEntries(people.map(item => [item.id, item]));
  const decisionById = Object.fromEntries(decisions.map(item => [item.id, item]));
  const rockById = Object.fromEntries(rocks.map(item => [item.id, item]));

  const addGoal = goal => {
    const group = make('g', { class: 'aaf-goal', 'data-id': goal.id, 'data-kind': 'goal', role: 'button', tabindex: '0', 'aria-label': `${goal.title}, ${goal.horizon}` });
    group.append(make('path', { class: 'aaf-goal__future', fill: 'url(#aaf-goal-future)', d: `M${goal.x} ${goal.y} L${goal.cx - goal.rx} ${goal.cy} L${goal.cx + goal.rx} ${goal.cy} Z` }));
    group.append(make('path', { class: 'aaf-goal__past', fill: 'url(#aaf-goal-past)', d: `M${goal.cx - goal.rx} ${goal.cy} L${goal.cx + goal.rx} ${goal.cy} L${goal.cx} ${goal.bottom} Z` }));
    group.append(make('ellipse', { class: 'aaf-goal__waist', cx: goal.cx, cy: goal.cy, rx: goal.rx, ry: goal.ry }));
    group.append(make('circle', { class: 'aaf-goal__button', cx: goal.x, cy: goal.y, r: 9 }));
    group.append(make('circle', { class: 'aaf-goal__core', cx: goal.x, cy: goal.y, r: 2 }));
    const title = make('text', { class: 'aaf-goal__title', x: goal.labelX, y: goal.labelY, 'text-anchor': 'middle' });
    goal.lines.forEach((line, index) => title.append(make('tspan', { x: goal.labelX, dy: index ? 11 : 0 }, line)));
    group.append(title);
    group.append(make('text', { class: 'aaf-goal__horizon', x: goal.labelX, y: goal.metaY, 'text-anchor': 'middle' }, goal.horizon));
    goalLayer.append(group);
  };

  const addPerson = person => {
    const sizes = { 1: { h: 21, r: 10 }, 2: { h: 29, r: 13 }, 3: { h: 38, r: 17 } }, size = sizes[person.authority];
    const group = make('g', { class: 'aaf-person', 'data-id': person.id, 'data-kind': 'person', role: 'button', tabindex: '0', 'aria-label': person.name });
    group.append(make('path', { class: 'aaf-person__future', d: `M${person.x} ${person.y - size.h} L${person.x - size.r} ${person.y} L${person.x + size.r} ${person.y} Z` }));
    group.append(make('path', { class: 'aaf-person__past', d: `M${person.x - size.r} ${person.y} L${person.x + size.r} ${person.y} L${person.x} ${person.y + size.h} Z` }));
    group.append(make('ellipse', { class: 'aaf-person__waist', cx: person.x, cy: person.y, rx: size.r, ry: Math.max(3.5, size.r * .34) }));
    group.append(make('circle', { class: 'aaf-person__core', cx: person.x, cy: person.y, r: 1.8 }));
    group.append(make('text', { class: 'aaf-person__label', x: person.x + size.r + 5, y: person.y + 3 }, person.id));
    personLayer.append(group);
  };

  const addRock = rock => {
    const group = make('g', { class: 'aaf-rock', 'data-id': rock.id, 'data-kind': 'rock', role: 'button', tabindex: '0', 'aria-label': `${rock.portal.title}, completed ${rock.closed}` });
    group.append(make('path', { class: 'aaf-rock__glyph', d: `M${rock.x} ${rock.y - 9} L${rock.x - 7} ${rock.y} L${rock.x + 7} ${rock.y} Z` }));
    group.append(make('path', { class: 'aaf-rock__glyph', d: `M${rock.x - 7} ${rock.y} L${rock.x + 7} ${rock.y} L${rock.x} ${rock.y + 9} Z` }));
    group.append(make('circle', { class: 'aaf-rock__core', cx: rock.x, cy: rock.y, r: 1.6 }));
    group.append(make('text', { class: 'aaf-rock__label', x: rock.x, y: rock.y + rock.labelDy, 'text-anchor': 'middle' }, rock.short.toUpperCase()));
    rockLayer.append(group);
  };

  const addDecision = decision => {
    const group = make('g', { class: 'aaf-decision', 'data-id': decision.id, 'data-kind': 'decision', role: 'button', tabindex: '0', 'aria-label': `${decision.title}, ${decision.date}` });
    group.append(make('rect', { class: 'aaf-decision__diamond', x: decision.x - 4.5, y: decision.y - 4.5, width: 9, height: 9, transform: `rotate(45 ${decision.x} ${decision.y})` }));
    group.append(make('circle', { class: 'aaf-decision__core', cx: decision.x, cy: decision.y, r: 1.4 }));
    group.append(make('text', { class: 'aaf-decision__label', x: decision.x + 10, y: decision.y - 2 }, decision.title.toUpperCase()));
    decisionLayer.append(group);
  };

  const addPath = (modifier, d, rel) => {
    const path = make('path', { class: `aaf-relation aaf-relation--${modifier}`, 'data-rel': rel, d });
    relationLayer.append(path);
    return path;
  };

  goals.forEach(addGoal);
  rocks.forEach(addRock);
  bands.forEach(b => {
    const midY = (b.top + b.bottom) / 2 + 3;
    rockLayer.append(make('text', {
      class: 'aaf-quarter', x: 520 - aafConeHalfWidth(midY) - 10, y: midY, 'text-anchor': 'end', 'aria-hidden': 'true'
    }, `${b.quarter.slice(5)} ${b.quarter.slice(0, 4)}`));
  });
  decisions.forEach(addDecision);
  people.forEach(addPerson);

  /* chronological spine — always-on faint thread, deepest first */
  if (spine.length > 1) {
    let d = `M${spine[0].x} ${spine[0].y}`;
    for (let i = 1; i < spine.length; i++) {
      const a = spine[i - 1], b = spine[i];
      d += ` C${a.x} ${a.y - 22} ${b.x} ${b.y + 22} ${b.x} ${b.y}`;
    }
    addPath('spine', d, 'spine');
  }

  /* rock → live goal — always-on faint, with ambient particles feeding the
     present; brightens (and speeds up) when either end is selected */
  let flowIndex = 0;
  rocks.forEach(rock => rock.goals.forEach(id => {
    const goal = goalById[id];
    const d = `M${rock.x} ${rock.y} C${rock.x} ${rock.y - 130} ${goal.x} ${goal.y + 150} ${goal.x} ${goal.y}`;
    addPath('rockgoal', d, `r:${rock.id}|g:${id}`);
    relationLayer.append(make('circle', {
      class: 'aaf-flow aaf-flow--ambient', r: 2, 'data-flow': `r:${rock.id}|g:${id}`,
      style: `offset-path:path('${d}');animation-delay:-${(flowIndex++ * 1.3).toFixed(1)}s`
    }));
  }));

  people.forEach(person => {
    person.goals.forEach(id => { const goal = goalById[id]; addPath('goal', `M${person.x} ${person.y} C${person.x} ${person.y - 70} ${goal.x} ${goal.y + 80} ${goal.x} ${goal.y}`, `p:${person.id}|g:${id}`); });
    person.decisions.forEach(id => { const decision = decisionById[id]; addPath('decision', `M${decision.x} ${decision.y} C${decision.x} ${decision.y - 68} ${person.x} ${person.y + 62} ${person.x} ${person.y}`, `p:${person.id}|d:${id}`); });
    person.peers.filter(id => person.id < id).forEach(id => { const peer = personById[id]; addPath('peer', `M${person.x} ${person.y} Q${(person.x + peer.x) / 2} ${(person.y + peer.y) / 2 - 16} ${peer.x} ${peer.y}`, `p:${person.id}|p:${id}`); });
  });
  decisions.forEach(decision => {
    if (decision.rockId) {
      const rock = rockById[decision.rockId];
      const d = `M${decision.x} ${decision.y} Q${(decision.x + rock.x) / 2} ${(decision.y + rock.y) / 2 - 10} ${rock.x} ${rock.y}`;
      addPath('decision', d, `d:${decision.id}|r:${rock.id}`);
    } else if (decision.liveGoalId) {
      const goal = goalById[decision.liveGoalId];
      const d = `M${decision.x} ${decision.y} C${decision.x} ${decision.y - 95} ${goal.x} ${goal.y + 112} ${goal.x} ${goal.y}`;
      addPath('decision', d, `d:${decision.id}|g:${goal.id}`);
      relationLayer.append(make('circle', { class: 'aaf-flow', r: 2.4, 'data-flow': `d:${decision.id}|g:${goal.id}`, style: `offset-path:path('${d}')` }));
    }
  });

  const emit = (type, id, item) => root.dispatchEvent(new CustomEvent('aion-agency-select', { bubbles: true, detail: { type, id, item } }));
  const reset = (notify = true) => {
    root.querySelectorAll('.is-hot,.is-dim,.is-visible,.is-focus').forEach(node => node.classList.remove('is-hot', 'is-dim', 'is-visible', 'is-focus'));
    status.textContent = 'Selection cleared';
    if (notify) emit('clear', null, null);
  };
  const showRelations = tokens => {
    root.querySelectorAll('[data-rel]').forEach(node => { if (tokens.some(token => (node.dataset.rel || '').includes(token))) node.classList.add('is-visible'); });
    root.querySelectorAll('[data-flow]').forEach(node => { if (tokens.some(token => (node.dataset.flow || '').includes(token))) node.classList.add('is-visible'); });
  };
  const hotDim = (selector, isHot) => {
    root.querySelectorAll(selector).forEach(node => {
      const hot = isHot(node.dataset.id);
      node.classList.toggle('is-hot', hot);
      node.classList.toggle('is-dim', !hot);
    });
  };

  const selectPerson = person => {
    reset(false);
    const goalIds = new Set(person.goals), decisionIds = new Set(person.decisions), peerIds = new Set(person.peers), rockIds = new Set(person.rocks);
    hotDim('.aaf-person', id => id === person.id || peerIds.has(id));
    hotDim('.aaf-goal', id => goalIds.has(id));
    hotDim('.aaf-decision', id => decisionIds.has(id));
    hotDim('.aaf-rock', id => rockIds.has(id));
    showRelations([`p:${person.id}`]);
    /* the person's trail through the past: their decisions' threads into
       rocks/goals, and those rocks' threads into the present */
    person.decisions.forEach(id => root.querySelectorAll(`[data-rel^="d:${id}|"]`).forEach(node => node.classList.add('is-visible')));
    rockIds.forEach(id => root.querySelectorAll(`[data-rel^="r:${id}|"]`).forEach(node => node.classList.add('is-visible')));
    status.textContent = `Selected ${person.name}`;
    emit('person', person.id, person);
  };

  const selectGoal = goal => {
    reset(false);
    const peopleIds = new Set(people.filter(person => person.goals.includes(goal.id)).map(person => person.id));
    const decisionIds = new Set(decisions.filter(decision => decision.goals.includes(goal.id)).map(decision => decision.id));
    const rockIds = new Set(rocks.filter(rock => rock.goals.includes(goal.id)).map(rock => rock.id));
    hotDim('.aaf-goal', id => id === goal.id);
    hotDim('.aaf-person', id => peopleIds.has(id));
    hotDim('.aaf-decision', id => decisionIds.has(id));
    hotDim('.aaf-rock', id => rockIds.has(id));
    showRelations([`g:${goal.id}`]);
    rockIds.forEach(id => root.querySelectorAll(`[data-rel*="|r:${id}"]`).forEach(node => node.classList.add('is-visible')));
    status.textContent = `Selected ${goal.title}`;
    emit('goal', goal.id, goal);
  };

  const selectRock = rock => {
    reset(false);
    const mine = decisions.filter(decision => decision.rockId === rock.id);
    const decisionIds = new Set(mine.map(decision => decision.id));
    const owners = new Set(mine.map(decision => decision.portal.owner));
    const peopleIds = new Set(people.filter(person => person.aliases.some(a => owners.has(a))).map(person => person.id));
    const goalIds = new Set(rock.goals);
    hotDim('.aaf-rock', id => id === rock.id);
    hotDim('.aaf-decision', id => decisionIds.has(id));
    hotDim('.aaf-goal', id => goalIds.has(id));
    hotDim('.aaf-person', id => peopleIds.has(id));
    showRelations([`r:${rock.id}`]);
    status.textContent = `Selected ${rock.portal.title}, completed ${rock.closed}`;
    emit('rock', rock.id, rock);
  };

  const selectDecision = decision => {
    reset(false);
    const goalIds = new Set(decision.goals);
    const peopleIds = new Set(people.filter(person => person.decisions.includes(decision.id)).map(person => person.id));
    hotDim('.aaf-decision', id => id === decision.id);
    hotDim('.aaf-goal', id => goalIds.has(id));
    hotDim('.aaf-person', id => peopleIds.has(id));
    hotDim('.aaf-rock', id => id === decision.rockId);
    const node = root.querySelector(`.aaf-decision[data-id="${decision.id}"]`);
    if (node) node.classList.add('is-focus');
    showRelations([`d:${decision.id}`]);
    if (decision.rockId) root.querySelectorAll(`[data-rel^="r:${decision.rockId}|"]`).forEach(node => node.classList.add('is-visible'));
    status.textContent = `Selected ${decision.title}`;
    emit('decision', decision.id, decision);
  };

  const activate = node => {
    const { kind, id } = node.dataset;
    if (kind === 'person') selectPerson(personById[id]);
    if (kind === 'goal') selectGoal(goalById[id]);
    if (kind === 'rock') selectRock(rockById[id]);
    if (kind === 'decision') selectDecision(decisionById[id]);
  };
  const nodeListeners = [];
  root.querySelectorAll('[data-kind]').forEach(node => {
    const onClick = event => { event.stopPropagation(); activate(node); };
    const onKey = event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); activate(node); } };
    node.addEventListener('click', onClick);
    node.addEventListener('keydown', onKey);
    nodeListeners.push([node, onClick, onKey]);
  });
  const onRootClick = event => { if (event.target === root || event.target === root.querySelector('.aaf__svg')) reset(); };
  root.addEventListener('click', onRootClick);

  return {
    cleanup: () => {
      root.removeEventListener('click', onRootClick);
      nodeListeners.forEach(([node, onClick, onKey]) => {
        node.removeEventListener('click', onClick);
        node.removeEventListener('keydown', onKey);
      });
      layers.forEach(l => { l.innerHTML = ''; });
    },
    reset
  };
}

function AgencyField({ data, goalsIndex, onSelect, selection }) {
  const rootRef = React.useRef(null);
  const apiRef = React.useRef(null);
  const model = React.useMemo(() => buildAgencyModel(data, goalsIndex), [data, goalsIndex]);

  React.useEffect(() => {
    const root = rootRef.current;
    if (!root || !model) return;
    const api = buildAgencyField(root, model);
    apiRef.current = api;
    const handler = e => { if (onSelect) onSelect(e.detail); };
    root.addEventListener('aion-agency-select', handler);
    return () => {
      root.removeEventListener('aion-agency-select', handler);
      apiRef.current = null;
      api.cleanup();
    };
  }, [model, onSelect]);

  // pane-side clears (Esc, ✕) un-dim the cone without re-emitting
  React.useEffect(() => {
    if (!selection && apiRef.current) apiRef.current.reset(false);
  }, [selection]);

  if (!model) return null;

  /* Gradient stops read the v2 token contract (stop-color is a CSS property —
     var() resolves in inline STYLE, never as a bare attribute); fallbacks keep
     the cone sane when no theme tokens are set. No color-mix anywhere (older
     Safari hard-fails) — translucency rides stopOpacity/fill-opacity. */
  return (
    <div className="agency-block">
      <section className="aaf" ref={rootRef} aria-label="AION collective agency field">
        <svg className="aaf__svg" viewBox="30 26 980 690" preserveAspectRatio="xMidYMid meet" role="group" aria-label="AION collective agency field. Select a person, goal, completed rock, or decision to reveal its relationships.">
          <defs>
            <linearGradient id="aaf-collective-future" x1="0" y1="1" x2="0" y2="0"><stop style={{ stopColor: 'var(--accent-deep,#0135fe)', stopOpacity: 0.13 }} /><stop offset="1" style={{ stopColor: 'var(--accent-deep,#0135fe)', stopOpacity: 0.07 }} /></linearGradient>
            <linearGradient id="aaf-collective-past" x1="0" y1="0" x2="0" y2="1"><stop style={{ stopColor: 'var(--accent-deep,#0135fe)', stopOpacity: 0.10 }} /><stop offset="1" style={{ stopColor: 'var(--accent-deep,#0135fe)', stopOpacity: 0.03 }} /></linearGradient>
            <linearGradient id="aaf-goal-future" x1="0" y1="1" x2="0" y2="0"><stop style={{ stopColor: 'var(--m1,#0091ea)', stopOpacity: 0.2 }} /><stop offset="1" style={{ stopColor: 'var(--accent-bright,#5ec8f5)', stopOpacity: 0.34 }} /></linearGradient>
            <linearGradient id="aaf-goal-past" x1="0" y1="0" x2="0" y2="1"><stop style={{ stopColor: 'var(--m1,#0091ea)', stopOpacity: 0.15 }} /><stop offset="1" style={{ stopColor: 'var(--accent-deep,#0135fe)', stopOpacity: 0.05 }} /></linearGradient>
            <linearGradient id="aaf-person-pink" x1="0" y1="1" x2="0" y2="0"><stop style={{ stopColor: 'var(--m2,#d4d4d4)', stopOpacity: 0.26 }} /><stop offset=".5" style={{ stopColor: 'var(--m2,#ffffff)', stopOpacity: 0.42 }} /><stop offset="1" style={{ stopColor: 'var(--m2,#cfcfcf)', stopOpacity: 0.2 }} /></linearGradient>
          </defs>

          <ellipse className="aaf__plane" cx="520" cy="374" rx="410" ry="112" />
          <text className="aaf__now" x="938" y="377">NOW</text>

          <path className="aaf__collective" fill="url(#aaf-collective-future)" d="M520 48 L902 374 L138 374 Z" />
          <path className="aaf__collective" fill="url(#aaf-collective-past)" d="M138 374 L902 374 L520 704 Z" />
          <g data-aaf-relations aria-hidden="true"></g>
          <g data-aaf-goals></g>
          <g data-aaf-rocks></g>
          <g data-aaf-decisions></g>
          <g data-aaf-people></g>
        </svg>
        <span className="aaf__status" data-aaf-status aria-live="polite"></span>
      </section>
    </div>
  );
}

Object.assign(window, { AgencyField });
