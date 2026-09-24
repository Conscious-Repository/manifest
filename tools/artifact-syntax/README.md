# Artifact syntax bundle

Run `npm ci --ignore-scripts` then `npm run build` here. The pinned lockfile builds
`server/web/vendor/artifact-syntax.js`; commit that file so production needs no
Node installation or external CDN. Copy highlight.js/LICENSE to the adjacent
artifact-syntax.LICENSE.txt when updating the dependency.

The bundle registers ten explicit grammars and exports only a string highlighter.
The shared artifact component owns byte/line limits, source-integrity checks,
DOM isolation, fallback, theme tokens and the plain-text toggle. It never calls
auto detection or scans the document for code blocks. See the official API:
https://highlightjs.readthedocs.io/en/latest/api.html
