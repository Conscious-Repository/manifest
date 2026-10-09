// ================= app update notice (2026-10-09) =================
// A deploy re-hashes every script and stylesheet URL (?v=), but a page that
// stays open — an iPhone home-screen app most of all — keeps running the code
// it started with. When the app comes back to the foreground (and every
// fifteen minutes while it's open) this asks for the current page and
// compares the asset versions it names with the ones running. If they
// differ, a small bar offers Reload. It never reloads by itself: a draft in
// progress stays where it is until you choose.
(function () {
  const tokens = (urls) => urls.map((u) => (/[?&]v=([^&"']+)/.exec(u) || [])[1]).filter(Boolean).sort().join(",");
  const running = tokens([...document.querySelectorAll("script[src], link[rel=stylesheet][href]")].map((n) => n.getAttribute("src") || n.getAttribute("href")));
  if (!running) return;
  let last = 0, shown = false;
  async function check() {
    if (shown || document.hidden || Date.now() - last < 60000) return;
    last = Date.now();
    try {
      const res = await fetch("/", { cache: "no-store", credentials: "same-origin" });
      if (!res.ok) return;
      const html = await res.text();
      const now = tokens([...html.matchAll(/(?:src|href)="([^"]+\?v=[^"]+)"/g)].map((m) => m[1]));
      if (now && now !== running) show();
    } catch (e) {}
  }
  function show() {
    shown = true;
    const bar = document.createElement("div");
    bar.className = "app-update";
    bar.setAttribute("role", "status");
    const t = document.createElement("span");
    t.textContent = "A new version is ready";
    const go = document.createElement("button");
    go.type = "button"; go.className = "app-update-go"; go.textContent = "Reload";
    go.onclick = () => location.reload();
    const x = document.createElement("button");
    x.type = "button"; x.className = "app-update-x"; x.textContent = "✕"; x.setAttribute("aria-label", "Not now");
    x.onclick = () => { bar.remove(); shown = false; last = Date.now() + 30 * 60000; };
    bar.append(t, go, x);
    document.body.append(bar);
  }
  document.addEventListener("visibilitychange", () => { if (!document.hidden) check(); });
  window.addEventListener("focus", check);
  setInterval(check, 15 * 60000);
})();
