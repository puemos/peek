// gen-assets.mjs - drives Chromium with Playwright to record Peek's demo MP4.
//
// Invoked by scripts/gen-assets.sh. Env:
//   BASE      - Peek base URL
//   SLUG      - slug of the seeded report upload
//   VIDEO_RAW - raw Playwright video output path
//   CHROME    - Chrome / Chromium executable path
import { mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright-core";

const { BASE, SLUG, VIDEO_RAW, CHROME } = process.env;
const VIDEO_W = 1920;
const VIDEO_H = 1080;
const REPORT_ZOOM = "1.18";
const DEMO_HOST = "https://peek.acme.com";
const SEEDED_COMMENT_COUNT = 2;

for (const [key, value] of Object.entries({ BASE, SLUG, VIDEO_RAW, CHROME })) {
  if (!value) throw new Error(`missing required env var: ${key}`);
}

const shareURL = `${BASE}/p/${SLUG}`;
const visibleShareURL = `${DEMO_HOST}/p/${SLUG}`;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function launchBrowser() {
  return chromium.launch({
    headless: true,
    executablePath: CHROME,
    args: ["--hide-scrollbars", "--disable-gpu", "--force-color-profile=srgb"],
  });
}

async function hold(ms) {
  await sleep(ms);
}

async function typeIntoElement(page, selector, text, delay = 18) {
  for (const ch of text) {
    await page.locator(selector).evaluate((el, value) => {
      el.textContent += value;
    }, ch);
    await hold(delay);
  }
}

async function showTerminalIntro(page) {
  await page.setContent(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Peek demo</title>
<style>
  :root { color-scheme: light; --line: #d8dce5; }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    min-height: 100vh;
    display: grid;
    place-items: center;
    background: #f7f8fb;
    font: 18px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Inter, Roboto, sans-serif;
  }
  .terminal {
    width: min(1180px, calc(100vw - 96px));
    overflow: hidden;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #101217;
    box-shadow: 0 24px 70px rgba(17, 20, 26, .18);
  }
  .chrome {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 46px;
    padding: 0 18px;
    border-bottom: 1px solid rgba(255,255,255,.08);
  }
  .dot { width: 12px; height: 12px; border-radius: 50%; background: #ff5f57; }
  .dot:nth-child(2) { background: #ffbd2e; }
  .dot:nth-child(3) { background: #28c840; }
  .body {
    min-height: 300px;
    padding: 38px 42px 42px;
    font: 28px/1.7 ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  }
  .prompt { color: #8b93a7; }
  .cmd { color: #f7f8fb; }
  .loader { color: #aab2c5; min-height: 48px; }
  .output { color: #9be7c4; min-height: 48px; }
  .cursor {
    display: inline-block;
    width: 12px;
    height: 34px;
    margin-left: 3px;
    vertical-align: -7px;
    background: #f7f8fb;
    animation: blink .8s steps(1) infinite;
  }
  @keyframes blink { 50% { opacity: 0; } }
</style>
</head>
<body>
  <section class="terminal" aria-label="terminal">
    <div class="chrome"><span class="dot"></span><span class="dot"></span><span class="dot"></span></div>
    <div class="body">
      <div><span class="prompt">$ </span><span id="cmd" class="cmd"></span><span class="cursor"></span></div>
      <div id="loader" class="loader"></div>
      <div id="output" class="output"></div>
    </div>
  </section>
</body>
</html>`);

  await hold(350);
  await typeIntoElement(page, "#cmd", "peek upload codebase-health-report.html");
  await hold(220);

  const frames = ["|", "/", "-", "\\"];
  for (let i = 0; i < 4; i++) {
    await page.locator("#loader").evaluate((el, value) => {
      el.textContent = value;
    }, `${frames[i % frames.length]} uploading codebase-health-report.html`);
    await hold(140);
  }

  await page.locator("#loader").evaluate((el) => {
    el.textContent = "uploaded";
  });
  await hold(140);
  await page.locator("#output").evaluate((el, url) => {
    el.textContent = url;
  }, visibleShareURL);
  await hold(850);
}

async function openSharedPage(page) {
  await page.context().addCookies([{ name: "hn_name", value: "Sam", url: BASE, sameSite: "Lax" }]);
  await page.context().addInitScript(() => {
    try { localStorage.setItem("hn_name_asked", "1"); } catch (e) {}
  });

  await page.goto(shareURL, { waitUntil: "domcontentloaded" });
  await page.locator("#hn-frame").waitFor({ state: "attached" });
  await page.locator("#hn-comment-btn").waitFor({ state: "visible" });
  await page.evaluate(() => {
    const modal = document.getElementById("hn-name-modal");
    if (modal && getComputedStyle(modal).display !== "none") document.getElementById("hn-name-skip")?.click();
  });
  await page.frameLocator("#hn-frame").locator("html").evaluate((html, zoom) => {
    html.style.zoom = zoom;
  }, REPORT_ZOOM);
  await page.locator("#hn-count").waitFor({ state: "visible" });
  await page.waitForFunction((expected) => {
    const el = document.getElementById("hn-count");
    return el && Number(el.textContent || "0") === expected;
  }, SEEDED_COMMENT_COUNT);
  await page.frameLocator("#hn-frame").locator(".hn-pin").nth(SEEDED_COMMENT_COUNT - 1).waitFor({ state: "visible" });
}

async function selectTextTarget(page, selector) {
  const report = page.frameLocator("#hn-frame");
  await report.locator(selector).evaluate((el) => {
    el.scrollIntoView({ block: "center", inline: "nearest" });
  });
  await hold(180);
  await report.locator(selector).evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  });
  await report.locator(".hn-sel-btn").waitFor({ state: "visible" });
  return report;
}

async function typeComment(page, chunks) {
  await page.locator("#hn-body").click();
  for (const [text, pause] of chunks) {
    await page.keyboard.insertText(text);
    await hold(pause);
  }
}

async function submitComment(page, report, expectedCount) {
  await page.locator('#hn-comment-form button[type="submit"]').click();
  await page.locator("#hn-composer").waitFor({ state: "hidden" });
  await page.locator("#hn-panel.hn-panel-open").waitFor({ state: "hidden" });
  await page.waitForFunction((count) => {
    const el = document.getElementById("hn-count");
    return el && Number(el.textContent || "0") === count;
  }, expectedCount);
  await report.locator(".hn-pin").nth(expectedCount - 1).waitFor({ state: "visible" });
  await hold(760);
}

async function addSelectedTextComment(page, selector, chunks, expectedCount) {
  const report = await selectTextTarget(page, selector);
  await hold(650);
  await report.locator(".hn-sel-btn").evaluate((btn) => {
    btn.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, view: window }));
  });
  await page.locator("#hn-body").waitFor({ state: "visible" });
  await hold(420);
  await typeComment(page, chunks);
  await submitComment(page, report, expectedCount);
}

async function addElementComment(page, selector, chunks, expectedCount) {
  const report = page.frameLocator("#hn-frame");
  await page.locator("#hn-comment-btn").click();
  await page.locator("#hn-hint").waitFor({ state: "visible" });
  await hold(520);
  await report.locator(selector).click();
  await page.locator("#hn-body").waitFor({ state: "visible" });
  await hold(420);
  await typeComment(page, chunks);
  await submitComment(page, report, expectedCount);
}

async function showVisitSparkChart(page) {
  await page.keyboard.press("Escape");
  await page.locator("#hn-panel.hn-panel-open").waitFor({ state: "hidden" });
  await page.waitForFunction(() => {
    const count = document.getElementById("hn-views-count");
    return count && Number((count.textContent || "").replace(/\D/g, "")) > 0;
  });
  await page.locator("#hn-views-btn").click();
  await page.locator("#hn-sparkline").waitFor({ state: "visible" });
  await page.locator("#hn-sparkline-svg path").first().waitFor({ state: "attached" });
  await hold(2200);
}

async function recordDemo(browser) {
  console.log("screencast...");
  const videoDir = path.dirname(VIDEO_RAW);
  await mkdir(videoDir, { recursive: true });
  await rm(VIDEO_RAW, { force: true });

  const context = await browser.newContext({
    viewport: { width: VIDEO_W, height: VIDEO_H },
    deviceScaleFactor: 1,
    recordVideo: {
      dir: videoDir,
      size: { width: VIDEO_W, height: VIDEO_H },
    },
  });
  const page = await context.newPage();
  const video = page.video();

  try {
    await showTerminalIntro(page);

    await openSharedPage(page);
    await hold(900);

    await addSelectedTextComment(page, "#latency-claim", [
      ["Can we attach the benchmark run ", 240],
      ["that shows this 38% jump?", 640],
    ], SEEDED_COMMENT_COUNT + 1);

    await addSelectedTextComment(page, "#f2", [
      ["Please link this to the incident review ", 240],
      ["before sharing broadly.", 640],
    ], SEEDED_COMMENT_COUNT + 2);

    await addElementComment(page, "#issues", [
      ["Can we add owner and due date here?", 720],
    ], SEEDED_COMMENT_COUNT + 3);

    await hold(900);
    await showVisitSparkChart(page);
  } finally {
    await context.close();
    if (video) await video.saveAs(VIDEO_RAW);
  }
}

const browser = await launchBrowser();
try {
  await recordDemo(browser);
} finally {
  await browser.close();
}
