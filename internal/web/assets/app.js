(function () {
  "use strict";

  const STORAGE_KEY = "hn_name";
  const AUTHOR_PALETTE = [
    { color: "#1677e8", soft: "rgba(22, 119, 232, .12)", text: "#0f5dbb" },
    { color: "#0e9f6e", soft: "rgba(14, 159, 110, .13)", text: "#047857" },
    { color: "#d97706", soft: "rgba(217, 119, 6, .14)", text: "#92400e" },
    { color: "#7c3aed", soft: "rgba(124, 58, 237, .13)", text: "#5b21b6" },
    { color: "#e11d48", soft: "rgba(225, 29, 72, .12)", text: "#be123c" },
    { color: "#0891b2", soft: "rgba(8, 145, 178, .13)", text: "#0e7490" },
  ];

  function frame() {
    const f = document.getElementById("hn-frame");
    return f ? f.contentWindow : null;
  }

  function postToFrame(msg) {
    const f = frame();
    if (f) {
      f.postMessage(msg, "*");
    }
  }

  function colorizeComments(comments) {
    const byAuthor = new Map();
    let colorIndex = 0;
    return comments.map((comment, index) => {
      const author = comment.author || "anonymous";
      if (!byAuthor.has(author)) {
        byAuthor.set(author, AUTHOR_PALETTE[colorIndex % AUTHOR_PALETTE.length]);
        colorIndex++;
      }
      return normalizeComment(comment, index, byAuthor.get(author));
    });
  }

  function normalizeComment(comment, index, color) {
    const created = comment.created_at ? new Date(comment.created_at * 1000) : null;
    return {
      ...comment,
      number: comment.selector ? index + 1 : 0,
      name: comment.author || "anonymous",
      scope: comment.anchor_kind === "text" ? "Text" : (comment.anchor_kind === "element" ? "Element" : "Page"),
      target: comment.element_text || comment.selector || "",
      created_human: created && !Number.isNaN(created.getTime()) ? created.toLocaleString() : "",
      authorColor: color.color,
      authorSoft: color.soft,
      authorText: color.text,
    };
  }

  function fallbackOriginRect() {
    const width = 56;
    const height = 42;
    return {
      left: window.innerWidth / 2 - width / 2,
      top: window.innerHeight / 2 - height / 2,
      width: width,
      height: height,
    };
  }

  function rectFromElement(el) {
    if (!el || typeof el.getBoundingClientRect !== "function") {
      return fallbackOriginRect();
    }
    const r = el.getBoundingClientRect();
    return {
      left: r.left,
      top: r.top,
      width: r.width,
      height: r.height,
    };
  }

  function normalizeRect(rect) {
    if (!rect) {
      return fallbackOriginRect();
    }
    const width = Math.max(24, Math.min(Number(rect.width) || 44, window.innerWidth - 16));
    const height = Math.max(24, Math.min(Number(rect.height) || 40, window.innerHeight - 16));
    const left = Math.min(Math.max(8, Number(rect.left) || 0), Math.max(8, window.innerWidth - width - 8));
    const top = Math.min(Math.max(8, Number(rect.top) || 0), Math.max(8, window.innerHeight - height - 8));
    return { left: left, top: top, width: width, height: height };
  }

  function setMorphOrigin(rect) {
    const anchor = document.getElementById("hn-morph-anchor");
    if (!anchor) return;
    const r = normalizeRect(rect);
    anchor.style.left = r.left + "px";
    anchor.style.top = r.top + "px";
    anchor.style.width = r.width + "px";
    anchor.style.height = r.height + "px";
  }

  function composerElement() {
    return document.getElementById("hn-composer");
  }

  async function responseMessage(res, fallback) {
    const contentType = res.headers.get("Content-Type") || "";
    if (contentType.indexOf("application/json") !== -1) {
      try {
        const data = await res.json();
        return (data && data.error) || fallback;
      } catch (e) {
        return fallback;
      }
    }
    try {
      const text = await res.text();
      return text || fallback;
    } catch (e) {
      return fallback;
    }
  }

  document.addEventListener("alpine:init", () => {
    Alpine.data("pageApp", () => ({
      slug: "",
      visibility: "password",

      comments: [],
      commentCount: 0,
      panelOpen: false,
      panelLoading: false,
      panelLoaded: false,
      activeComment: null,

      viewCount: 0,
      showSparkline: false,
      sparklineTotal: "",
      sparklineUnique: "",

      commentMode: false,
      composerOpen: false,
      composerTarget: null,
      composerTargetLabel: "",
      composerTargetIsGeneral: false,
      composerBody: "",
      composerError: "",

      name: localStorage.getItem(STORAGE_KEY) || "",
      nameInput: "",
      nameModalOpen: false,
      pendingCommentAfterName: false,
      allowAnonymousComment: false,

      init() {
        const dataset = document.body ? document.body.dataset : {};
        this.slug = dataset.slug || "";
        this.visibility = dataset.visibility || "password";

        const stored = localStorage.getItem(STORAGE_KEY);
        if (stored) {
          this.name = stored;
        } else if (!localStorage.getItem("hn_name_asked")) {
          this.nameModalOpen = true;
          localStorage.setItem("hn_name_asked", "1");
        }

        this.loadComments();
        this.loadViews();
        this.bindBridge();
        this.bindKeyboard();
      },

      loadComments() {
        if (!this.slug) return;
        this.panelLoading = true;
        fetch("/api/uploads/" + this.slug + "/comments", {
          credentials: "same-origin",
        })
          .then((r) => r.json())
          .then((data) => {
            const comments = Array.isArray(data) ? data : (data.comments || []);
            this.comments = colorizeComments(comments);
            this.commentCount = this.comments.length;
            this.panelLoading = false;
            this.panelLoaded = true;
            this.sendPins();
          })
          .catch(() => {
            this.panelLoading = false;
          });
      },

      loadViews() {
        if (!this.slug) return;
        fetch("/api/uploads/" + this.slug + "/views", {
          credentials: "same-origin",
        })
          .then((r) => r.json())
          .then((data) => {
            this.viewCount = data.total || 0;
            this.sparklineTotal = (data.total || 0) + " visits";
            this.sparklineUnique = (data.unique || 0) + " unique";
            this.renderSparkline((data.buckets || []).map((b) => b.n));
          })
          .catch(() => {});
      },

      renderSparkline(counts) {
        const svg = document.getElementById("hn-sparkline-svg");
        if (!svg || !counts.length) return;

        const w = 168, h = 56, pad = 2;
        const max = Math.max.apply(null, counts) || 1;
        const stepX = (w - pad * 2) / Math.max(counts.length - 1, 1);

        let points = "";
        for (let i = 0; i < counts.length; i++) {
          const x = pad + i * stepX;
          const y = h - pad - (counts[i] / max) * (h - pad * 4);
          points += (i ? " L " : "M ") + x.toFixed(1) + " " + y.toFixed(1);
        }

        const area = points + " L " + (pad + (counts.length - 1) * stepX).toFixed(1) + " " + (h - pad) + " L " + pad.toFixed(1) + " " + (h - pad) + " Z";
        const lastX = pad + (counts.length - 1) * stepX;
        const lastY = h - pad - (counts[counts.length - 1] / max) * (h - pad * 4);

        while (svg.firstChild) svg.removeChild(svg.firstChild);

        const NS = "http://www.w3.org/2000/svg";
        const areaPath = document.createElementNS(NS, "path");
        areaPath.setAttribute("d", area);
        areaPath.setAttribute("fill", "rgba(22,119,232,.14)");
        svg.appendChild(areaPath);

        const linePath = document.createElementNS(NS, "path");
        linePath.setAttribute("d", points);
        linePath.setAttribute("fill", "none");
        linePath.setAttribute("stroke", "rgb(22,119,232)");
        linePath.setAttribute("stroke-width", "1.5");
        linePath.setAttribute("stroke-linecap", "round");
        linePath.setAttribute("stroke-linejoin", "round");
        svg.appendChild(linePath);

        const dot = document.createElementNS(NS, "circle");
        dot.setAttribute("cx", lastX.toFixed(1));
        dot.setAttribute("cy", lastY.toFixed(1));
        dot.setAttribute("r", "2.5");
        dot.setAttribute("fill", "rgb(22,119,232)");
        svg.appendChild(dot);
      },

      bindBridge() {
        window.addEventListener("message", (e) => {
          if (e.source !== frame()) return;
          const data = e.data;
          if (!data) return;

          if (data.hn === "pick") {
            this.openComposer(data);
            return;
          }
          if (data.hn === "pinclick") {
            this.panelOpen = true;
            const match = this.comments.find((c) => c.selector === data.selector && (c.element_text || "") === (data.quote || ""));
            if (match) {
              this.activeComment = match.id;
              setTimeout(() => {
                if (this.activeComment === match.id) this.activeComment = null;
              }, 3000);
            }
            return;
          }
          if (!data.type) return;

          switch (data.type) {
            case "elementSelected":
              this.openComposer(data);
              break;
            case "commentModeEscaped":
              this.commentMode = false;
              break;
          }
        });
      },

      bindKeyboard() {
        document.addEventListener("keydown", (e) => {
          if (e.key === "Escape") {
            if (this.nameModalOpen) {
              this.closeNameModal();
              return;
            }
            if (this.composerOpen) {
              this.closeComposer();
              return;
            }
            if (this.commentMode) {
              this.commentMode = false;
              return;
            }
            if (this.panelOpen) {
              this.panelOpen = false;
            }
          }
        });
      },

      toggleCommentMode() {
        this.commentMode = !this.commentMode;
        if (this.commentMode) {
          postToFrame({ hn: "mode", on: true });
        } else {
          postToFrame({ hn: "mode", on: false });
          if (this.composerOpen) this.closeComposer();
        }
      },

      startGeneralComment(event) {
        setMorphOrigin(rectFromElement(event && event.currentTarget));
        this.composerTarget = { selector: "", text: "", anchorKind: "page", style: "" };
        this.composerTargetLabel = "General comment on this page";
        this.composerTargetIsGeneral = true;
        this.composerBody = "";
        this.composerError = "";
        this.showComposer();
      },

      openComposer(data) {
        setMorphOrigin(data.trigger_rect || data.rect);
        this.composerTarget = {
          selector: data.selector || "",
          text: data.element_text || data.text || "",
          anchorKind: data.anchor_kind || (data.selector ? "element" : "page"),
          style: "",
        };
        this.composerTargetLabel = data.target || data.element_text || data.text || data.selector || "General comment on this page";
        this.composerTargetIsGeneral = false;
        this.composerBody = "";
        this.composerError = "";
        this.commentMode = false;
        postToFrame({ hn: "mode", on: false });
        this.showComposer();
      },

      showComposer() {
        this.composerOpen = true;
        this.$nextTick(() => {
          const composer = composerElement();
          if (composer && typeof composer.showPopover === "function" && !composer.matches(":popover-open")) {
            try {
              composer.showPopover();
            } catch (e) {}
          }
          const ta = this.$refs.composerTextarea;
          if (ta) ta.focus();
        });
      },

      closeComposer() {
        const composer = composerElement();
        if (composer && typeof composer.hidePopover === "function" && composer.matches(":popover-open")) {
          try {
            composer.hidePopover();
          } catch (e) {}
        }
        this.composerOpen = false;
        this.scheduleComposerClear();
      },

      scheduleComposerClear() {
        setTimeout(() => {
          if (this.composerOpen) return;
          this.composerTarget = null;
          this.composerBody = "";
          this.composerError = "";
        }, 420);
      },

      syncComposerPopover(event) {
        if (event && event.newState === "closed" && this.composerOpen) {
          this.composerOpen = false;
          this.scheduleComposerClear();
        }
      },

      showComposerError(message) {
        this.composerError = message;
        window.peekToast(message, { type: "danger", duration: 6500 });
      },

      async postComment() {
        const body = (this.composerBody || "").trim();
        if (!body) return;

        this.composerError = "";
        if (!this.slug) {
          this.showComposerError("Page is still loading. Please try again.");
          return;
        }
        if (!this.name && !this.allowAnonymousComment) {
          this.pendingCommentAfterName = true;
          this.showNameModal();
          return;
        }
        this.pendingCommentAfterName = false;
        this.allowAnonymousComment = false;

        const target = this.composerTarget;
        const payload = { body: body, name: this.name || "" };
        if (target) {
          payload.selector = target.selector || "";
          payload.element_text = target.text || "";
          payload.anchor_kind = target.anchorKind || "";
        }

        try {
          const res = await fetch("/api/uploads/" + this.slug + "/comments", {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          });

          if (!res.ok) {
            const text = await responseMessage(res, "Failed to post comment.");
            this.showComposerError(text);
            return;
          }

          this.closeComposer();
          this.loadComments();
        } catch (e) {
          this.showComposerError("Network error. Please try again.");
        }
      },

      sendPins() {
        const items = this.comments
          .filter((c) => c.selector)
          .map((c) => ({
            selector: c.selector,
            quote: c.anchor_kind === "text" ? c.element_text : "",
            anchor_kind: c.anchor_kind,
            n: c.number,
            color: c.authorColor,
            author: c.name,
          }));
        postToFrame({ hn: "comments", items: items });
      },

      locateComment(c) {
        if (!c.selector) return;
        this.activeComment = c.id;
        postToFrame({ hn: "locate", selector: c.selector, quote: c.anchor_kind === "text" ? c.element_text : "" });
        setTimeout(() => {
          if (this.activeComment === c.id) this.activeComment = null;
        }, 3000);
      },

      exportComments(format) {
        let text = "";
        if (format === "json") {
          text = JSON.stringify(this.comments, null, 2);
        } else {
          for (const c of this.comments) {
            text += "## " + (c.name || "anonymous") + " (" + c.created_human + ")\n";
            if (c.target) text += "> on: " + c.target + "\n";
            text += "\n" + c.body + "\n\n---\n\n";
          }
        }
        window.peekCopyText(text, { success: format === "json" ? "Copied comments as JSON." : "Copied comments as Markdown." });
      },

      showNameModal() {
        this.nameInput = this.name || "";
        this.nameModalOpen = true;
        localStorage.setItem("hn_name_asked", "1");
        this.$nextTick(() => {
          const inp = this.$refs.nameInput;
          if (inp) inp.focus();
        });
      },

      closeNameModal() {
        this.nameModalOpen = false;
        this.pendingCommentAfterName = false;
        this.allowAnonymousComment = false;
      },

      saveName() {
        const n = (this.nameInput || "").trim();
        if (!n) {
          const inp = this.$refs.nameInput;
          if (inp) inp.focus();
          return;
        }
        const resume = this.pendingCommentAfterName;
        this.pendingCommentAfterName = false;
        this.allowAnonymousComment = false;
        this.name = n;
        localStorage.setItem(STORAGE_KEY, n);
        localStorage.setItem("hn_name_asked", "1");
        this.nameModalOpen = false;
        if (resume) {
          this.$nextTick(() => {
            this.postComment();
          });
        }
      },

      skipName() {
        const resume = this.pendingCommentAfterName;
        this.pendingCommentAfterName = false;
        this.nameModalOpen = false;
        localStorage.setItem("hn_name_asked", "1");
        if (resume) {
          this.allowAnonymousComment = true;
          this.$nextTick(() => {
            this.postComment();
          });
        }
      },

      toggleSparkline() {
        this.showSparkline = !this.showSparkline;
      },
    }));
  });
})();
