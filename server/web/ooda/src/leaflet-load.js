// Leaflet, lazily. Ported from the cockpit's 82-properties-map.js loadLeaflet()
// so both maps run the same version off the same CDN.
//
// Lazy rather than a <script> tag in index.html: three of the five tabs never
// draw a map, and Leaflet + its CSS is ~150 KB they should not pay for. The
// promise is memoized, so a tab switch back is instant.

const OODA_LEAFLET_VER = "1.9.4";
let oodaLeafletPromise = null;

function loadLeaflet() {
  if (window.L) return Promise.resolve(window.L);
  if (oodaLeafletPromise) return oodaLeafletPromise;
  oodaLeafletPromise = new Promise((resolve, reject) => {
    const base = "https://cdnjs.cloudflare.com/ajax/libs/leaflet/" + OODA_LEAFLET_VER + "/";
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = base + "leaflet.min.css";
    document.head.appendChild(css);
    const js = document.createElement("script");
    js.src = base + "leaflet.min.js";
    js.onload = () => resolve(window.L);
    // offline or a blocked CDN is a degraded tab, not a broken portal — the
    // caller shows the list instead of a blank rectangle
    js.onerror = () => { oodaLeafletPromise = null; reject(new Error("leaflet failed to load")); };
    document.head.appendChild(js);
  });
  return oodaLeafletPromise;
}

// The parcel study palette — IDENTICAL to ooda.group/parcels and to the
// cockpit's 91-parcels.js. A partner who has seen one map must recognize this
// one, so these three colors are a shared constant, not a local choice.
const OODA_TAX_COLOR = { delinquent: "#d9534f", lra: "#e0972f", current: "#8fb0a0" };
const OODA_TAX_LABEL = { delinquent: "Tax delinquent", lra: "LRA land-bank", current: "Privately owned" };
const OODA_TAX_ORDER = ["delinquent", "lra", "current"];

// Holdings are colored by ACQUISITION STATE, not status: a partner looking at
// the map needs to see what we own apart from what we have merely signed for.
const OODA_ACQ_COLOR = {
  "owned": "#265ACC",
  "under-contract": "#8a93a6",
  "pipeline": "#b0a58e",
};
const OODA_ACQ_LABEL = {
  "owned": "Owned",
  "under-contract": "Under contract",
  "pipeline": "In negotiation",
};
const OODA_ACQ_ORDER = ["owned", "under-contract", "pipeline"];
