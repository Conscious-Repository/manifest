# Code artifact syntax preview

Code artifact previews now lazily load a locally served, pinned highlight.js bundle with JavaScript, TypeScript, Python, Go, JSON, Bash, CSS, XML/HTML/SVG, SQL and YAML grammars. Unknown extensions remain plain text. No CDN, automatic language guessing, remote source execution or document-wide highlighting is used. The lockfile, bundle source, generated asset and license are checked in.

The shared renderer verifies that highlighted output reconstructs the exact original text, including CRLF, and permits only span/class markup before displaying it. Literal HTML remains text. A toggle switches to original plain text, with its setting restored for the selected immutable version. Existing edit, review and source-line anchors remain tied to the original artifact bytes. Theme colors use shared tokens and the toggle has a touch-sized target.

Highlighting is bounded to 64 KiB UTF-8 and 2,000 lines. Larger files, load failures and parser/integrity failures retain original text with an explanation. No source is truncated. This delivers code-file syntax preview; syntax inside diff hunks and other unchecked plan requirements remain open.

Evidence: real Chromium fixture covers the local lazy bundle, all ten grammars, CRLF/literal markup preservation, toggle restoration, version-specific line review, oversize/error fallback, and both themes at 320/390/1440px. Phone screenshot inspected. Existing saved-receipt and workspace recovery browser fixtures passed. Build, JS syntax and diff checks passed. `make test` passed server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files. No real owner reviews or provider messages were submitted.

The bundle uses the documented explicit-language highlighting API: https://highlightjs.readthedocs.io/en/latest/api.html .
