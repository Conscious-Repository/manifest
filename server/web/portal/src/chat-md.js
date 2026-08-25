// CHAT-MD — a deliberately small markdown renderer for agent chat replies.
//
// The two portals are served from SEPARATE go:embed subtrees, so this file
// exists as two byte-identical copies (like chat-actions.js);
// server/web_chat_assets_test.go fails the build if they drift.
//
// Safety is by construction: render() builds React elements from text and
// never injects raw markup into the DOM, so nothing an agent (or a person
// quoted by an agent) writes can become live HTML. A repo test enforces it. Grammar, on purpose, is only what the
// agents actually emit: **bold**, *italic*, `code`, fenced blocks, # headings
// (styled spans, never real h1s), - and 1. lists one level deep, and bare
// http(s) links. Everything else is passed through as the text it is.
(function () {
  'use strict';

  var URL_RE = /(https?:\/\/[^\s<>")\]]+)/g;

  // inline parses **bold**, *italic*, `code` and links inside one line.
  function inline(R, text, keyBase) {
    var out = [];
    var k = 0;
    // tokenize on code spans first — their contents are verbatim
    var parts = String(text).split(/(`[^`]+`)/g);
    parts.forEach(function (part) {
      if (!part) return;
      if (part.length > 2 && part.charAt(0) === '`' && part.charAt(part.length - 1) === '`') {
        out.push(R.createElement('code', { key: keyBase + '-' + (k++), className: 'md-code' }, part.slice(1, -1)));
        return;
      }
      // bold, then italic, inside the non-code run
      var boldParts = part.split(/(\*\*[^*]+\*\*)/g);
      boldParts.forEach(function (bp) {
        if (!bp) return;
        if (bp.length > 4 && bp.indexOf('**') === 0 && bp.lastIndexOf('**') === bp.length - 2) {
          out.push(R.createElement('strong', { key: keyBase + '-' + (k++) }, inline(R, bp.slice(2, -2), keyBase + 'b' + k)));
          return;
        }
        var italParts = bp.split(/(\*[^*\s][^*]*\*)/g);
        italParts.forEach(function (ip) {
          if (!ip) return;
          if (ip.length > 2 && ip.charAt(0) === '*' && ip.charAt(ip.length - 1) === '*') {
            out.push(R.createElement('em', { key: keyBase + '-' + (k++) }, ip.slice(1, -1)));
            return;
          }
          // linkify bare http(s) URLs — never any other scheme
          var linkParts = ip.split(URL_RE);
          linkParts.forEach(function (lp) {
            if (!lp) return;
            if (lp.indexOf('http://') === 0 || lp.indexOf('https://') === 0) {
              out.push(R.createElement('a', {
                key: keyBase + '-' + (k++), href: lp, target: '_blank',
                rel: 'noopener noreferrer', className: 'md-link'
              }, lp));
            } else {
              out.push(lp);
            }
          });
        });
      });
    });
    return out;
  }

  // render turns a whole message into a block-element tree.
  function render(text, R) {
    var lines = String(text || '').split('\n');
    var blocks = [];
    var i = 0, key = 0;
    while (i < lines.length) {
      var line = lines[i];

      // fenced block — verbatim until the closing fence
      if (/^\s*```/.test(line)) {
        var fence = [];
        i++;
        while (i < lines.length && !/^\s*```/.test(lines[i])) { fence.push(lines[i]); i++; }
        i++; // the closing fence (or EOF)
        blocks.push(R.createElement('pre', { key: 'k' + (key++), className: 'md-pre' }, fence.join('\n')));
        continue;
      }

      // list run — consecutive -/* or "1." lines, one nesting level
      if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
        var items = [];
        var ordered = /^\s*\d+\./.test(line); // the run's first marker decides ol vs ul
        while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) {
          var m = lines[i].match(/^(\s*)([-*]|\d+\.)\s+(.*)$/);
          items.push({ nested: m[1].length >= 2, text: m[3] });
          i++;
        }
        var lis = [];
        var j = 0;
        while (j < items.length) {
          if (!items[j].nested) {
            var kids = [inline(R, items[j].text, 'li' + key + '-' + j)];
            var sub = [];
            var j2 = j + 1;
            while (j2 < items.length && items[j2].nested) { sub.push(items[j2]); j2++; }
            if (sub.length) {
              kids.push(R.createElement('ul', { key: 'sub' + j, className: 'md-list' },
                sub.map(function (si, si2) {
                  return R.createElement('li', { key: si2 }, inline(R, si.text, 'sli' + key + '-' + j + '-' + si2));
                })));
            }
            lis.push(R.createElement('li', { key: j }, kids));
            j = j2;
          } else {
            // an orphan nested item — render flat rather than losing it
            lis.push(R.createElement('li', { key: j }, inline(R, items[j].text, 'li' + key + '-' + j)));
            j++;
          }
        }
        blocks.push(R.createElement(ordered ? 'ol' : 'ul', { key: 'k' + (key++), className: 'md-list' }, lis));
        continue;
      }

      // heading — styled span, never a real h1 (a chat message is not a page)
      var h = line.match(/^\s*(#{1,4})\s+(.*)$/);
      if (h) {
        blocks.push(R.createElement('div', { key: 'k' + (key++), className: 'md-h' + h[1].length },
          inline(R, h[2], 'h' + key)));
        i++;
        continue;
      }

      // blank line → paragraph gap
      if (!line.trim()) {
        blocks.push(R.createElement('div', { key: 'k' + (key++), className: 'md-gap' }));
        i++;
        continue;
      }

      // plain paragraph line (pre-wrap CSS keeps intra-paragraph breaks)
      blocks.push(R.createElement('div', { key: 'k' + (key++), className: 'md-p' }, inline(R, line, 'p' + key)));
      i++;
    }
    return R.createElement('div', { className: 'md-body' }, blocks);
  }

  window.CHAT_MD = { render: render };
})();
