// CHAT ACTIONS — the composer's words and the ask|delegate wire mapping.
//
// The two portals are served from SEPARATE go:embed subtrees (server/portal.go
// does fs.Sub over web/portal | web/ooda), so they cannot share one file. These
// two copies must stay byte-identical: server/web_chat_assets_test.go fails the
// build if they drift.
//
// The UI labels by OUTPUT — what you get back — while the wire still speaks the
// backend's two rituals. ritualOf() is the only place the two vocabularies meet.
(function () {
  'use strict';
  var ASK = 'ask', DELEGATE = 'delegate';
  window.CHAT_ACTIONS = {
    ASK: ASK,
    DELEGATE: DELEGATE,
    ask: { key: ASK, label: 'ask', sub: 'an answer from what it can read — nothing is written' },
    propose: { key: DELEGATE, label: 'send & propose', sub: 'comes back as a change you approve or discard' },
    assurance: 'Nothing is applied to your records unless you approve it.',
    busyLabel: 'busy',
    placeholder: 'type your question',
    // attachments — the composer's paperclip. Documents are read; images are
    // looked at. Anything a browser would execute is refused server-side.
    attach: {
      label: '\uD83D\uDCCE attach',
      accept: '.pdf,.docx,.xlsx,.csv,.tsv,.txt,.md,.json,.log,.png,.jpg,.jpeg,.heic,.webp,.gif',
      hint: 'pdf · word · spreadsheet · image — up to 25 MB',
      uploading: function (n) { return 'uploading ' + n + '\u2026'; },
      failed: function (n, why) { return "couldn't attach " + n + (why ? ' \u2014 ' + why : ''); }
    },
    // bytes are shared, names are not: the same file sent twice is stored once
    // and each sender keeps their own name for it.
    attachID: function (hash) { return 'file/' + hash; },
    isAttach: function (id) { return String(id || '').indexOf('file/') === 0; },
    attachHash: function (id) { return String(id || '').slice(5); },
    // ritualOf maps a UI action key to the wire value the API takes. The server
    // re-normalizes anyway (portal_agent.go chatAskFor) — this is belt and braces.
    ritualOf: function (key) { return key === DELEGATE ? DELEGATE : ASK; },
    // busyNote names WHOSE run is holding the one-run-at-a-time gate, so a
    // teammate's turn doesn't read as a dead button. engine is the sidebar
    // snapshot (server/chat_kairos.go chatEngine).
    busyNote: function (engine) {
      var active = engine && engine.active;
      var pending = (engine && engine.pending) || [];
      var who = (active && active.by) || (pending[0] && pending[0].by) || '';
      var what = (active && active.ritual) || (pending[0] && pending[0].ritual) || 'run';
      if (what === DELEGATE) what = 'proposal run';
      if (!who) return 'one run at a time — this queues behind the run in flight';
      return 'one run at a time — this queues behind ' + who + '’s ' + what;
    }
  };
})();
