(function () {
  "use strict";

  const POSITION = "bottom-center";
  const DEFAULT_TYPE = "default";
  const DEFAULT_DURATION = 4500;
  const queuedToasts = [];
  const allowedTypes = new Set(["default", "success", "info", "warning", "danger"]);

  function cleanText(value) {
    return String(value || "").trim();
  }

  function normalizeToast(message, options) {
    const opts = options || {};
    const detail = typeof message === "object" && message !== null ? message : { message: message };
    const type = allowedTypes.has(cleanText(opts.type || detail.type)) ? cleanText(opts.type || detail.type) : DEFAULT_TYPE;
    const duration = Number(opts.duration || detail.duration || DEFAULT_DURATION);
    const normalized = {
      message: cleanText(opts.message || detail.message),
      description: cleanText(opts.description || detail.description),
      type: type,
      position: POSITION,
      duration: Number.isFinite(duration) ? duration : DEFAULT_DURATION,
    };
    if (!normalized.message && normalized.description) {
      normalized.message = normalized.description;
      normalized.description = "";
    }
    return normalized.message ? normalized : null;
  }

  function dispatchToast(detail) {
    if (!detail) return;
    if (window.__peekToastReady) {
      window.dispatchEvent(new CustomEvent("toast-show", { detail: detail }));
      return;
    }
    queuedToasts.push(detail);
  }

  function readToastNode(node) {
    const message = node.getAttribute("data-toast-message") || node.textContent || "";
    const detail = normalizeToast(message, {
      type: node.getAttribute("data-toast-type") || DEFAULT_TYPE,
      description: node.getAttribute("data-toast-description") || "",
      duration: node.getAttribute("data-toast-duration") || DEFAULT_DURATION,
    });
    node.remove();
    return detail;
  }

  function fallbackCopy(text, options) {
    return new Promise((resolve) => {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      let copied = false;
      try {
        copied = document.execCommand("copy");
      } catch (e) {
        copied = false;
      }
      document.body.removeChild(ta);
      resolve(copied ? copySucceeded(options) : copyFailed(options));
    });
  }

  function copySucceeded(options) {
    const opts = options || {};
    window.peekToast(opts.success || "Copied to clipboard.", { type: "success" });
    return true;
  }

  function copyFailed(options) {
    const opts = options || {};
    window.peekToast(opts.failure || "Copy failed.", {
      type: "danger",
      description: opts.failureDescription || "Select and copy the text manually.",
    });
    return false;
  }

  window.peekToast = function (message, options) {
    dispatchToast(normalizeToast(message, options));
  };

  window.peekCopyText = function (text, options) {
    const value = String(text || "");
    if (!value) {
      return Promise.resolve(copyFailed(options));
    }
    if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
      return navigator.clipboard.writeText(value)
        .then(() => copySucceeded(options))
        .catch(() => fallbackCopy(value, options));
    }
    return fallbackCopy(value, options);
  };

  document.addEventListener("alpine:init", () => {
    Alpine.data("peekToasts", () => ({
      toasts: [],
      nextID: 0,

      init() {
        window.__peekToastReady = true;
        while (queuedToasts.length > 0) {
          this.show(queuedToasts.shift());
        }
        this.$nextTick(() => {
          document.querySelectorAll("[data-peek-toast]").forEach((node) => {
            this.show(readToastNode(node));
          });
        });
      },

      show(detail) {
        const normalized = normalizeToast(detail || {});
        if (!normalized) return;
        const toast = {
          id: "peek-toast-" + (++this.nextID),
          show: false,
          message: normalized.message,
          description: normalized.description,
          type: normalized.type,
          role: normalized.type === "danger" || normalized.type === "warning" ? "alert" : "status",
          duration: normalized.duration,
        };
        this.toasts.push(toast);
        this.$nextTick(() => {
          toast.show = true;
          if (toast.duration > 0) {
            setTimeout(() => this.remove(toast.id), toast.duration);
          }
        });
      },

      remove(id) {
        const toast = this.toasts.find((item) => item.id === id);
        if (!toast) return;
        toast.show = false;
        setTimeout(() => {
          this.toasts = this.toasts.filter((item) => item.id !== id);
        }, 180);
      },

      typeClass(toast) {
        return "peek-toast-" + (toast.type || DEFAULT_TYPE);
      },
    }));
  });
})();
