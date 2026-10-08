# Construction Intelligence — P0 geometry/export spike

A bounded proof artifact for the go/no-go questions in the Construction
Intelligence plan (§5, P0.2). It is **not** the product: `construction/` owns
the product geometry, GLB, sections and drawings. Nothing here is wired into
the server.

What it proves, with the tests that prove it:

| Question | Evidence |
|---|---|
| A pure-Go compiler gives byte-identical GLB per input | `TestSpikeDeterministicGLB` |
| GLB is well formed, embeds its buffer, names every part, and a 1000 mm cube is exactly 1 m in glTF | `TestSpikeGLBStructureAndCalibrationCube` |
| World (mm, X along wall, Y outward, Z up) → glTF (m, Y up) is `(x, z, -y)/1000`, right-handed and invertible | `TestSpikeBasisMap` |
| The roof slopes down as +Y; corrugation tessellation states its chord error | `TestSpikeSlopeDirectionAndChordError` |
| Sections come from solid boundaries, close exactly, and resolve coplanar cuts once | `TestSpikeSectionCubeExact`, `TestSpikeSectionCoplanarFace`, `TestSpikeObliqueSection` |
| A vector PDF prints at a stated scale (100 mm at 1:5 = 56.693 pt), escapes text, has no raster | `TestSpikeVectorPDF` |
| Pinned three.js draws the compiled parts and picks them by semantic id | `viewer.cjs` |

Primitives: every solid is a prism swept from a two-rail "ribbon" profile
(sheets, flashings, rectangles), so caps triangulate as quad strips without a
general polygon triangulator. Sections key each intersection point by its
shared mesh edge and orient segments from vertex classification and winding
alone, so a cut through a vertex or along a face still yields closed loops.

Run:

```sh
go test -v ./tools/construction-spike -count=1
node tools/construction-spike/viewer.cjs   # needs NODE_PATH with playwright
```

`viewer.cjs` serves only `server/web/vendor/three-0.180.0/` and the committed
`testdata/spike-scene.json` on 127.0.0.1, fails on any non-loopback request,
and records which WebGL implementation ran. Headless Chromium here uses
SwiftShader (software WebGL2): that proves the code path, not device
performance or realistic-render quality.
