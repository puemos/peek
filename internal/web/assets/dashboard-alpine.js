(function () {
  "use strict";
  document.addEventListener("alpine:init", () => {
    Alpine.data("dashboard", (initial = {}) => ({
      mode: "file",
      visibility: "password",
      password: "",
      settingsTab: "auth",
      oauth: {
        google: Boolean(initial.googleOAuth),
        github: Boolean(initial.githubOAuth),
        oidc: Boolean(initial.oidcOAuth),
      },
      storageBackend: initial.storageBackend || "file",
      copied: "",
      fileName: "",
      fileSizeLabel: "",
      html: "",

      setSelectedFile(file) {
        if (!file) {
          this.fileName = "";
          this.fileSizeLabel = "";
          return;
        }
        this.fileName = file.name || "Selected file";
        this.fileSizeLabel = this.formatFileSize(file.size);
      },

      formatFileSize(bytes) {
        const size = Number(bytes);
        if (!Number.isFinite(size) || size < 0) return "";
        if (size < 1024) return `${size} B`;
        if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
        return `${(size / (1024 * 1024)).toFixed(1)} MB`;
      },

      canUpload() {
        if (this.visibility === "password" && this.password.trim() === "") return false;
        if (this.mode === "paste") return this.html.trim().length > 0;
        return this.fileName !== "";
      },

      guardUploadSubmit(event) {
        if (!this.canUpload()) {
          event.preventDefault();
        }
      },

      copy(text) {
        const value = String(text || "");
        return window.peekCopyText(value, { success: "Copied link." }).then((ok) => {
          if (ok) {
            this.copied = value;
            setTimeout(() => { if (this.copied === value) this.copied = ""; }, 1500);
          }
          return ok;
        });
      },

      absoluteURL(value) {
        const text = String(value || "");
        try {
          return new URL(text, window.location.origin).href;
        } catch (e) {
          return text;
        }
      },

      copyAbsolute(value) {
        this.copy(this.absoluteURL(value));
      },

      confirm(event, message) {
        if (!window.confirm(message)) {
          event.preventDefault();
        }
      },
    }));
  });
})();
