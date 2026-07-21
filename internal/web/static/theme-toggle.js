// Three-state theme: "system" (default), "light", "dark". The pre-paint init
// lives inline in <head> (layout.templ) to avoid a flash; this file applies
// later changes from the user menu and reflects the active choice with a check.
(function () {
  function resolveDark(mode) {
    if (mode === "dark") return true;
    if (mode === "light") return false;
    return window.matchMedia("(prefers-color-scheme: dark)").matches; // system
  }
  function currentMode() {
    return localStorage.getItem("theme") || "system";
  }
  function applyTheme() {
    document.documentElement.classList.toggle("dark", resolveDark(currentMode()));
  }
  function markActiveTheme() {
    var mode = currentMode();
    document.querySelectorAll("[data-theme-option]").forEach(function (el) {
      var check = el.querySelector("[data-theme-check]");
      if (check) {
        check.classList.toggle("invisible", el.getAttribute("data-theme-option") !== mode);
      }
    });
  }
  // Called from the theme menu items.
  window.setTheme = function (mode) {
    localStorage.setItem("theme", mode);
    applyTheme();
    markActiveTheme();
  };
  // Follow OS changes while in system mode.
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
    if (currentMode() === "system") applyTheme();
  });
  document.addEventListener("DOMContentLoaded", markActiveTheme);
})();
