// app.js — runs in the trusted parent page (same-origin as the server).
// Drives the comment UI, talks to the server API, and orchestrates the
// sandboxed iframe picker over postMessage. This is the only code that
// touches same-origin APIs; the iframe is opaque-origin and untrusted.
(function () {
  "use strict";

  var SLUG = document.body.dataset.slug || "";

  var $ = function (id) { return document.getElementById(id); };
  var els = {
    frame: $("hn-frame"),
    bar: $("hn-bar"),
    commentBtn: $("hn-comment-btn"),
    panelBtn: $("hn-panel-btn"),
    panel: $("hn-panel"),
    panelClose: $("hn-panel-close"),
    exportWrap: $("hn-export"),
    exportBtn: $("hn-export-btn"),
    exportLabel: $("hn-export-label"),
    exportMenu: $("hn-export-menu"),
    exportMarkdownBtn: $("hn-export-markdown"),
    exportJSONBtn: $("hn-export-json"),
    list: $("hn-comment-list"),
    count: $("hn-count"),
    composer: $("hn-composer"),
    composerForm: $("hn-comment-form"),
    target: $("hn-target"),
    bodyInput: $("hn-body"),
    cancelBtn: $("hn-cancel"),
    hint: $("hn-hint"),
    hintGeneral: $("hn-hint-general"),
    error: $("hn-error"),
    commentingAs: $("hn-commenting-as"),
    nameModal: $("hn-name-modal"),
    nameForm: $("hn-name-form"),
    nameModalInput: $("hn-name-input"),
    nameSkip: $("hn-name-skip")
  };

  var state = {
    modeOn: false,
    pendingPick: null,
    commentsLoaded: false,
    comments: [],
    nameMemory: ""
  };

  if (!SLUG || !els.frame || !els.commentBtn || !els.composerForm) {
    if (window.console) console.warn("peek: missing page config or elements");
    return;
  }

  // --- name: asked once, then remembered locally ---
  function getName() {
    try { return (localStorage.getItem("hn_name") || "").trim(); }
    catch (e) { return state.nameMemory; }
  }
  function saveName(n) {
    state.nameMemory = n;
    try { localStorage.setItem("hn_name", n); } catch (e) {}
  }
  function markAsked() { try { localStorage.setItem("hn_name_asked", "1"); } catch (e) {} }
  function wasAsked() { try { return !!localStorage.getItem("hn_name_asked"); } catch (e) { return false; } }

  function openNameModal() {
    if (!els.nameModal) return;
    els.nameModalInput.value = getName();
    els.nameModal.hidden = false;
    setTimeout(function () { els.nameModalInput.focus(); els.nameModalInput.select(); }, 30);
  }
  function closeNameModal() {
    if (els.nameModal) els.nameModal.hidden = true;
    markAsked();
    setCommentingAs();
  }
  if (els.nameForm) els.nameForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var n = els.nameModalInput.value.trim();
    if (n) saveName(n);
    closeNameModal();
  });
  if (els.nameSkip) els.nameSkip.addEventListener("click", closeNameModal);

  // ask once, on first page load
  if (!getName() && !wasAsked()) openNameModal();

  function postToFrame(msg) {
    if (els.frame.contentWindow) els.frame.contentWindow.postMessage(msg, "*");
  }

  // dim the island after inactivity (paused while commenting)
  var dimTimer = null;
  function resetDim() {
    els.bar.classList.remove("hn-bar-dim");
    clearTimeout(dimTimer);
    if (state.modeOn) return;
    dimTimer = setTimeout(function () { els.bar.classList.add("hn-bar-dim"); }, 2500);
  }
  resetDim();
  els.bar.addEventListener("mouseenter", resetDim);
  els.bar.addEventListener("mouseleave", resetDim);

  function api(method, path, body) {
    var opts = { method: method, credentials: "same-origin", headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (r) {
      if (!r.ok) {
        return r.text().then(function (text) {
          var msg = text || ("HTTP " + r.status);
          if (text) {
            try {
              var data = JSON.parse(text);
              msg = data.error || data.message || msg;
            } catch (e) {}
          }
          throw new Error(msg);
        });
      }
      return r.json();
    });
  }

  function setMode(on) {
    state.modeOn = on;
    els.commentBtn.classList.toggle("active", on);
    postToFrame({ hn: "mode", on: on });
    if (on) { showHint(); resetDim(); }
    else hideHint();
  }
  function showHint() { if (els.hint) els.hint.hidden = false; }
  function hideHint() { if (els.hint) els.hint.hidden = true; }

  function updateCount(n) {
    els.count.textContent = n;
    els.count.classList.toggle("hn-badge-zero", n === 0);
  }

  function appendText(el, text) {
    el.appendChild(document.createTextNode(text));
  }

  function makeLoadingRow() {
    var li = document.createElement("li");
    li.className = "hn-loading";
    li.textContent = "Loading comments…";
    return li;
  }

  function makeLoadErrorRow(err) {
    var li = document.createElement("li");
    li.className = "hn-loading";
    li.textContent = "Couldn’t load comments: " + err.message;
    return li;
  }

  function makeEmptyState() {
    var empty = document.createElement("li");
    empty.className = "hn-empty-state";

    var icon = document.createElement("div");
    icon.className = "hn-empty-icon";
    icon.textContent = "💬";
    empty.appendChild(icon);

    var title = document.createElement("p");
    title.textContent = "No comments yet";
    empty.appendChild(title);

    var details = document.createElement("span");
    appendText(details, "Hit ");
    var strong = document.createElement("strong");
    strong.textContent = "Comment";
    details.appendChild(strong);
    appendText(details, " to pin one to an element or leave a note on the page.");
    empty.appendChild(details);

    return empty;
  }

  function makeMeta(c, nmap) {
    var meta = document.createElement("div");
    meta.className = "hn-meta";
    if (c.selector) {
      var num = document.createElement("span");
      num.className = "hn-cnum";
      num.textContent = nmap[keyOf(c)];
      meta.appendChild(num);
    }

    var author = document.createElement("strong");
    author.textContent = c.author || "";
    meta.appendChild(author);
    appendText(meta, " · ");

    var when = document.createElement("span");
    when.title = new Date(c.created_at * 1000).toLocaleString();
    when.textContent = timeAgo(c.created_at);
    meta.appendChild(when);

    return meta;
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
    state.comments = comments;
    updateCount(comments.length);
    if (els.exportWrap) els.exportWrap.hidden = comments.length === 0;
    if (!comments.length) closeExportMenu();
    var nmap = numberMap(comments);

    // sync on-page pins/highlights (one per unique anchored target)
    var seen = {}, items = [];
    comments.forEach(function (c) {
      if (!c.selector) return;
      var k = keyOf(c);
      if (!seen[k]) { seen[k] = true; items.push({ selector: c.selector, quote: c.element_text || "", n: nmap[k] }); }
    });
    postToFrame({ hn: "comments", items: items });

    var frag = document.createDocumentFragment();
    if (!comments.length) {
      frag.appendChild(makeEmptyState());
      els.list.replaceChildren(frag);
      return;
    }

    comments.forEach(function (c) {
      var li = document.createElement("li");
      li.dataset.selector = c.selector || "";
      li.dataset.quote = c.element_text || "";

      li.appendChild(makeMeta(c, nmap));

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
      frag.appendChild(li);
    });
    els.list.replaceChildren(frag);
  }

  // Render the loaded comments — each with the on-page anchor it points at —
  // as a Markdown document for pasting into an issue, doc, or agent prompt.
  function commentsToMarkdown(comments) {
    var lines = ["# Comments", "", "Page: " + location.href, ""];
    comments.forEach(function (c, i) {
      var when = new Date(c.created_at * 1000).toLocaleString();
      lines.push("## " + (i + 1) + ". " + (c.author || "anonymous") + " · " + when);
      if (c.element_text) {
        lines.push("**On:** > " + c.element_text.replace(/\s+/g, " ").trim());
      } else if (c.selector) {
        lines.push("**On:** `" + c.selector + "`");
      } else {
        lines.push("**On:** whole page");
      }
      lines.push("");
      lines.push(c.body);
      lines.push("");
    });
    return lines.join("\n");
  }

  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(ta);
      ok ? resolve() : reject(new Error("copy failed"));
    });
  }

  function setExportMenu(open) {
    if (!els.exportBtn || !els.exportMenu) return;
    els.exportMenu.hidden = !open;
    els.exportBtn.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) {
      var first = els.exportMenu.querySelector("button");
      if (first) first.focus();
    }
  }

  function closeExportMenu() { setExportMenu(false); }

  function toggleExportMenu() {
    if (!state.comments.length) return;
    setExportMenu(els.exportMenu ? els.exportMenu.hidden : false);
  }

  function showExportFeedback(text) {
    if (!els.exportBtn) return;
    var target = els.exportLabel || els.exportBtn;
    var prev = target.textContent;
    target.textContent = text;
    setTimeout(function () { target.textContent = prev; }, 1500);
  }

  function copyExport(text) {
    if (!state.comments.length) return;
    closeExportMenu();
    copyText(text).then(function () {
      showExportFeedback("Copied!");
    }).catch(function () {
      showExportFeedback("Copy failed");
    });
  }

  function exportMarkdown() {
    copyExport(commentsToMarkdown(state.comments));
  }

  function exportJSON() {
    copyExport(JSON.stringify(state.comments, null, 2));
  }

  function loadComments() {
    if (!state.commentsLoaded) {
      els.list.replaceChildren(makeLoadingRow());
    }
    api("GET", "/api/uploads/" + SLUG + "/comments").then(function (c) {
      state.commentsLoaded = true;
      render(c);
    }).catch(function (err) {
      state.commentsLoaded = true;
      els.list.replaceChildren(makeLoadErrorRow(err));
    });
  }

  function openPanel() { els.panel.classList.add("hn-panel-open"); }
  function closePanel() {
    els.panel.classList.remove("hn-panel-open");
    closeExportMenu();
  }

  function locateAndFlag(selector, quote, li) {
    postToFrame({ hn: "locate", selector: selector, quote: quote });
    if (li) {
      var prev = els.list.querySelector(".hn-row-active");
      if (prev) prev.classList.remove("hn-row-active");
      li.classList.add("hn-row-active");
    }
  }

  function setCommentingAs() {
    if (!els.commentingAs) return;
    var n = getName();
    var strong = document.createElement("strong");
    strong.textContent = n || "Anonymous";
    els.commentingAs.replaceChildren(document.createTextNode("Commenting as "), strong);
  }

  function showComposer(pick) {
    state.pendingPick = pick;
    if (pick.selector) {
      els.target.className = "hn-target";
      els.target.textContent = pick.element_text ? "“" + pick.element_text + "”" : "↳ " + pick.selector;
      els.target.title = pick.selector;
    } else {
      els.target.className = "hn-target hn-target-general";
      els.target.textContent = "Commenting on the page";
      els.target.removeAttribute("title");
    }
    els.bodyInput.value = "";
    if (els.error) { els.error.hidden = true; els.error.textContent = ""; }
    setCommentingAs();
    els.composer.hidden = false;
    positionComposer(pick.rect);
    els.bodyInput.focus();
  }

  // Anchor the composer near the picked element; centered above the bar for general.
  function positionComposer(rect) {
    els.composer.style.transform = "";
    var cw = els.composer.offsetWidth || 360;
    var ch = els.composer.offsetHeight || 200;
    var margin = 12;
    if (rect && (rect.width || rect.height)) {
      var left = rect.right + margin;
      var top = rect.top;
      if (left + cw > window.innerWidth - margin) left = rect.left - cw - margin;
      if (left < margin) left = margin;
      if (left + cw > window.innerWidth - margin) left = window.innerWidth - cw - margin;
      if (top + ch > window.innerHeight - 88) top = window.innerHeight - ch - 88;
      if (top < margin) top = margin;
      els.composer.style.left = left + "px";
      els.composer.style.top = top + "px";
      els.composer.style.bottom = "auto";
      return;
    }
    els.composer.style.left = "50%";
    els.composer.style.top = "auto";
    els.composer.style.bottom = "88px";
    els.composer.style.transform = "translateX(-50%)";
  }

  function hideComposer() {
    els.composer.hidden = true;
    state.pendingPick = null;
  }

  els.commentBtn.addEventListener("click", function () {
    setMode(!state.modeOn);
    if (state.modeOn) closePanel();
  });
  els.panelBtn.addEventListener("click", function () {
    if (els.panel.classList.contains("hn-panel-open")) closePanel();
    else { openPanel(); setMode(false); }
  });
  els.panelClose.addEventListener("click", closePanel);
  if (els.exportBtn) els.exportBtn.addEventListener("click", function (e) {
    e.stopPropagation();
    toggleExportMenu();
  });
  if (els.exportMarkdownBtn) els.exportMarkdownBtn.addEventListener("click", function (e) {
    e.stopPropagation();
    exportMarkdown();
  });
  if (els.exportJSONBtn) els.exportJSONBtn.addEventListener("click", function (e) {
    e.stopPropagation();
    exportJSON();
  });
  if (els.exportWrap) els.exportWrap.addEventListener("click", function (e) { e.stopPropagation(); });
  document.addEventListener("click", function (e) {
    if (els.exportWrap && !els.exportWrap.contains(e.target)) closeExportMenu();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") closeExportMenu();
  });
  els.cancelBtn.addEventListener("click", function () { hideComposer(); setMode(false); });
  if (els.commentingAs) els.commentingAs.addEventListener("click", openNameModal);
  if (els.hintGeneral) els.hintGeneral.addEventListener("click", function () {
    setMode(false);
    showComposer({ selector: "", element_text: "", rect: null });
  });

  // Cmd/Ctrl+Enter submits from the textarea.
  els.bodyInput.addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      els.composerForm.requestSubmit ? els.composerForm.requestSubmit() : els.composerForm.dispatchEvent(new Event("submit", { cancelable: true }));
    }
  });

  els.composerForm.addEventListener("submit", function (e) {
    e.preventDefault();
    if (!state.pendingPick) return;
    var body = els.bodyInput.value.trim();
    if (!body) return;
    if (els.error) { els.error.hidden = true; els.error.textContent = ""; }
    api("POST", "/api/uploads/" + SLUG + "/comments", {
      selector: state.pendingPick.selector,
      element_text: state.pendingPick.element_text,
      name: getName(),
      body: body
    }).then(function (c) {
      hideComposer();
      setMode(false);
      render(c);
      openPanel();
    }).catch(function (err) {
      if (els.error) { els.error.textContent = "Couldn’t post: " + err.message; els.error.hidden = false; }
    });
  });

  window.addEventListener("message", function (e) {
    if (e.source !== els.frame.contentWindow) return;
    var d = e.data;
    if (!d || !d.hn) return;
    if (d.hn === "pick") {
      setMode(false);
      showComposer(d);
    } else if (d.hn === "pinclick") {
      setMode(false);
      openPanel();
      var rows = els.list.querySelectorAll("li[data-selector]");
      var match = null;
      for (var i = 0; i < rows.length; i++) {
        if (rows[i].dataset.selector === d.selector && (rows[i].dataset.quote || "") === (d.quote || "")) { match = rows[i]; break; }
      }
      if (match) {
        var prev = els.list.querySelector(".hn-row-active");
        if (prev) prev.classList.remove("hn-row-active");
        match.classList.add("hn-row-active");
        match.scrollIntoView({ block: "nearest" });
      }
    }
  });

  // escape closes the name modal first, otherwise composer / mode / panel
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    if (els.nameModal && !els.nameModal.hidden) { closeNameModal(); return; }
    hideComposer();
    setMode(false);
    closePanel();
  });

  els.frame.addEventListener("load", loadComments);
  loadComments();
})();
