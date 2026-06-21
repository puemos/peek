(function () {
  function setUploadMode(mode) {
    var fileInput = document.getElementById("hn-file-input");
    var pasteInput = document.getElementById("hn-paste-input");
    if (!fileInput || !pasteInput) return;
    fileInput.hidden = mode !== "file";
    pasteInput.hidden = mode === "file";
  }

  function copied(button) {
    var original = button.textContent;
    button.textContent = "copied!";
    setTimeout(function () {
      button.textContent = original;
    }, 1500);
  }

  function copyText(button, text) {
    if (!text) return;
    navigator.clipboard.writeText(text).then(function () {
      copied(button);
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
    copyText(button, url);
  });

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || !form.dataset.confirm) return;
    if (!window.confirm(form.dataset.confirm)) {
      event.preventDefault();
    }
  });
})();
