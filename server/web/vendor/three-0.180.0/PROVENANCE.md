# three.js 0.180.0 — vendored for Construction Intelligence

Pinned, unmodified files from the npm package `three@0.180.0` (MIT, see
`LICENSE`). Used only by `js/87-construction-renderer.js`, which loads
`three.module.min.js` with a dynamic `import()` when a construction
workbench opens. No CDN, no runtime install, no other three.js files.

Provenance (verified offline on 2026-10-07):

- Registry tarball: `https://registry.npmjs.org/three/-/three-0.180.0.tgz`
- Registry integrity (npm lock / npm cache index):
  `sha512-o+qycAMZrh+TsE01GqWUxUIKR1AL0S8pq7zDkYOQw8GqfX8b8VoCKYUoHbhiX5j+7hr8XsuHDVU6+gkQJQKg9w==`
- The tarball bytes were read from the local npm content cache and their
  SHA-512 recomputed: it equals the integrity above. The files below were
  extracted from that tarball, not copied from an installed tree.

| File | Bytes | SHA-256 |
|---|---:|---|
| `three.module.min.js` | 338908 | `e2b5ee6bccd38fd6d8a2428546b83c5f2426d84b152ef82be8055556e3b40eb6` |
| `three.core.min.js` | 381124 | `61ba0df005b05991361d040d8ff670e1aadfd0ce7aeebd1fdb0725957a8957de` |
| `LICENSE` | 1081 | `bfe119ea4fd413f5f7ca3fcd63adb0c4a073ed39daa2fe7d3e6b769e21272601` |

`three.module.min.js` imports `./three.core.min.js`; the original file
names are kept so neither file is edited. `server/construction_vendor_test.go`
pins these hashes so an accidental edit or upgrade fails a test.
