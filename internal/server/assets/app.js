// app.js — runs in the trusted parent page (same-origin as the server).
// Drives the comment UI, talks to the server API, and orchestrates the
// sandboxed iframe picker over postMessage. This is the only code that
// touches auth/cookies; the iframe is opaque-origin and untrusted.
(function () {
  "use strict";

  var SLUG = document.body.dataset.slug || "";
  var MODE_ON = false;
  var pendingPick = null;
  var loaded = false;

  var $ = function (id) { return document.getElementById(id); };
  var frame = $("hn-frame");
  var bar = $("hn-bar");
  var commentBtn = $("hn-comment-btn");
  var panelBtn = $("hn-panel-btn");
  var panel = $("hn-panel");
  var panelClose = $("hn-panel-close");
  var list = $("hn-comment-list");
  var count = $("hn-count");
  var composer = $("hn-composer");
  var composerForm = $("hn-comment-form");
  var targetEl = $("hn-target");
  var bodyInput = $("hn-body");
  var cancelBtn = $("hn-cancel");
  var hint = $("hn-hint");
  var hintGeneral = $("hn-hint-general");
  var errorEl = $("hn-error");
  var commentingAs = $("hn-commenting-as");
  var nameModal = $("hn-name-modal");
  var nameForm = $("hn-name-form");
  var nameModalInput = $("hn-name-input");
  var nameSkip = $("hn-name-skip");

  if (!SLUG || !frame || !commentBtn || !composerForm) {
    if (window.console) console.warn("html-now: missing page config or elements");
    return;
  }

  // --- name: asked once, then remembered ---
  function getName() {
    var m = document.cookie.match(/(?:^|; )hn_name=([^;]+)/);
    return m ? decodeURIComponent(m[1]).trim() : "";
  }
  function saveName(n) {
    document.cookie = "hn_name=" + encodeURIComponent(n) + "; path=/; max-age=" + (365 * 24 * 3600) + "; samesite=lax";
  }
  function markAsked() { try { localStorage.setItem("hn_name_asked", "1"); } catch (e) {} }
  function wasAsked() { try { return !!localStorage.getItem("hn_name_asked"); } catch (e) { return false; } }

  function openNameModal() {
    if (!nameModal) return;
    nameModalInput.value = getName();
    nameModal.hidden = false;
    setTimeout(function () { nameModalInput.focus(); nameModalInput.select(); }, 30);
  }
  function closeNameModal() {
    if (nameModal) nameModal.hidden = true;
    markAsked();
    setCommentingAs();
  }
  if (nameForm) nameForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var n = nameModalInput.value.trim();
    if (n) saveName(n);
    closeNameModal();
  });
  if (nameSkip) nameSkip.addEventListener("click", closeNameModal);

  // ask once, on first page load
  if (!getName() && !wasAsked()) openNameModal();

  function postToFrame(msg) {
    if (frame.contentWindow) frame.contentWindow.postMessage(msg, "*");
  }

  // dim the island after inactivity (paused while commenting)
  var dimTimer = null;
  function resetDim() {
    bar.classList.remove("hn-bar-dim");
    clearTimeout(dimTimer);
    if (MODE_ON) return;
    dimTimer = setTimeout(function () { bar.classList.add("hn-bar-dim"); }, 2500);
  }
  resetDim();
  bar.addEventListener("mouseenter", resetDim);
  bar.addEventListener("mouseleave", resetDim);

  function api(method, path, body) {
    var opts = { method: method, credentials: "same-origin", headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (r) {
      if (!r.ok) return r.json().then(function (e) { throw new Error(e.error || "error"); });
      return r.json();
    });
  }

  function setMode(on) {
    MODE_ON = on;
    commentBtn.classList.toggle("active", on);
    postToFrame({ hn: "mode", on: on });
    if (on) { showHint(); resetDim(); }
    else hideHint();
  }
  function showHint() { if (hint) hint.hidden = false; }
  function hideHint() { if (hint) hint.hidden = true; }

  function updateCount(n) {
    count.textContent = n;
    count.classList.toggle("hn-badge-zero", n === 0);
  }

  // A comment's anchor key: element selector + the exact text quote. Two
  // selections inside the same element get distinct pins/numbers.
  function keyOf(c) { return (c.selector || "") + "" + (c.element_text || ""); }

  // Number only anchored comments (element or text); shared with on-page pins.
  function numberMap(comments) {
    var map = {}, next = 1;
    comments.forEach(function (c) {
      if (!c.selector) return;
      var k = keyOf(c);
      if (!(k in map)) map[k] = next++;
    });
    return map;
  }

  function timeAgo(unix) {
    var s = Math.floor(Date.now() / 1000 - unix);
    if (s < 45) return "just now";
    if (s < 90) return "1m";
    var m = Math.round(s / 60);
    if (m < 60) return m + "m";
    var h = Math.round(m / 60);
    if (h < 24) return h + "h";
    var d = Math.round(h / 24);
    if (d < 7) return d + "d";
    return new Date(unix * 1000).toLocaleDateString();
  }

  function render(comments) {
    updateCount(comments.length);
    var nmap = numberMap(comments);

    // sync on-page pins/highlights (one per unique anchored target)
    var seen = {}, items = [];
    comments.forEach(function (c) {
      if (!c.selector) return;
      var k = keyOf(c);
      if (!seen[k]) { seen[k] = true; items.push({ selector: c.selector, quote: c.element_text || "", n: nmap[k] }); }
    });
    postToFrame({ hn: "comments", items: items });

    list.innerHTML = "";
    if (!comments.length) {
      var empty = document.createElement("li");
      empty.className = "hn-empty-state";
      empty.innerHTML = "<div class=\"hn-empty-icon\">💬</div>" +
        "<p>No comments yet</p>" +
        "<span>Hit <strong>Comment</strong> to pin one to an element or leave a note on the page.</span>";
      list.appendChild(empty);
      return;
    }

    comments.forEach(function (c) {
      var li = document.createElement("li");
      li.dataset.selector = c.selector || "";
      li.dataset.quote = c.element_text || "";

      var meta = document.createElement("div");
      meta.className = "hn-meta";
      var when = "<span title=\"" + escapeHtml(new Date(c.created_at * 1000).toLocaleString()) + "\">" + escapeHtml(timeAgo(c.created_at)) + "</span>";
      meta.innerHTML = "<strong>" + escapeHtml(c.author) + "</strong> · " + when;
      if (c.selector) {
        var num = document.createElement("span");
        num.className = "hn-cnum";
        num.textContent = nmap[keyOf(c)];
        meta.insertBefore(num, meta.firstChild);
      }
      li.appendChild(meta);

      if (c.selector && c.element_text) {
        var target = document.createElement("div");
        target.className = "hn-target";
        target.textContent = "“" + c.element_text + "”";
        target.title = c.selector;
        li.appendChild(target);
      } else if (!c.selector) {
        var scope = document.createElement("div");
        scope.className = "hn-scope";
        scope.textContent = "On this page";
        li.appendChild(scope);
      }

      var b = document.createElement("div");
      b.className = "hn-body";
      b.textContent = c.body;
      li.appendChild(b);

      if (c.selector) {
        li.classList.add("hn-locatable");
        li.addEventListener("click", function () { locateAndFlag(c.selector, c.element_text || "", li); });
      }
      list.appendChild(li);
    });
  }

  function escapeHtml(s) {
    var d = document.createElement("div");
    d.textContent = s;
    return d.innerHTML;
  }

  function loadComments() {
    if (!loaded) list.innerHTML = "<li class=\"hn-loading\">Loading comments…</li>";
    api("GET", "/api/uploads/" + SLUG + "/comments").then(function (c) {
      loaded = true;
      render(c);
    }).catch(function () { loaded = true; });
  }

  function openPanel() { panel.classList.add("hn-panel-open"); }
  function closePanel() { panel.classList.remove("hn-panel-open"); }

  function locateAndFlag(selector, quote, li) {
    postToFrame({ hn: "locate", selector: selector, quote: quote });
    if (li) {
      var prev = list.querySelector(".hn-row-active");
      if (prev) prev.classList.remove("hn-row-active");
      li.classList.add("hn-row-active");
    }
  }

  function setCommentingAs() {
    if (!commentingAs) return;
    var n = getName();
    commentingAs.innerHTML = "Commenting as <strong>" + escapeHtml(n || "Anonymous") + "</strong>";
  }

  function showComposer(pick) {
    pendingPick = pick;
    if (pick.selector) {
      targetEl.className = "hn-target";
      targetEl.textContent = pick.element_text ? "“" + pick.element_text + "”" : "↳ " + pick.selector;
      targetEl.title = pick.selector;
    } else {
      targetEl.className = "hn-target hn-target-general";
      targetEl.textContent = "Commenting on the page";
      targetEl.removeAttribute("title");
    }
    bodyInput.value = "";
    if (errorEl) { errorEl.hidden = true; errorEl.textContent = ""; }
    setCommentingAs();
    composer.hidden = false;
    positionComposer(pick.rect);
    bodyInput.focus();
  }

  // Anchor the composer near the picked element; centered above the bar for general.
  function positionComposer(rect) {
    composer.style.transform = "";
    var cw = composer.offsetWidth || 360;
    var ch = composer.offsetHeight || 200;
    var margin = 12;
    if (rect && (rect.width || rect.height)) {
      var left = rect.right + margin;
      var top = rect.top;
      if (left + cw > window.innerWidth - margin) left = rect.left - cw - margin;
      if (left < margin) left = margin;
      if (left + cw > window.innerWidth - margin) left = window.innerWidth - cw - margin;
      if (top + ch > window.innerHeight - 88) top = window.innerHeight - ch - 88;
      if (top < margin) top = margin;
      composer.style.left = left + "px";
      composer.style.top = top + "px";
      composer.style.bottom = "auto";
      return;
    }
    composer.style.left = "50%";
    composer.style.top = "auto";
    composer.style.bottom = "88px";
    composer.style.transform = "translateX(-50%)";
  }

  function hideComposer() {
    composer.hidden = true;
    pendingPick = null;
  }

  commentBtn.addEventListener("click", function () {
    setMode(!MODE_ON);
    if (MODE_ON) closePanel();
  });
  panelBtn.addEventListener("click", function () {
    if (panel.classList.contains("hn-panel-open")) closePanel();
    else { openPanel(); setMode(false); }
  });
  panelClose.addEventListener("click", closePanel);
  cancelBtn.addEventListener("click", function () { hideComposer(); setMode(false); });
  if (commentingAs) commentingAs.addEventListener("click", openNameModal);
  if (hintGeneral) hintGeneral.addEventListener("click", function () {
    setMode(false);
    showComposer({ selector: "", element_text: "", rect: null });
  });

  // Cmd/Ctrl+Enter submits from the textarea.
  bodyInput.addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      composerForm.requestSubmit ? composerForm.requestSubmit() : composerForm.dispatchEvent(new Event("submit", { cancelable: true }));
    }
  });

  composerForm.addEventListener("submit", function (e) {
    e.preventDefault();
    if (!pendingPick) return;
    var body = bodyInput.value.trim();
    if (!body) return;
    if (errorEl) { errorEl.hidden = true; errorEl.textContent = ""; }
    api("POST", "/api/uploads/" + SLUG + "/comments", {
      selector: pendingPick.selector,
      element_text: pendingPick.element_text,
      name: getName(),
      body: body
    }).then(function (c) {
      hideComposer();
      setMode(false);
      render(c);
      openPanel();
    }).catch(function (err) {
      if (errorEl) { errorEl.textContent = "Couldn’t post: " + err.message; errorEl.hidden = false; }
    });
  });

  window.addEventListener("message", function (e) {
    var d = e.data;
    if (!d || !d.hn) return;
    if (d.hn === "pick") {
      setMode(false);
      showComposer(d);
    } else if (d.hn === "pinclick") {
      setMode(false);
      openPanel();
      var rows = list.querySelectorAll("li[data-selector]");
      var match = null;
      for (var i = 0; i < rows.length; i++) {
        if (rows[i].dataset.selector === d.selector && (rows[i].dataset.quote || "") === (d.quote || "")) { match = rows[i]; break; }
      }
      if (match) {
        var prev = list.querySelector(".hn-row-active");
        if (prev) prev.classList.remove("hn-row-active");
        match.classList.add("hn-row-active");
        match.scrollIntoView({ block: "nearest" });
      }
    }
  });

  // escape closes the name modal first, otherwise composer / mode / panel
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    if (nameModal && !nameModal.hidden) { closeNameModal(); return; }
    hideComposer();
    setMode(false);
    closePanel();
  });

  frame.addEventListener("load", loadComments);
  loadComments();
})();
