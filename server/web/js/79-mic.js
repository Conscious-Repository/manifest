// ---- mic dictation (cmd-ctr import P6) ----
// micButton(onText) is the drop-in composer control: press to record, press
// again (or auto-stop on ~2s silence) to transcribe. Audio is captured via
// MediaRecorder, decoded + resampled to 16k mono PCM WAV in the browser
// (guarantees a format the lab's granite-speech accepts regardless of codec),
// and POSTed to /api/stt — the manifest server relays to the lab; no cloud,
// no keys in the browser. Per-utterance batch (live partials need a
// streaming service; the seam allows one later).

let _micActive = null; // only one recording at a time

function micButton(onText) {
  const btn = el("button", "mic-btn", "🎙");
  btn.title = "Dictate (press to talk, press again to stop)";
  btn.onclick = () => micToggle(btn, onText);
  return btn;
}

async function micToggle(btn, onText) {
  if (_micActive) { _micActive.stop(); return; }
  let stream;
  try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }); }
  catch (e) { showToast("Microphone unavailable — " + (e.message || "denied")); return; }

  const rec = new MediaRecorder(stream);
  const chunks = [];
  rec.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
  btn.classList.add("recording");
  btn.textContent = "●";

  // silence auto-stop: watch the input level; ~2s under threshold ends the take
  const ac = new (window.AudioContext || window.webkitAudioContext)();
  const src = ac.createMediaStreamSource(stream);
  const analyser = ac.createAnalyser();
  analyser.fftSize = 512;
  src.connect(analyser);
  const buf = new Uint8Array(analyser.fftSize);
  let lastLoud = Date.now(), spoke = false;
  const watch = setInterval(() => {
    analyser.getByteTimeDomainData(buf);
    let peak = 0;
    for (const v of buf) peak = Math.max(peak, Math.abs(v - 128));
    if (peak > 12) { lastLoud = Date.now(); spoke = true; }
    if (spoke && Date.now() - lastLoud > 2000) stopFn();
    if (!spoke && Date.now() - lastLoud > 10000) stopFn(); // nothing said
  }, 150);

  const stopFn = () => { if (rec.state !== "inactive") rec.stop(); };
  _micActive = { stop: stopFn };

  rec.onstop = async () => {
    clearInterval(watch);
    stream.getTracks().forEach((t) => t.stop());
    _micActive = null;
    btn.classList.remove("recording");
    btn.textContent = "…";
    try {
      const blob = new Blob(chunks, { type: rec.mimeType || "audio/webm" });
      if (!spoke || blob.size < 2000) { btn.textContent = "🎙"; return; }
      const wav = await blobToWav16k(blob, ac);
      const res = await fetch("/api/stt", { method: "POST", body: wav });
      if (!res.ok) throw new Error((await res.text()).slice(0, 140));
      const d = await res.json();
      if (d.text) onText(d.text);
    } catch (e) {
      showToast("Dictation failed — " + (e.message || "error"));
    } finally {
      btn.textContent = "🎙";
      ac.close();
    }
  };
  rec.start();
}

// blobToWav16k decodes any recorded codec via WebAudio, downmixes to mono,
// resamples to 16 kHz, and emits a PCM16 WAV blob.
async function blobToWav16k(blob, ac) {
  const raw = await blob.arrayBuffer();
  const decoded = await ac.decodeAudioData(raw.slice(0));
  const target = 16000;
  const off = new OfflineAudioContext(1, Math.ceil(decoded.duration * target), target);
  const src = off.createBufferSource();
  src.buffer = decoded;
  src.connect(off.destination);
  src.start();
  const rendered = await off.startRendering();
  const pcm = rendered.getChannelData(0);
  const out = new DataView(new ArrayBuffer(44 + pcm.length * 2));
  const wstr = (o, s) => { for (let i = 0; i < s.length; i++) out.setUint8(o + i, s.charCodeAt(i)); };
  wstr(0, "RIFF"); out.setUint32(4, 36 + pcm.length * 2, true); wstr(8, "WAVE");
  wstr(12, "fmt "); out.setUint32(16, 16, true); out.setUint16(20, 1, true);
  out.setUint16(22, 1, true); out.setUint32(24, target, true);
  out.setUint32(28, target * 2, true); out.setUint16(32, 2, true); out.setUint16(34, 16, true);
  wstr(36, "data"); out.setUint32(40, pcm.length * 2, true);
  let o = 44;
  for (let i = 0; i < pcm.length; i++, o += 2) {
    const s = Math.max(-1, Math.min(1, pcm[i]));
    out.setInt16(o, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return new Blob([out.buffer], { type: "audio/wav" });
}

// ---- voice → destination routing (B3) ----
// Mics mount onto the live composers once per page load; each inserts the
// transcript into its input so the OWNER still routes/edits/sends — voice is
// entry, not automation.
function mountMics() {
  const spots = [
    // chat tab composer
    { sel: "#chatComposer", input: () => document.querySelector("#chatComposer textarea") },
    // ⌘K palette (mic sits inside the card, after the input)
    { sel: "#cmdbar .cmdbar-card", input: () => els.cmdbarInput },
  ];
  spots.forEach((sp) => {
    const host = document.querySelector(sp.sel);
    if (!host || host.querySelector(".mic-btn")) return;
    host.append(micButton((text) => {
      const input = sp.input();
      if (!input) return;
      input.value = (input.value ? input.value + " " : "") + text;
      input.focus();
      input.dispatchEvent(new Event("input", { bubbles: true }));
    }));
  });
}
setTimeout(mountMics, 400);
window.addEventListener("hashchange", () => setTimeout(mountMics, 400));
