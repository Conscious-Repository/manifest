/* Portal v2 theme system (design handoff, THEMES verbatim from the prototype).
   Plain JS on purpose — no Babel pass, syntax-safe, loads before any JSX.
   The whole surface reads a CSS custom-property token contract; a theme is
   just the token values. Derived tokens (--line/--line-soft/--ink-mute) are
   computed per theme by channel-wise linear mix; E-Ink overrides them to pure
   black (black hairlines are the point of that theme). */
(function () {
  const THEMES = [
    { id: 'aion', name: 'AION charcoal', mode: 'dark', bg: ['#262626', '#1e1e1e', '#1a1a1a'], ink: ['#d4d4d4', '#aaaaaa', '#888888'], accent: ['#0091ea', '#5ec8f5', '#0135fe'], warn: '#aa4444', good: '#6fb8dd', data: '#0091ea', m: ['#0091ea', '#e0e0e0', '#b0b0b0', '#9fd8f5', '#cfcfcf', '#a8a8a8', '#5ec8f5', '#6a6a6a'] },
    { id: 'nord', name: 'Snowcrasher', mode: 'dark', bg: ['#0f1216', '#161a1f', '#1e242a'], ink: ['#eef2f6', '#b0bac2', '#737e88'], accent: ['#6fa8d4', '#9cc6e6', '#22415f'], warn: '#db5c34', good: '#9cc6e6', data: '#6fa8d4', m: ['#6fa8d4', '#db5c34', '#a88fd0', '#eaf3fb', '#f0a24e', '#9aa7c7', '#4fb0d8', '#4a6579'] },
    { id: 'lightcrasher', name: 'Lightcrasher', mode: 'light', bg: ['#f2f5f8', '#e9eef3', '#dfe6ec'], ink: ['#14181c', '#303941', '#525d66'], accent: ['#33719f', '#5490bd', '#1e4a6d'], warn: '#c64c26', good: '#2e7d99', data: '#33719f', m: ['#33719f', '#c64c26', '#705a9e', '#1c2f40', '#b06f22', '#5c6b8c', '#2688b3', '#4a6579'] },
    { id: 'solarized', name: 'Greco-Atlantic', mode: 'dark', bg: ['#0e1720', '#12202b', '#16242f'], ink: ['#b6c2c2', '#8ca0a8', '#647680'], accent: ['#5da7c0', '#7fc3d8', '#3b7c93'], warn: '#cb9a5e', good: '#7fbfa8', data: '#5da7c0', m: ['#5da7c0', '#6f8fb8', '#7fbfa8', '#b6c2c2', '#cb9a5e', '#9a8fc0', '#5fb8c7', '#4a6472'] },
    { id: 'gruvbox', name: 'GrecoGruv', mode: 'dark', bg: ['#282828', '#32302f', '#3c3836'], ink: ['#ebdbb2', '#c4b598', '#a89984'], accent: ['#fabd2f', '#fe8019', '#d79921'], warn: '#fe8019', good: '#b8bb26', data: '#83a598', m: ['#83a598', '#8ec07c', '#fb4934', '#b8bb26', '#fabd2f', '#d3869b', '#fe8019', '#458588'] },
    { id: 'everforest', name: 'Everforest', mode: 'dark', bg: ['#2d353b', '#343f44', '#3d484d'], ink: ['#d3c6aa', '#9da9a0', '#859289'], accent: ['#a7c080', '#83c092', '#677d54'], warn: '#dbbc7f', good: '#83c092', data: '#7fbbb3', m: ['#7fbbb3', '#a7c080', '#e67e80', '#83c092', '#dbbc7f', '#d699b6', '#e69875', '#859289'] },
    { id: 'ember', name: 'Ember', mode: 'dark', bg: ['#1f1610', '#281b13', '#2e1f17'], ink: ['#f3e7d9', '#b79b86', '#8a715f'], accent: ['#ea6e4b', '#f0996a', '#c84b22'], warn: '#dbbc7f', good: '#a6c07a', data: '#ea6e4b', m: ['#ea6e4b', '#e89a5c', '#e67e80', '#a6c07a', '#dbbc7f', '#8e7cc3', '#c84b22', '#b79b86'] },
    { id: 'carbon', name: 'Carbon Red', mode: 'dark', bg: ['#08080a', '#0d0d10', '#131317'], ink: ['#f5f5f7', '#b0b0b8', '#7c7c86'], accent: ['#e10600', '#ff241d', '#8c0d0a'], warn: '#ffb020', good: '#d4d4d8', data: '#ff5b54', m: ['#e10600', '#ff6b3d', '#ffb020', '#d4d4d8', '#ff241d', '#9aa0a6', '#c9302c', '#6e6e76'] },
    { id: 'sol', name: 'Sol', mode: 'light', bg: ['#f6f4ec', '#efece1', '#e7e3d5'], ink: ['#191813', '#35332a', '#57544a'], accent: ['#3e6484', '#5a7fa0', '#2b4a66'], warn: '#a4512f', good: '#47705a', data: '#3e6484', m: ['#3e6484', '#a4512f', '#6d5a8c', '#2e2c24', '#8a6a35', '#5c6b80', '#2e7d99', '#8a8574'] },
    { id: 'eink', name: 'E-Ink', mode: 'light', bg: ['#ffffff', '#ffffff', '#ffffff'], ink: ['#000000', '#000000', '#000000'], accent: ['#000000', '#000000', '#000000'], warn: '#000000', good: '#000000', data: '#000000', m: ['#000000', '#000000', '#000000', '#000000', '#000000', '#000000', '#000000', '#000000'] },
    { id: 'blackout', name: 'Blackout', mode: 'dark', bg: ['#000000', '#000000', '#0b0b0c'], ink: ['#eef0f2', '#aab0b6', '#767c82'], accent: ['#d5d8da', '#f2f4f6', '#6a7076'], warn: '#e2683c', good: '#d5d8da', data: '#eef0f2', m: ['#eef0f2', '#b9bec3', '#8b9096', '#d5d8da', '#a5aab0', '#626870', '#cfd3d7', '#4a5056'] }
  ];

  const KEY = 'aion-portal-theme'; // the prototype's key (code truth)

  function hex(c) {
    let s = String(c || '').replace('#', '');
    if (s.length === 3) s = s[0] + s[0] + s[1] + s[1] + s[2] + s[2];
    return [parseInt(s.slice(0, 2), 16) || 0, parseInt(s.slice(2, 4), 16) || 0, parseInt(s.slice(4, 6), 16) || 0];
  }
  function mix(a, b, t) {
    const x = hex(a), y = hex(b);
    const c = x.map(function (v, i) { return Math.round(v + (y[i] - v) * t); });
    return '#' + c.map(function (v) { return ('0' + v.toString(16)).slice(-2); }).join('');
  }
  function byId(id) {
    for (let i = 0; i < THEMES.length; i++) if (THEMES[i].id === id) return THEMES[i];
    return THEMES[0];
  }

  /* tokenObject returns a React style object — React writes `--*` custom
     properties through natively, so the whole contract rides one inline style
     on the shell root. */
  function tokenObject(id) {
    const t = byId(id);
    const eink = t.id === 'eink';
    const o = {
      '--bg-0': t.bg[0], '--bg-1': t.bg[1], '--bg-2': t.bg[2],
      '--sel': eink ? '#e8e8e8' : t.bg[2],
      '--ink': t.ink[0], '--ink-dim': t.ink[1], '--ink-faint': t.ink[2],
      '--ink-mute': eink ? '#000000' : mix(t.ink[2], t.bg[0], 0.34),
      '--line': eink ? '#000000' : mix(t.bg[0], t.ink[0], 0.17),
      '--line-soft': eink ? '#000000' : mix(t.bg[0], t.ink[0], 0.09),
      '--accent': t.accent[0], '--accent-bright': t.accent[1], '--accent-deep': t.accent[2],
      '--warn': t.warn, '--good': t.good, '--data': t.data,
      colorScheme: t.mode
    };
    for (let i = 0; i < 8; i++) o['--m' + (i + 1)] = t.m[i];
    return o;
  }

  function applyBody(id) {
    const t = byId(id);
    document.body.style.background = t.bg[0];
    document.documentElement.style.colorScheme = t.mode;
  }

  function load() {
    try {
      const v = localStorage.getItem(KEY);
      for (let i = 0; i < THEMES.length; i++) if (THEMES[i].id === v) return v;
    } catch (e) { /* private mode */ }
    return 'aion';
  }
  function save(id) {
    try { localStorage.setItem(KEY, id); } catch (e) { /* private mode */ }
  }

  window.PORTAL_THEMES = { THEMES: THEMES, byId: byId, mix: mix, tokenObject: tokenObject, applyBody: applyBody, load: load, save: save };
})();
