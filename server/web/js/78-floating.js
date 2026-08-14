// ---- floating windows: ⌘J quick-chat · ⌘I sticky note (cmd-ctr import P4) ----
// Draggable glass panels mounted on <body> OUTSIDE tab routing, so they
// survive navigation. Position persists per key in localStorage. Desktop-only
// (a floating window over a phone viewport is a trap — the CHAT tab serves
// mobile). Both are thin consumers of existing plumbing: the quick-chat
// drives the same /api/chat core as the CHAT tab against one dedicated
// session; the sticky is one dataDir file behind GET/PUT /api/sticky.

function floatWin(key, title, buildBody) {
  const saved = (() => { try { return JSON.parse(localStorage.getItem("manifest.float." + key)) || {}; } catch (e) { return {}; } })();
  const win = el("div", "float-win");
  win.style.left = (saved.x ?? (window.innerWidth - 440)) + "px";
  win.style.top = (saved.y ?? 90) + "px";
  if (saved.w) win.style.width = saved.w + "px";
  if (saved.h) win.style.height = saved.h + "px";
  const head = el("div", "float-head");
  head.append(el("span", "float-title", title));
  const close = el("button", "float-close", "✕");
  head.append(close);
  win.append(head);
  const body = el("div", "float-body");
  win.append(body);
  buildBody(body, win);

  // drag by the header; persist position + size on release
  let drag = null;
  head.addEventListener("mousedown", (e) => {
    if (e.target === close) return;
    drag = { dx: e.clientX - win.offsetLeft, dy: e.clientY - win.offsetTop };
    e.preventDefault();
  });
  window.addEventListener("mousemove", (e) => {
    if (!drag) return;
    win.style.left = Math.max(0, e.clientX - drag.dx) + "px";
    win.style.top = Math.max(0, e.clientY - drag.dy) + "px";
  });
  window.addEventListener("mouseup", () => {
    if (!drag) return;
    drag = null;
    persist();
  });
  const persist = () => {
    try {
      localStorage.setItem("manifest.float." + key, JSON.stringify({
        x: win.offsetLeft, y: win.offsetTop, w: win.offsetWidth, h: win.offsetHeight,
      }));
    } catch (e) {}
  };
  new ResizeObserver(persist).observe(win);
  close.onclick = () => { win.remove(); };
  document.body.append(win);
  return win;
}

function floatToggle(key, title, buildBody) {
  const open = document.querySelector('.float-win[data-key="' + key + '"]');
  if (open) { open.remove(); return; }
  if (window.mf && window.mf.phone()) { showToast("Floating windows are desktop-only"); return; }
  const win = floatWin(key, title, buildBody);
  win.dataset.key = key;
}

// ---- ⌘I sticky note ----

function toggleSticky() {
  floatToggle("sticky", "sticky", (body) => {
    const ta = document.createElement("textarea");
    ta.className = "float-sticky-area";
    ta.placeholder = "scratch — saves as you type (dataDir, not the vault)";
    ta.spellcheck = false;
    body.append(ta);
    fetch("/api/sticky").then((r) => r.json()).then((d) => { ta.value = d.body || ""; }).catch(() => {});
    let t;
    ta.addEventListener("input", () => {
      clearTimeout(t);
      t = setTimeout(() => {
        fetch("/api/sticky", { method: "PUT", body: ta.value }).catch(() => {});
      }, 800);
    });
    setTimeout(() => ta.focus(), 50);
  });
}

// ---- ⌘J quick-chat ----
// One dedicated concierge session (id remembered per browser); the mini view
// renders the last few turns and polls while thinking — same file-derived
// liveness as the CHAT tab, in miniature.

let quickChatPoll = null;

async function quickChatSession() {
  let id = localStorage.getItem("manifest.quickchat.session") || "";
  if (id) {
    try {
      const res = await fetch("/api/chat/sessions/" + encodeURIComponent(id));
      if (res.ok) return id;
    } catch (e) {}
  }
  const r = await postJSONOk("/api/chat/sessions", { spirit: "concierge", title: "quick chat" });
  localStorage.setItem("manifest.quickchat.session", r.id);
  return r.id;
}

function toggleQuickChat() {
  floatToggle("quickchat", "quick chat · concierge", (body, win) => {
    const log = el("div", "float-chat-log");
    const row = el("div", "float-chat-row");
    const ta = document.createElement("textarea");
    ta.className = "float-chat-input";
    ta.rows = 1;
    ta.placeholder = "ask concierge…";
    const full = el("button", "sprt-quiet", "open full ↗");
    row.append(ta);
    if (typeof micButton === "function") {
      row.append(micButton((text) => {
        ta.value = (ta.value ? ta.value + " " : "") + text;
        ta.focus();
      }));
    }
    row.append(full);
    body.append(log, row);

    let sid = "";
    const render = async () => {
      if (!sid) return;
      let d;
      try {
        const res = await fetch("/api/chat/sessions/" + encodeURIComponent(sid));
        if (!res.ok) return;
        d = await res.json();
      } catch (e) { return; }
      const stick = log.scrollTop + log.clientHeight >= log.scrollHeight - 40;
      log.innerHTML = "";
      const turns = parseChatTurns(d.body || "").slice(-6);
      turns.forEach((t) => {
        if (t.who === "user") {
          log.append(el("div", "float-chat-user", t.text));
        } else if (t.who !== "system") {
          const say = parseChatSteps(t.text).find((s) => s.cast === "say");
          const div = el("div", "float-chat-say");
          try { div.append(renderMarkdown((say && say.body) || t.text, "", { readOnly: true })); }
          catch (e) { div.textContent = (say && say.body) || t.text; }
          log.append(div);
        }
      });
      (d.queued || []).forEach((q) => log.append(el("div", "float-chat-user queued", q)));
      if (d.session.status === "thinking") log.append(el("div", "chat-thinking", "✦ thinking…"));
      if (stick) log.scrollTop = log.scrollHeight;
      const busy = d.session.status === "thinking" || (d.queued || []).length;
      if (busy && !quickChatPoll) {
        quickChatPoll = setInterval(() => {
          if (!document.body.contains(win)) { clearInterval(quickChatPoll); quickChatPoll = null; return; }
          render();
        }, 1500);
      } else if (!busy && quickChatPoll) {
        clearInterval(quickChatPoll); quickChatPoll = null;
      }
    };

    quickChatSession().then((id) => { sid = id; render(); });
    ta.addEventListener("keydown", async (e) => {
      if (e.key !== "Enter" || e.shiftKey) return;
      e.preventDefault();
      const text = ta.value.trim();
      if (!text || !sid) return;
      ta.value = "";
      try {
        await postJSONOk("/api/chat/sessions/" + encodeURIComponent(sid) + "/messages", { text });
        render();
      } catch (err) { showToast("Send failed — " + (err.message || "error")); ta.value = text; }
    });
    full.onclick = () => { if (sid) { win.remove(); chatOpenSession(sid); } };
    setTimeout(() => ta.focus(), 50);
  });
}
