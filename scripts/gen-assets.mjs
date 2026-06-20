// gen-assets.mjs — drives headless Chrome (via the DevTools Protocol) to capture
// Peek's launch assets: ProductHunt-ratio (1270x760) screenshots at 2x plus a
// screencast of the commenting flow as numbered frames for ffmpeg.
//
// Invoked by scripts/gen-assets.sh. Env:
//   DEBUG  - Chrome remote-debugging base (http://localhost:PORT)
//   BASE   - Peek base URL
//   SLUG   - slug of the seeded report upload
//   TOKEN  - API token minted by the setup script
//   OUT    - assets output dir
//   FRAMES - dir for video frames
import fs from "node:fs";

const { DEBUG, BASE, SLUG, TOKEN, OUT, FRAMES } = process.env;
const W = 1270, H = 760; // ProductHunt gallery ratio (~1.67:1)

const t = await (await fetch(`${DEBUG}/json/new?${encodeURIComponent(BASE + "/p/" + SLUG)}`, { method: "PUT" })).json();
const ws = new WebSocket(t.webSocketDebuggerUrl);
let id = 0; const pending = new Map();
const send = (m, p = {}) => new Promise((r, j) => { const i = ++id; pending.set(i, { r, j }); ws.send(JSON.stringify({ id: i, method: m, params: p })); });
await new Promise((r) => (ws.onopen = r));
ws.onmessage = (e) => { const d = JSON.parse(e.data); if (d.id && pending.has(d.id)) { const { r, j } = pending.get(d.id); pending.delete(d.id); d.error ? j(new Error(d.error.message)) : r(d.result); } };

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const ev = (x) => send("Runtime.evaluate", { expression: x, awaitPromise: true, returnByValue: true });
const mouse = (type, x, y, ex = {}) => send("Input.dispatchMouseEvent", Object.assign({ type, x, y, button: "left", buttons: 1 }, ex));
const png = async (p) => { const r = await send("Page.captureScreenshot", { format: "png" }); fs.writeFileSync(p, Buffer.from(r.data, "base64")); console.log("  shot", p.split("/").pop()); };
const metrics = (scale) => send("Emulation.setDeviceMetricsOverride", { width: W, height: H, deviceScaleFactor: scale, mobile: false });
const nav = async (url) => { await send("Page.navigate", { url }); await sleep(2700); };

await send("Page.enable");
await send("Runtime.enable");

// ---------------- screenshots (2x, crisp, PH ratio) ----------------
console.log("screenshots…");
await metrics(2);
await nav(`${BASE}/p/${SLUG}`);
await ev(`(document.getElementById('hn-name-skip')||{click(){}}).click()`);
await sleep(500);
await png(`${OUT}/hero.png`);                       // report + pins + highlight + island

await ev(`document.getElementById('hn-panel-btn').click()`);
await sleep(650);
await png(`${OUT}/comments.png`);                   // comments panel

await nav(`${BASE}/login`);
await ev(`(function(){var f=document.querySelector('input[name=token]');f.value=${JSON.stringify(TOKEN)};f.form.submit();})()`);
await sleep(1600);
await png(`${OUT}/dashboard.png`);                  // dashboard

// ---------------- screencast (1x, PH ratio) ----------------
console.log("screencast…");
let fi = 0;
const frame = async () => { try { const r = await send("Page.captureScreenshot", { format: "png" }); fs.writeFileSync(`${FRAMES}/f_${String(fi++).padStart(4, "0")}.png`, Buffer.from(r.data, "base64")); } catch {} };
const hold = async (ms) => { const end = Date.now() + ms; while (Date.now() < end) { await frame(); await sleep(80); } };

await metrics(1);
await nav(`${BASE}/p/${SLUG}`);

// 1) one-time name prompt (onboarding)
await hold(1200);
await ev(`(function(){var i=document.getElementById('hn-name-input'); if(i) i.value='Sam';})()`);
await hold(450);
await ev(`document.getElementById('hn-name-form').requestSubmit()`);
await hold(900);

// 2) open the comments panel (existing review comments)
await ev(`document.getElementById('hn-panel-btn').click()`);
await hold(1500);

// 3) click a comment -> scroll to + flash its anchor on the page
await ev(`(function(){var li=document.querySelector('#hn-comment-list li.hn-locatable'); if(li) li.click();})()`);
await hold(1800);

// 4) flash comment mode + hint, then toggle back off
await ev(`document.getElementById('hn-panel-btn').click()`);
await hold(400);
await ev(`document.getElementById('hn-comment-btn').click()`);   // mode ON -> hint
await hold(1300);
await ev(`document.getElementById('hn-comment-btn').click()`);   // mode OFF
await hold(500);

// 5) select a word in a paragraph (double-click) -> Comment bubble
const sx = 430, sy = 360;
await mouse("mousePressed", sx, sy, { clickCount: 1 }); await mouse("mouseReleased", sx, sy, { clickCount: 1 });
await mouse("mousePressed", sx, sy, { clickCount: 2 }); await mouse("mouseReleased", sx, sy, { clickCount: 2 });
await hold(1400);

// 6) click the Comment bubble -> composer anchored to the text
await mouse("mousePressed", sx, sy - 42, { clickCount: 1 }); await mouse("mouseReleased", sx, sy - 42, { clickCount: 1 });
await hold(1100);

// 7) type a comment
await ev(`(function(){var b=document.getElementById('hn-body'); if(b) b.focus();})()`);
for (const c of ["Can we ", "link the trace ", "for this number?"]) { await send("Input.insertText", { text: c }); await hold(420); }

// 8) post -> panel reopens with the new comment
await ev(`document.getElementById('hn-comment-form').requestSubmit()`);
await hold(2000);

console.log("  frames:", fi);
ws.close();
process.exit(0);
