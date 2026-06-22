// gen-screenshots.mjs - drives Chromium to capture Peek screenshots.
//
// Invoked by scripts/gen-screenshots.sh. Env:
//   BASE        - Peek base URL
//   SLUG        - slug of the seeded report upload
//   OUT         - output directory
//   COOKIE_FILE - curl cookie jar containing the dashboard session
//   CHROME      - Chrome / Chromium executable path
import { mkdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright-core";

const { BASE, SLUG, OUT, COOKIE_FILE, CHROME } = process.env;
const WIDTH = 1270;
const HEIGHT = 760;
const DEMO_HOST = "https://peek.acme.com";
const SEEDED_COMMENT_COUNT = 3;

for (const [key, value] of Object.entries({ BASE, SLUG, OUT, COOKIE_FILE, CHROME })) {
  if (!value) throw new Error(`missing required env var: ${key}`);
}

const shareURL = `${BASE}/p/${SLUG}`;
const visibleShareURL = `${DEMO_HOST}/p/${SLUG}`;
const written = [];

async function launchBrowser() {
  return chromium.launch({
    headless: true,
    executablePath: CHROME,
    args: ["--hide-scrollbars", "--disable-gpu", "--force-color-profile=srgb"],
  });
}

async function addCookieJar(context) {
  const text = await readFile(COOKIE_FILE, "utf8");
  const cookies = [];

  for (const rawLine of text.split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line) continue;

    let httpOnly = false;
    if (line.startsWith("#HttpOnly_")) {
      httpOnly = true;
      line = line.slice("#HttpOnly_".length);
    } else if (line.startsWith("#")) {
      continue;
    }

    const fields = line.split(/\t+/);
    if (fields.length < 7) continue;

    const [, , , secure, expires, name, value] = fields;
    const cookie = {
      name,
      value,
      url: BASE,
      secure: secure === "TRUE",
      httpOnly,
      sameSite: "Lax",
    };
    const expiresAt = Number(expires);
    if (Number.isFinite(expiresAt) && expiresAt > 0) {
      cookie.expires = expiresAt;
    }
    cookies.push(cookie);
  }

  if (cookies.length) {
    await context.addCookies(cookies);
  }
}

async function newContext(browser) {
  const context = await browser.newContext({
    viewport: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 1,
    colorScheme: "light",
  });
  await addCookieJar(context);
  await context.addCookies([{ name: "hn_name", value: "Sam", url: BASE, sameSite: "Lax" }]);
  await context.addInitScript(() => {
    try {
      localStorage.setItem("hn_name", "Sam");
      localStorage.setItem("hn_name_asked", "1");
    } catch (e) {}
  });
  return context;
}

async function newPlainContext(browser) {
  return browser.newContext({
    viewport: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 1,
    colorScheme: "light",
  });
}

async function waitForFonts(page) {
  await page.evaluate(() => (document.fonts ? document.fonts.ready : Promise.resolve())).catch(() => {});
}

async function capture(page, filename) {
  const outPath = path.join(OUT, filename);
  await rm(outPath, { force: true });
  await waitForFonts(page);
  await page.waitForTimeout(250);
  await page.screenshot({
    path: outPath,
    type: "png",
    fullPage: false,
    animations: "disabled",
  });
  written.push(filename);
}

