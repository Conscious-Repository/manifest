# Writing editor vendor bundle

CodeMirror 6, MIT-licensed, built from the exact versions and transitive tree
in `package-lock.json`. The generated browser bundle and license notices live
under `server/web/vendor/`; Manifest embeds them in its Go binary. No CDN,
production Node process, framework, or runtime npm installation is required.

Rebuild from this directory:

```sh
npm ci
npm run build
node licenses.cjs
```

After the locked dependencies are cached locally, `npm ci --offline` supports
an offline rebuild. A new dependency version requires fetching it first. Upgrade
by reviewing the lockfile delta, regenerating the bundle/licenses, and running
the editor interaction checks in `docs/writing-workspace.md`. Do not hand-edit
the generated bundle.

The bundle is approximately 550 KiB uncompressed. It includes editor state,
view, Markdown/HTML language support, history, selection, search, completion,
and their Lezer parser dependencies. The lockfile is the full footprint inventory.
The editor adapter itself is `editor.js`; this is an isolated vendor-build tool,
not a production application build pipeline.

From the repository root, test draft and anchor behavior with:

```sh
node --test tools/writing-editor/recovery.test.cjs
```
