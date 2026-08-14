/* Loads data/*.json + content/*.md (all ?t= cache-busted, never throws).
   Result shape:
     { finances, vto, goals, backlog, heuristics, people, meta,   — objects | null
       hiring, references,                                        — md text | null
       errors: { <key>: 'missing' | 'drift' } }
   A missing file → null (section renders "no data yet"); a file that loads
   but fails its shape check → null + errors[key]='drift' (quiet footnote).
   The page never blanks. */

(function () {
  'use strict';

  var U = window.PORTAL_UTIL;

  // key → [path, minimal shape check on the parsed value]
  var JSON_FILES = {
    finances:   ['data/finances.json',   function (v) { return v && typeof v === 'object'; }],
    vto:        ['data/vto.json',        function (v) { return v && typeof v === 'object'; }],
    goals:      ['data/goals.json',      function (v) { return v && Array.isArray(v.goals); }],
    backlog:    ['data/backlog.json',    function (v) { return v && Array.isArray(v.items); }],
    heuristics: ['data/heuristics.json', function (v) { return v && Array.isArray(v.heuristics); }],
    people:     ['data/people.json',     function (v) { return v && Array.isArray(v.people); }],
    meta:       ['data/meta.json',       function (v) { return v && typeof v === 'object'; }],
    // team layer (Phase 2–3): server endpoints, not static files. 404 (team
    // layer disabled) → null, and the portal renders exactly as before.
    team:       ['api/team/state',       function (v) { return v && typeof v === 'object'; }],
    me:         ['api/me',               function (v) { return v && typeof v === 'object'; }]
  };
  var MD_FILES = {
    hiring:     'content/hiring.md',
    references: 'content/references.md'
  };

  window.loadPortalData = function () {
    var keys = [];
    var jobs = [];

    Object.keys(JSON_FILES).forEach(function (k) {
      keys.push(k);
      jobs.push(U.fetchJSON(JSON_FILES[k][0]));
    });
    Object.keys(MD_FILES).forEach(function (k) {
      keys.push(k);
      jobs.push(U.fetchText(MD_FILES[k]));
    });

    return Promise.all(jobs).then(function (results) {
      var out = { errors: {} };
      results.forEach(function (r, i) {
        var k = keys[i];
        if (!r.ok) {
          out[k] = null;
          out.errors[k] = r.error;
          return;
        }
        var check = JSON_FILES[k] && JSON_FILES[k][1];
        if (check && !check(r.value)) {
          out[k] = null;
          out.errors[k] = 'drift';
          return;
        }
        out[k] = r.value;
      });

      // people.json → { initials: {name, role} } for PORTAL_UTIL.personName
      if (out.people) {
        var map = {};
        out.people.people.forEach(function (p) {
          if (p && p.initials) map[p.initials] = { name: p.name || p.initials, role: p.role || '' };
        });
        window.PORTAL_PEOPLE = map;
      }

      return out;
    });
  };
})();