async function showTerminalUpload(page) {
  await page.setContent(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Peek upload</title>
<style>
  :root { color-scheme: light; --line: #d8dce5; --ink: #11141a; --muted: #687080; --accent: #1677e8; }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    min-height: 100vh;
    display: grid;
    place-items: center;
    background: #f7f8fb;
    color: var(--ink);
    font: 18px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Inter, Roboto, sans-serif;
  }
  .stage { width: min(1080px, calc(100vw - 96px)); }
  .eyebrow { margin: 0 0 16px; color: var(--muted); font-size: 16px; font-weight: 600; }
  h1 { margin: 0 0 28px; max-width: 720px; font-size: 46px; line-height: 1.04; letter-spacing: 0; }
  .terminal {
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
    height: 44px;
    padding: 0 18px;
    border-bottom: 1px solid rgba(255,255,255,.08);
  }
  .dot { width: 12px; height: 12px; border-radius: 50%; background: #ff5f57; }
  .dot:nth-child(2) { background: #ffbd2e; }
  .dot:nth-child(3) { background: #28c840; }
  .body {
    min-height: 260px;
    padding: 34px 40px 40px;
    font: 24px/1.75 ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  }
  .prompt { color: #8b93a7; }
  .cmd { color: #f7f8fb; }
  .ok { color: #9be7c4; }
  .url { color: #8bc7ff; overflow-wrap: anywhere; }
  .dim { color: #aab2c5; }
</style>
</head>
<body>
  <main class="stage">
    <p class="eyebrow">CLI upload</p>
    <h1>Share an HTML review in one command.</h1>
    <section class="terminal" aria-label="terminal">
      <div class="chrome"><span class="dot"></span><span class="dot"></span><span class="dot"></span></div>
      <div class="body">
        <div><span class="prompt">$ </span><span class="cmd">peek upload codebase-health-report.html</span></div>
        <div class="dim">uploading codebase-health-report.html</div>
        <div class="ok">uploaded</div>
        <div><span class="dim">url: </span><span class="url">${visibleShareURL}</span></div>
        <div><span class="dim">slug: </span><span class="ok">${SLUG}</span></div>
      </div>
    </section>
  </main>
</body>
</html>`);
}

async function openSharedPage(page) {
  await page.goto(shareURL, { waitUntil: "domcontentloaded" });
  await page.locator("#hn-frame").waitFor({ state: "attached" });
  await page.locator("#hn-comment-btn").waitFor({ state: "visible" });
  await page.evaluate(() => {
    const modal = document.getElementById("hn-name-modal");
    if (modal && getComputedStyle(modal).display !== "none") document.getElementById("hn-name-skip")?.click();
  });

  const report = page.frameLocator("#hn-frame");
  await report.locator("#summary").waitFor({ state: "visible" });
  await report.locator("html").evaluate((html) => {
    html.style.zoom = "1";
  });
  await page.waitForFunction((expected) => {
    const el = document.getElementById("hn-count");
    return el && Number(el.textContent || "0") === expected;
  }, SEEDED_COMMENT_COUNT);
  await report.locator(".hn-pin").nth(SEEDED_COMMENT_COUNT - 1).waitFor({ state: "attached" });
  await report.locator("#summary").evaluate((el) => {
    el.scrollIntoView({ block: "center", inline: "nearest" });
  });
  await page.waitForTimeout(400);
}

async function showCommentsPanel(page) {
  await page.locator("#hn-panel-btn").click();
  await page.locator("#hn-panel.hn-panel-open").waitFor({ state: "visible" });
  await page.locator("#hn-comment-list li").first().waitFor({ state: "visible" });
}

async function showTextSelection(page) {
  const report = page.frameLocator("#hn-frame");
  await page.keyboard.press("Escape");
  await report.locator("#latency-claim").evaluate((el) => {
    el.scrollIntoView({ block: "center", inline: "nearest" });
    const selection = window.getSelection();
    if (!selection) return;
    const range = document.createRange();
    range.selectNodeContents(el);
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  });
  await report.locator(".hn-sel-btn").waitFor({ state: "visible" });
}

async function showElementComposer(page) {
  const report = page.frameLocator("#hn-frame");
  await page.keyboard.press("Escape");
  await report.locator("body").evaluate(() => {
    const selection = window.getSelection();
    if (selection) selection.removeAllRanges();
  });
  await report.locator("#issues").evaluate((el) => {
    el.scrollIntoView({ block: "center", inline: "nearest" });
  });
  await page.locator("#hn-comment-btn").click();
  await page.locator("#hn-hint").waitFor({ state: "visible" });
  await report.locator("#issues").click();
  await page.locator("#hn-body").waitFor({ state: "visible" });
  await page.locator("#hn-body").fill("Can we assign an owner before this report goes out?");
}

async function showDashboard(page) {
  await page.goto(`${BASE}/dashboard`, { waitUntil: "domcontentloaded" });
  await page.locator("h2", { hasText: "Upload HTML" }).waitFor({ state: "visible" });
  await page.locator("h2", { hasText: "Your uploads" }).waitFor({ state: "visible" });
}

async function showStats(page) {
  await page.goto(`${BASE}/dashboard/stats/${SLUG}`, { waitUntil: "domcontentloaded" });
  await page.locator("h3", { hasText: "Recent visits" }).waitFor({ state: "visible" });
}

async function scrollToSettings(page) {
  await page.goto(`${BASE}/dashboard`, { waitUntil: "domcontentloaded" });
  await page.locator("h2", { hasText: "Settings" }).waitFor({ state: "attached" });
  await page.locator("h2", { hasText: "Settings" }).evaluate((el) => {
    const top = el.getBoundingClientRect().top + window.scrollY - 78;
    window.scrollTo({ top, left: 0, behavior: "instant" });
  });
  await page.waitForTimeout(150);
}

async function showLoginOAuth(page) {
  await page.goto(`${BASE}/login`, { waitUntil: "domcontentloaded" });
  await page.locator("a", { hasText: "Continue with Google" }).waitFor({ state: "visible" });
  await page.locator("a", { hasText: "Continue with GitHub" }).waitFor({ state: "visible" });
}

async function showAdminAuth(page) {
  await scrollToSettings(page);
  await page.locator("button[role='tab']", { hasText: "Auth" }).click();
  await page.locator("input[name='oauth_google_client_id']").waitFor({ state: "visible" });
  await page.waitForTimeout(250);
}

async function showAdminStorageS3(page) {
  await scrollToSettings(page);
  await page.locator("button[role='tab']", { hasText: "Storage" }).click();
  await page.locator(".peek-segmented-option", { hasText: "S3" }).click();
  await page.locator("input[name='s3_endpoint']").waitFor({ state: "visible" });
  await page.locator("h2", { hasText: "Settings" }).evaluate((el) => {
    const top = el.getBoundingClientRect().top + window.scrollY - 78;
    window.scrollTo({ top, left: 0, behavior: "instant" });
  });
  await page.waitForTimeout(250);
}

async function showAdminLimits(page) {
  await scrollToSettings(page);
  await page.locator("button[role='tab']", { hasText: "Limits" }).click();
  await page.locator("input[name='max_upload_mb']").waitFor({ state: "visible" });
  await page.waitForTimeout(250);
}

async function showAdminUsersInvites(page) {
  await page.goto(`${BASE}/dashboard`, { waitUntil: "domcontentloaded" });
  await page.locator("h2", { hasText: "Invitations" }).waitFor({ state: "visible" });
  await page.locator("h2", { hasText: "Users" }).waitFor({ state: "visible" });
  await page.locator("h2", { hasText: "Invitations" }).scrollIntoViewIfNeeded();
  await page.waitForTimeout(250);
}

function pngSize(buffer) {
  const signature = buffer.subarray(0, 8).toString("hex");
  if (signature !== "89504e470d0a1a0a") {
    throw new Error("not a PNG");
  }
  return {
    width: buffer.readUInt32BE(16),
    height: buffer.readUInt32BE(20),
  };
}

async function validateOutputs() {
  for (const filename of written) {
    const buffer = await readFile(path.join(OUT, filename));
    const size = pngSize(buffer);
    if (size.width !== WIDTH || size.height !== HEIGHT) {
      throw new Error(`${filename} is ${size.width}x${size.height}, expected ${WIDTH}x${HEIGHT}`);
    }
    console.log(`wrote ${filename} (${size.width}x${size.height})`);
  }
}

await mkdir(OUT, { recursive: true });

const browser = await launchBrowser();
try {
  const context = await newContext(browser);
  const page = await context.newPage();

  await showTerminalUpload(page);
  await capture(page, "01-cli-upload.png");

  await openSharedPage(page);
  await showCommentsPanel(page);
  await capture(page, "02-viewer-comments.png");

  await showTextSelection(page);
  await capture(page, "03-text-anchor.png");

  await showElementComposer(page);
  await capture(page, "04-element-pin.png");

  await showDashboard(page);
  await capture(page, "05-dashboard-uploads.png");

  await showStats(page);
  await capture(page, "06-upload-stats.png");

  const plainContext = await newPlainContext(browser);
  const plainPage = await plainContext.newPage();
  await showLoginOAuth(plainPage);
  await capture(plainPage, "07-login-oauth.png");
  await plainContext.close();

  await showAdminAuth(page);
  await capture(page, "08-admin-auth.png");

  await showAdminStorageS3(page);
  await capture(page, "09-admin-storage-s3.png");

  await showAdminLimits(page);
  await capture(page, "10-admin-limits-retention.png");

  await showAdminUsersInvites(page);
  await capture(page, "11-admin-users-invites.png");

  await context.close();
  await validateOutputs();
} finally {
  await browser.close();
}
