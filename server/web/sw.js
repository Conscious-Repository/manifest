// Manifest service worker (cmd-ctr import P5) — exists for INSTALLABILITY and
// an offline shell, not performance: network-first, /api/* never cached.
// Bump CACHE on any manifest.webmanifest OR sw.js change (Chrome re-reads it on
// SW update, which is how share_target edits propagate).
const CACHE = "manifest-shell-v4";

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.add("/")).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.origin !== location.origin) return;
  if (url.pathname.startsWith("/api/")) return; // live data only — never cached
  // The cached shell answers NAVIGATIONS only. A deploy re-hashes the ?v= on
  // every js/css ref, so during the restart window those URLs are guaranteed
  // cache misses; handing one index.html means the browser gets HTML where it
  // asked for a script ("Unexpected token '<'"), that module never defines its
  // globals, and the app paints its static shell with every JS-filled panel
  // empty. A sub-resource that can't be fetched must fail AS a sub-resource.
  const navigate = e.request.mode === "navigate";
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        // only a complete same-origin 200 is worth keeping — cache a 404/500
        // body and the offline shell replays the error forever
        if (res.ok && res.type === "basic") {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {});
        }
        return res;
      })
      .catch(() =>
        caches.match(e.request).then((hit) => {
          if (hit) return hit;
          return navigate ? caches.match("/").then((s) => s || Response.error()) : Response.error();
        })
      )
  );
});
