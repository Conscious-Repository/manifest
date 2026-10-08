// P0 browser proof: the pinned, vendored three.js (server/web/vendor/three-0.180.0)
// draws the Go spike's compiled parts in headless Chromium, and a click at the
// projected centre of a part selects that part by its semantic id (never by
// triangle order). It also records the WebGL renderer actually used, because
// software WebGL (SwiftShader) proves the code path, not device performance.
//
//   node tools/construction-spike/viewer.cjs
//
// Serves files from the repository only, on 127.0.0.1; no network.
const {chromium} = require('playwright');
const fs = require('node:fs'), path = require('node:path'), http = require('node:http'), assert = require('node:assert/strict');
const root = path.join(__dirname, '../..');
const scene = JSON.parse(fs.readFileSync(path.join(__dirname, 'testdata/spike-scene.json'), 'utf8'));
const page = `<!doctype html><meta charset="utf-8"><style>html,body{margin:0;background:#fff}canvas{display:block}</style>
<canvas id="c" width="800" height="600"></canvas>
<script type="module">
import * as THREE from '/server/web/vendor/three-0.180.0/three.module.min.js';
const scene = await (await fetch('/scene.json')).json();
const canvas = document.getElementById('c');
const renderer = new THREE.WebGLRenderer({canvas, antialias: true, preserveDrawingBuffer: true});
const gl = renderer.getContext();
const dbg = gl.getExtension('WEBGL_debug_renderer_info');
const world = new THREE.Group();
// world mm, Z-up → three m, Y-up: (x, z, -y) / 1000, the same map as the GLB
world.rotation.x = -Math.PI / 2; world.scale.setScalar(1 / 1000);
const colors = {'corrugated-panel': 0x8a9199, 'bent-flashing': 0x5f6b75, 'outer-wythe': 0xb0624a, 'inner-wythe': 0x9c5a46, 'calibration-cube': 0xcccccc};
const meshes = [];
for (const part of scene.parts) {
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(part.positions, 3));
  g.setIndex(part.indices);
  g.computeVertexNormals();
  const m = new THREE.Mesh(g, new THREE.MeshLambertMaterial({color: colors[part.part] || 0x999999}));
  m.userData.part = part.part;
  world.add(m); meshes.push(m);
  world.add(new THREE.LineSegments(new THREE.EdgesGeometry(g, 20), new THREE.LineBasicMaterial({color: 0x222222})));
}
const s = new THREE.Scene();
s.background = new THREE.Color(0xffffff);
s.add(world, new THREE.HemisphereLight(0xffffff, 0x666666, 2.2));
const camera = new THREE.PerspectiveCamera(35, 800 / 600, 0.01, 100);
// outside the building (world +Y is three -Z), above the roof, looking back at the wall
camera.position.set(0.6, 1.4, -3.6); camera.lookAt(0.9, 0.1, -0.3);
renderer.render(s, camera);
world.updateMatrixWorld(true);
// project a part's bounding-box centre to the canvas
function centreOf(name) {
  const m = meshes.find((x) => x.userData.part === name);
  const box = new THREE.Box3().setFromObject(m);
  const c = box.getCenter(new THREE.Vector3()).project(camera);
  return {x: (c.x + 1) / 2 * 800, y: (1 - c.y) / 2 * 600};
}
function pick(x, y) {
  const ray = new THREE.Raycaster();
  ray.setFromCamera(new THREE.Vector2(x / 800 * 2 - 1, -(y / 600) * 2 + 1), camera);
  const hit = ray.intersectObjects(meshes.filter((m) => m.visible), false)[0];
  return hit ? hit.object.userData.part : null;
}
function setVisible(name, on) { meshes.find((x) => x.userData.part === name).visible = on; renderer.render(s, camera); }
const cube = meshes.find((x) => x.userData.part === 'calibration-cube');
const cubeBox = new THREE.Box3().setFromObject(cube);
window.__spike = {
  revision: THREE.REVISION,
  webgl2: renderer.capabilities.isWebGL2,
  renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER),
  cubeSizeM: cubeBox.getSize(new THREE.Vector3()).toArray(),
  centre: {wythe: centreOf('outer-wythe'), cube: centreOf('calibration-cube')},
  pick, setVisible
};
</script>`;
const types = {'.js': 'text/javascript', '.json': 'application/json'};
const server = http.createServer((req, res) => {
  const p = new URL(req.url, 'http://x').pathname;
  if (p === '/') { res.writeHead(200, {'Content-Type': 'text/html'}); return res.end(page); }
  if (p === '/scene.json') { res.writeHead(200, {'Content-Type': 'application/json'}); return res.end(JSON.stringify(scene)); }
  const file = path.join(root, p);
  if (!file.startsWith(path.join(root, 'server/web/vendor/three-0.180.0/')) || !fs.existsSync(file)) { res.writeHead(404); return res.end(); }
  res.writeHead(200, {'Content-Type': types[path.extname(file)] || 'application/octet-stream'});
  fs.createReadStream(file).pipe(res);
});
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch({headless: true});
  const out = process.env.SPIKE_SCREENSHOT || '/tmp/manifest-construction-qa/spike-viewer.png';
  try {
    const context = await browser.newContext({viewport: {width: 800, height: 600}});
    const tab = await context.newPage();
    const errors = [];
    tab.on('pageerror', (e) => errors.push(e.message));
    const external = [];
    tab.on('request', (r) => { if (!r.url().startsWith('http://127.0.0.1:')) external.push(r.url()); });
    await tab.goto('http://127.0.0.1:' + server.address().port + '/');
    await tab.waitForFunction(() => window.__spike, null, {timeout: 30000});
    const info = await tab.evaluate(() => ({revision: __spike.revision, webgl2: __spike.webgl2, renderer: __spike.renderer, cubeSizeM: __spike.cubeSizeM, centre: __spike.centre}));
    assert.equal(info.revision, '180', 'pinned three.js r180');
    assert.equal(info.webgl2, true, 'WebGL2 context');
    info.cubeSizeM.forEach((v) => assert.ok(Math.abs(v - 1) < 1e-6, '1000 mm cube is 1 m in three: ' + info.cubeSizeM));
    // semantic picking: a click names the nearest visible part by its id.
    // At the wythe's projected centre the flashing upstand is in front of
    // the wall; hiding it (a view state, not a geometry edit) exposes the
    // wythe at the very same pixel.
    const pickAt = (at) => tab.evaluate(([x, y]) => __spike.pick(x, y), [at.x, at.y]);
    assert.equal(await pickAt(info.centre.cube), 'calibration-cube');
    assert.equal(await pickAt(info.centre.wythe), 'bent-flashing');
    await tab.evaluate(() => __spike.setVisible('bent-flashing', false));
    assert.equal(await pickAt(info.centre.wythe), 'outer-wythe');
    await tab.evaluate(() => __spike.setVisible('bent-flashing', true));
    // the canvas actually drew something other than background
    const drawn = await tab.evaluate(() => {
      const c = document.getElementById('c'), g = c.getContext('webgl2');
      const px = new Uint8Array(4 * 800 * 600);
      g.readPixels(0, 0, 800, 600, g.RGBA, g.UNSIGNED_BYTE, px);
      let n = 0; for (let i = 0; i < px.length; i += 4) if (px[i] < 250 || px[i + 1] < 250 || px[i + 2] < 250) n++;
      return n;
    });
    assert.ok(drawn > 20000, 'rendered pixels: ' + drawn);
    assert.deepEqual(errors, []);
    assert.deepEqual(external, [], 'no request leaves 127.0.0.1');
    fs.mkdirSync(path.dirname(out), {recursive: true});
    await tab.screenshot({path: out});
    console.log(JSON.stringify({ok: true, three: info.revision, webgl2: info.webgl2, renderer: info.renderer, drawnPixels: drawn, screenshot: out}));
  } finally {
    await browser.close();
    server.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });
