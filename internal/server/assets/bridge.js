// bridge.js — runs inside the sandboxed iframe containing user HTML.
// Responsibilities:
//  - expose an element picker when the parent requests "comment mode"
//  - show a "Comment" bubble when the viewer selects text
//  - send the anchor (element selector + text quote) to the parent on pick
//  - draw numbered pins + highlight commented text, anchored to the page
//  - scroll-to + flash an anchor when the parent requests "locate"
// It does NOT call the server directly (the iframe is opaque-origin); all
// comments traffic goes through the trusted parent page via postMessage.
(function () {
  "use strict";

  var ACCENT = "#5e6ad2";
  var MODE_ON = false;
  var HIGHLIGHT = null;
  var PINS = [];           // [{selector, quote, n, el, node, range}]
  var pinLayer = null;
  var selBtn = null;
  var rafPending = false;
  var selTimer = null;
  var supportsHL = !!(window.Highlight && window.CSS && CSS.highlights);

  // --- injected styles (parent /style.css is not loaded in this iframe) ---
  function injectStyles() {
    var css =
      "#hn-pin-layer{position:absolute;top:0;left:0;width:0;height:0;pointer-events:none;z-index:2147483000}" +
      ".hn-pin{position:absolute;display:flex;align-items:center;justify-content:center;" +
        "width:24px;height:24px;padding:0;border-radius:50%;box-sizing:border-box;" +
        "background:" + ACCENT + ";color:#fff;font:600 12px/1 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;" +
        "letter-spacing:0;box-shadow:0 2px 8px rgba(0,0,0,.35),0 0 0 2px rgba(255,255,255,.9);" +
        "cursor:pointer;pointer-events:auto;transform:translate(-50%,-50%);transition:transform .12s ease;" +
        "-webkit-font-smoothing:antialiased;user-select:none}" +
      ".hn-pin:hover{transform:translate(-50%,-50%) scale(1.12)}" +
      ".hn-hover-outline{outline:2px solid " + ACCENT + " !important;outline-offset:1px !important;cursor:crosshair}" +
      ".hn-pulse{animation:hn-pulse 1.2s ease-out 1}" +
      "@keyframes hn-pulse{0%{box-shadow:0 0 0 0 rgba(94,106,210,.55)}100%{box-shadow:0 0 0 14px rgba(94,106,210,0)}}" +
      "::highlight(hn-highlight){background-color:rgba(94,106,210,.24);color:inherit}" +
      "::highlight(hn-highlight-active){background-color:rgba(94,106,210,.5);color:inherit}" +
      ".hn-sel-btn{position:fixed;z-index:2147483600;display:flex;align-items:center;gap:6px;" +
        "height:32px;padding:0 13px;border:0;border-radius:999px;background:" + ACCENT + ";color:#fff;" +
        "font:600 13px/1 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;" +
        "box-shadow:0 6px 20px rgba(0,0,0,.35),0 0 0 1px rgba(255,255,255,.12);cursor:pointer;" +
        "pointer-events:auto;white-space:nowrap;-webkit-font-smoothing:antialiased}" +
      ".hn-sel-btn:hover{background:#6872e5}";
    var style = document.createElement("style");
    style.id = "hn-style";
    style.textContent = css;
    (document.head || document.documentElement).appendChild(style);
  }

  function uniqueSelector(el) {
    if (!el || el.nodeType !== 1) return "";
    if (el === document.body) return "body";
    if (el.id) return "#" + CSS.escape(el.id);
    var parts = [];
    var node = el;
    while (node && node.nodeType === 1 && node.tagName !== "HTML" && node.tagName !== "BODY") {
      var part = node.tagName.toLowerCase();
      if (node.id) {
        parts.unshift("#" + CSS.escape(node.id));
        return parts.join(" > ");
      }
      var parent = node.parentElement;
      if (parent) {
        var sibs = Array.prototype.filter.call(parent.children, function (c) {
          return c.tagName === node.tagName;
        });
        if (sibs.length > 1) part += ":nth-of-type(" + (sibs.indexOf(node) + 1) + ")";
      }
      parts.unshift(part);
      node = node.parentElement;
    }
    return parts.length ? parts.join(" > ") : "body";
  }

  function snippet(el) {
    var txt = (el.innerText || "").trim().replace(/\s+/g, " ");
    return txt.length > 120 ? txt.slice(0, 120) + "…" : txt;
  }

  function resolve(selector) {
    try { return document.querySelector(selector); } catch (e) { return null; }
  }

  // --- text quote anchoring ---
  function normalize(s) {
    var out = "", map = [], prevSpace = false;
    for (var i = 0; i < s.length; i++) {
      var ch = s[i];
      if (ch === " " || ch === "\t" || ch === "\n" || ch === "\r" || ch === "\f") {
        if (prevSpace) continue;
        out += " "; map.push(i); prevSpace = true;
      } else { out += ch; map.push(i); prevSpace = false; }
    }
    return { out: out, map: map };
  }

  // Find a Range within `root` matching the (whitespace-insensitive) quote.
  function findQuoteRange(root, quote) {
    if (!root || !quote) return null;
    var nodes = [], w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null), n;
    while ((n = w.nextNode())) nodes.push(n);
    if (!nodes.length) return null;
    var raw = "", starts = [];
    for (var i = 0; i < nodes.length; i++) { starts.push(raw.length); raw += nodes[i].nodeValue; }
    var H = normalize(raw);
    var needle = normalize(quote).out.trim();
    if (!needle) return null;
    var idx = H.out.indexOf(needle);
    if (idx < 0) return null;
    var rawStart = H.map[idx];
    var rawEnd = H.map[idx + needle.length - 1] + 1;
    function locate(pos) {
      for (var k = nodes.length - 1; k >= 0; k--) {
        if (starts[k] <= pos) return { node: nodes[k], offset: Math.min(pos - starts[k], nodes[k].nodeValue.length) };
      }
      return { node: nodes[0], offset: 0 };
    }
    var a = locate(rawStart), b = locate(rawEnd);
    try { var r = document.createRange(); r.setStart(a.node, a.offset); r.setEnd(b.node, b.offset); return r; }
    catch (e) { return null; }
  }

  function applyHighlights() {
    if (!supportsHL) return;
    var hl = new Highlight();
    PINS.forEach(function (p) { if (p.range) hl.add(p.range); });
    CSS.highlights.set("hn-highlight", hl);
  }

  // --- element picker highlight (comment mode) ---
  function highlight(el) {
    if (HIGHLIGHT) HIGHLIGHT.classList.remove("hn-hover-outline");
    HIGHLIGHT = el;
    if (el && el.nodeType === 1) el.classList.add("hn-hover-outline");
  }
  function onMouseMove(e) { if (!MODE_ON) return; highlight(e.target); }
  function onClick(e) {
    if (!MODE_ON) return;
    e.preventDefault();
    e.stopPropagation();
    if (!e.target || e.target.nodeType !== 1) return;
    var sel = uniqueSelector(e.target);
    var r = e.target.getBoundingClientRect();
    var rect = { top: r.top, left: r.left, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
    parent.postMessage({ hn: "pick", selector: sel, element_text: snippet(e.target), rect: rect }, "*");
    highlight(null);
  }
  function setMode(on) {
    MODE_ON = on;
    document.body.style.cursor = on ? "crosshair" : "";
    if (on) hideSelButton();
    else highlight(null);
  }

  // --- text selection -> floating Comment bubble ---
  function ensureSelButton() {
    if (selBtn && selBtn.isConnected) return selBtn;
    selBtn = document.createElement("button");
    selBtn.className = "hn-sel-btn";
    selBtn.type = "button";
    selBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>Comment';
    selBtn.style.display = "none";
    // mousedown so the selection is still alive when we read it
    selBtn.addEventListener("mousedown", function (e) {
      e.preventDefault();
      e.stopPropagation();
      var sel = window.getSelection();
      if (!sel || sel.isCollapsed) { hideSelButton(); return; }
      var text = sel.toString().replace(/\s+/g, " ").trim();
      if (text.length < 2) { hideSelButton(); return; }
      if (text.length > 200) text = text.slice(0, 200);
      var range = sel.getRangeAt(0);
      var anchor = range.commonAncestorContainer;
      if (anchor.nodeType !== 1) anchor = anchor.parentElement;
      var selector = uniqueSelector(anchor) || "body";
      var rr = range.getBoundingClientRect();
      var rect = { top: rr.top, left: rr.left, right: rr.right, bottom: rr.bottom, width: rr.width, height: rr.height };
      parent.postMessage({ hn: "pick", selector: selector, element_text: text, rect: rect }, "*");
      hideSelButton();
      sel.removeAllRanges();
    });
    document.documentElement.appendChild(selBtn);
    return selBtn;
  }
  function showSelButton(rect) {
    var b = ensureSelButton();
    b.style.left = (rect.left + rect.width / 2) + "px";
    b.style.top = Math.max(8, rect.top - 42) + "px";
    b.style.transform = "translateX(-50%)";
    b.style.display = "flex";
  }
  function hideSelButton() { if (selBtn) selBtn.style.display = "none"; }
  function onSelectionChange() {
    clearTimeout(selTimer);
    selTimer = setTimeout(function () {
      if (MODE_ON) { hideSelButton(); return; }
      var sel = window.getSelection();
      if (!sel || sel.isCollapsed || sel.rangeCount === 0) { hideSelButton(); return; }
      var text = sel.toString().trim();
      if (text.length < 2) { hideSelButton(); return; }
      var rect = sel.getRangeAt(0).getBoundingClientRect();
      if (!rect || (!rect.width && !rect.height)) { hideSelButton(); return; }
      showSelButton(rect);
    }, 120);
  }

  // --- pins + highlights ---
  function ensureLayer() {
    if (pinLayer && pinLayer.isConnected) return pinLayer;
    pinLayer = document.getElementById("hn-pin-layer");
    if (!pinLayer) {
      pinLayer = document.createElement("div");
      pinLayer.id = "hn-pin-layer";
      document.documentElement.appendChild(pinLayer);
    }
    return pinLayer;
  }

  function renderPins(items) {
    var layer = ensureLayer();
    layer.innerHTML = "";
    PINS = [];
    (items || []).forEach(function (it) {
      var node = resolve(it.selector);
      if (!node) return;
      var range = it.quote ? findQuoteRange(node, it.quote) : null;
      var pin = document.createElement("div");
      pin.className = "hn-pin";
      pin.textContent = it.n;
      pin.title = "Comment " + it.n;
      pin.addEventListener("click", function (e) {
        e.preventDefault();
        e.stopPropagation();
        parent.postMessage({ hn: "pinclick", selector: it.selector, quote: it.quote || "" }, "*");
      });
      layer.appendChild(pin);
      PINS.push({ selector: it.selector, quote: it.quote || "", n: it.n, el: pin, node: node, range: range });
    });
    applyHighlights();
    positionPins();
  }

  function anchorRect(p) {
    if (p.range) { try { var rr = p.range.getBoundingClientRect(); if (rr.width || rr.height) return rr; } catch (e) {} }
    if (p.node && p.node.isConnected) return p.node.getBoundingClientRect();
    return null;
  }

  function positionPins() {
    if (!PINS.length) return;
    var sx = window.scrollX || window.pageXOffset;
    var sy = window.scrollY || window.pageYOffset;
    PINS.forEach(function (p) {
      var r = anchorRect(p);
      if (!r || (r.width === 0 && r.height === 0)) { p.el.style.display = "none"; return; }
      p.el.style.display = "";
      p.el.style.left = (r.right + sx) + "px";
      p.el.style.top = (r.top + sy) + "px";
    });
  }

  function scheduleReposition() {
    if (rafPending) return;
    rafPending = true;
    requestAnimationFrame(function () { rafPending = false; positionPins(); });
  }

  function locate(selector, quote) {
    var match = null;
    for (var i = 0; i < PINS.length; i++) {
      if (PINS[i].selector === selector && (PINS[i].quote || "") === (quote || "")) { match = PINS[i]; break; }
    }
    var node = match ? match.node : resolve(selector);
    if (!node) return;
    node.scrollIntoView({ behavior: "smooth", block: "center" });
    if (match && match.range && supportsHL) {
      var hl = new Highlight(); hl.add(match.range);
      CSS.highlights.set("hn-highlight-active", hl);
      setTimeout(function () { CSS.highlights.delete("hn-highlight-active"); }, 1300);
    } else {
      node.classList.remove("hn-pulse");
      void node.offsetWidth;
      node.classList.add("hn-pulse");
      setTimeout(function () { node.classList.remove("hn-pulse"); }, 1300);
    }
  }

  // --- wiring ---
  window.addEventListener("mousemove", onMouseMove, true);
  window.addEventListener("click", onClick, true);
  window.addEventListener("scroll", function () { scheduleReposition(); hideSelButton(); }, true);
  window.addEventListener("resize", scheduleReposition);
  document.addEventListener("selectionchange", onSelectionChange);
  window.addEventListener("load", function () {
    positionPins();
    setTimeout(positionPins, 300);
    setTimeout(positionPins, 1000);
  });

  window.addEventListener("message", function (e) {
    var d = e.data;
    if (!d || !d.hn) return;
    if (d.hn === "mode") setMode(d.on);
    else if (d.hn === "comments") renderPins(d.items);
    else if (d.hn === "locate") locate(d.selector, d.quote);
  });

  injectStyles();
})();
