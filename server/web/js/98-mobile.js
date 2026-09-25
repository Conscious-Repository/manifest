// ================= 98-mobile — phone-band chrome (Rev 4, <860px) =================
// Drawer nav, mobile top bar, and the bottom-sheet primitive. Loaded BEFORE
// 99-boot: this file only DEFINES things and wires listeners on static DOM
// (#appShell/#crumbBar exist — scripts sit at the end of <body>); every call
// into boot-defined globals (openCmdbar, openTodoQuickAdd, renderAion, …)
// happens at event time. Desktop (≥861px) is untouched by construction: all
// nodes built here are display:none outside the 95-mobile.css media block, and
// every behavior checks mf.phone() first. matchMedia everywhere — never
// innerWidth — so JS and CSS agree at exactly 860.

(function () {
  const mqPhone = window.matchMedia("(max-width: 860px)");
  const shell = document.getElementById("appShell");
  const crumbBar = document.getElementById("crumbBar");

  // ---- public namespace ----
  window.mf = { phone: () => mqPhone.matches };

  // A phone conversation gets the viewport; its directory is an explicit fold.
  // An open conversation reaches the fold from its own head (48-chat.js
  // chatBackToChats → mf.openChats); this shell-level toggle stays for the
  // landing (no head) and, as "Close chats", for the open list.
  const chatShell = document.querySelector(".chat-shell");
  const chatRail = document.getElementById("chatRail");
  if (chatShell && chatRail) {
    const toggle = document.createElement("button");
    toggle.className = "mf-chat-toggle rec-linkish";
    toggle.setAttribute("aria-label", "Conversations");
    toggle.setAttribute("aria-controls", "chatRail");
    const setOpen = (open) => {
      chatShell.classList.toggle("mf-chat-nav-open", open);
      toggle.setAttribute("aria-expanded", String(open));
      toggle.textContent = open ? "Close chats" : "‹ Chats";
    };
    setOpen(false);
    // The open list is a history entry (same URL, state mfChats), so the
    // phone's Back closes it instead of leaving the conversation — measured
    // 2026-09-25: Back with the list open went to the page before Chat.
    // Closing it from the page pops that entry again; a row click navigates
    // on top of it (one same-URL entry remains, harmless).
    const openList = () => {
      setOpen(true);
      if (!history.state?.mfChats) history.pushState({ ...(history.state || {}), mfChats: true }, "");
    };
    const closeList = () => {
      setOpen(false);
      if (history.state?.mfChats) history.back();
    };
    toggle.onclick = () => (chatShell.classList.contains("mf-chat-nav-open") ? closeList() : openList());
    chatShell.prepend(toggle);
    window.mf.openChats = () => { if (mqPhone.matches) { openList(); toggle.focus({ preventScroll: true }); } };
    window.mf.closeChats = closeList;
    window.addEventListener("popstate", () => {
      if (chatShell.classList.contains("mf-chat-nav-open") && !history.state?.mfChats) setOpen(false);
    });
    chatShell.addEventListener("keydown", (event) => {
      if (event.key !== "Escape" || !mqPhone.matches || !chatShell.classList.contains("mf-chat-nav-open")) return;
      event.stopPropagation();
      closeList();
      (chatShell.querySelector(".mf-chat-back") || toggle).focus({ preventScroll: true });
    });
    window.addEventListener("hashchange", () => {
      const section = /^#\/chat\/a\/[^/]+$/.test(location.hash) || location.hash === "#/chat/spirits";
      if (mqPhone.matches && !section) setOpen(false);
    });
    chatRail.addEventListener("click", (event) => {
      if (mqPhone.matches && event.target.closest(".chat-rail-row, .chat-rail-task, .chat-rail-new")) setOpen(false);
    });
    // Keyboard resize and pan are coalesced into one idempotent layout pass.
    // The app FOLLOWS the visual viewport while a keyboard is up (2026-09-21):
    // iOS never shrinks the layout viewport for its keyboard, it pans the
    // visible window over the page just far enough to show the focused
    // field — so a composer sitting at the bottom of a 100dvh shell fell
    // under the keyboard on focus and only came back once the shell was
    // refitted. Pinning the shell to the visual viewport's own top and height
    // keeps the composer at the bottom of what is visible through the whole
    // keyboard animation, and nothing here scrolls the page.
    let fitFrame = 0;
    const follow = () => {
      const vv = window.visualViewport;
      const root = document.documentElement.style;
      const keyboard = mqPhone.matches && vv && vv.scale === 1 && window.innerHeight - vv.height > 120;
      if (keyboard) {
        root.setProperty("--mf-vv-top", Math.round(vv.offsetTop) + "px");
        root.setProperty("--mf-vv-height", Math.round(vv.height) + "px");
        shell.classList.add("mf-keyboard");
      } else if (shell.classList.contains("mf-keyboard")) {
        shell.classList.remove("mf-keyboard");
        root.removeProperty("--mf-vv-top");
        root.removeProperty("--mf-vv-height");
      }
    };
    const refit = () => {
      if (!mqPhone.matches || fitFrame) return;
      fitFrame = requestAnimationFrame(() => { fitFrame = 0; follow(); if (typeof chatFitShell === "function") chatFitShell(); });
    };
    window.visualViewport?.addEventListener("resize", refit);
    window.visualViewport?.addEventListener("scroll", refit);
    window.mf.follow = follow;
  }

  // ---- scrim (shared by the drawer; the sheet has its own) ----
  const scrim = document.createElement("div");
  scrim.className = "mf-scrim";
  shell.append(scrim);

  // ---- drawer ----
  // Phone nav = the SAME rail (groups/order/counts), slid in as an overlay.
  // Never writes manifest.rail.collapsed — phone use must not pollute the
  // desktop preference.
  function openDrawer() { if (mqPhone.matches) shell.classList.add("drawer-open"); }
  function closeDrawer() { shell.classList.remove("drawer-open"); }
  scrim.addEventListener("click", closeDrawer);
  // every rail item is an <a href="#/...">: hashchange covers all navigation.
  window.addEventListener("hashchange", closeDrawer);
  // same-hash re-tap fires no hashchange — belt: any tap inside the groups.
  document.getElementById("railGroups").addEventListener("click", () => {
    if (mqPhone.matches) closeDrawer();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && shell.classList.contains("drawer-open")) closeDrawer();
  });

  // ---- mobile top bar buttons (☰ · … · ⌕ · ＋), hidden ≥861 by CSS ----
  const btn = (cls, glyph, label, fn) => {
    const b = document.createElement("button");
    b.className = "mf-topbar-btn " + cls;
    b.textContent = glyph;
    b.setAttribute("aria-label", label);
    b.addEventListener("click", fn);
    return b;
  };
  crumbBar.prepend(btn("mf-menu", "☰", "Menu", () => {
    shell.classList.contains("drawer-open") ? closeDrawer() : openDrawer();
  }));
  crumbBar.append(
    btn("mf-search", "⌕", "Search", () => window.openCmdbar && openCmdbar()),
    btn("mf-add", "＋", "Capture", () => {if(location.hash.startsWith("#/chat")){location.hash=chatNewHash();}else if(window.openTodoQuickAdd)openTodoQuickAdd();})
  );

  const updateCaptureLabel=()=>{const add=crumbBar.querySelector(".mf-add");if(add)add.setAttribute("aria-label",location.hash.startsWith("#/chat")?"New chat":"Capture");};
  window.addEventListener("hashchange",updateCaptureLabel);updateCaptureLabel();

  // ---- bottom-sheet primitive ----
  // One lazily-built host. Same-key re-open re-fills IN PLACE (no re-animation)
  // — renderAion() re-runs on every field save while its sheet is open, and a
  // sheet that re-animates per keystroke reads as a bug. close() nulls state
  // BEFORE invoking onClose so a close-handler that itself calls closeIf()
  // cannot recurse.
  let sheetEls = null; // {wrap, scrim, card, body}
  let sheetKey = null;
  let sheetOnClose = null;
  let sheetReopen = null; // desktop-restore callback for breakpoint crossing

  function ensureSheet() {
    if (sheetEls) return sheetEls;
    const wrap = document.createElement("div");
    wrap.className = "mf-sheet-wrap";
    wrap.hidden = true;
    const sScrim = document.createElement("div");
    sScrim.className = "mf-sheet-scrim";
    const card = document.createElement("div");
    card.className = "mf-sheet";
    const handle = document.createElement("div");
    handle.className = "mf-sheet-handle";
    const body = document.createElement("div");
    body.className = "mf-sheet-body";
    card.append(handle, body);
    wrap.append(sScrim, card);
    document.body.append(wrap);
    sScrim.addEventListener("click", () => mfSheet.close());
    handle.addEventListener("click", () => mfSheet.close());
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && !wrap.hidden) mfSheet.close();
    });
    sheetEls = { wrap, scrim: sScrim, card, body };
    return sheetEls;
  }

  const mfSheet = {
    // open(build, {key, onClose, reopen}) — build(bodyEl) fills a cleared body.
    open(build, opts = {}) {
      const ui = ensureSheet();
      const sameKey = !ui.wrap.hidden && opts.key && opts.key === sheetKey;
      sheetKey = opts.key || "_";
      sheetOnClose = opts.onClose || null;
      sheetReopen = opts.reopen || null;
      ui.body.innerHTML = "";
      build(ui.body);
      ui.card.classList.toggle("no-anim", !!sameKey);
      ui.wrap.hidden = false;
      return ui.body;
    },
    // body(key, onClose) — open empty and hand the body to an external builder
    // (openPropInspector writes into whatever host it's given).
    body(key, onClose, reopen) {
      return mfSheet.open(() => {}, { key, onClose, reopen });
    },
    close(opts = {}) {
      if (!sheetEls || sheetEls.wrap.hidden) return;
      sheetEls.wrap.hidden = true;
      sheetEls.card.classList.remove("no-anim");
      const cb = sheetOnClose;
      sheetKey = null;
      sheetOnClose = null;
      if (!opts.keepReopen) sheetReopen = null;
      if (!opts.silent && cb) cb();
    },
    closeIf(key) { if (sheetKey === key) mfSheet.close(); },
    openKey() { return sheetEls && !sheetEls.wrap.hidden ? sheetKey : null; },
  };
  window.mfSheet = mfSheet;

  // ---- breakpoint crossing ----
  // Close phone chrome silently in both directions; on phone→desktop restore
  // the desktop pane the open sheet was standing in for (e.g. the AION sticky
  // inspector repopulates via its surface re-render).
  mqPhone.addEventListener("change", (e) => {
    closeDrawer();
    if (sheetKey === "writing-comments") return; // Writing uses the wider 1100px sheet breakpoint.
    const restore = sheetReopen;
    mfSheet.close({ silent: true, keepReopen: false });
    if (typeof recPaint === "function" && typeof aionMode !== "undefined" &&
        aionMode === "recruiting" && !els.aionView.hidden) {
      recPaint(); // adapt recruiting navigation and selected candidate in both directions
    } else if (!e.matches && restore) restore();
  });
})();
