(function () {
  "use strict";

  function setUploadMode(mode) {
    var fileInput = document.getElementById("hn-file-input");
    var pasteInput = document.getElementById("hn-paste-input");
    if (!fileInput || !pasteInput) return;
    fileInput.hidden = mode !== "file";
    pasteInput.hidden = mode === "file";
  }

  function showCopyFeedback(button, text) {
    var original = button.textContent;
    button.textContent = text;
    setTimeout(function () {
      button.textContent = original;
    }, 1500);
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

  function copyButtonURL(button, text) {
    if (!text) return;
    copyText(text).then(function () {
      showCopyFeedback(button, "copied!");
    }).catch(function () {
      showCopyFeedback(button, "copy failed");
    });
  }

  document.addEventListener("change", function (event) {
    var target = event.target;
    if (!target || target.name !== "mode") return;
    setUploadMode(target.value);
  });

  document.addEventListener("click", function (event) {
    var button = event.target.closest(".hn-copy-relative, .hn-copy-absolute");
    if (!button) return;
    var url = button.dataset.url || "";
    if (button.classList.contains("hn-copy-relative")) {
      url = window.location.origin + url;
    }
    copyButtonURL(button, url);
  });

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || !form.dataset.confirm) return;
    if (!window.confirm(form.dataset.confirm)) {
      event.preventDefault();
    }
  });
})();
