// Inline click-to-edit for text "name" fields on the budget page. Two modes,
// both delegated on document so they survive htmx swaps:
//
//   * RENAME (existing group/category/income): a display element
//     (.js-name-display) plus a hidden pre-filled <input class="js-name-input">
//     inside a <form hx-put=...>. Click reveals the input; Enter/blur submits
//     (only if changed and non-empty); Escape cancels.
//
//   * CREATE (new group/category): a visible <input class="js-name-new">,
//     autofocused, inside a <form hx-post=...>, wrapped in an element marked
//     [data-newrow]. Enter/blur with a non-empty value submits; blur while
//     empty (or Escape) removes the [data-newrow] element without saving.
(function () {
  function closestClass(el, cls) {
    return el && el.closest ? el.closest(cls) : null;
  }

  // Click a rename display -> reveal its input, focused and selected.
  document.addEventListener("click", function (e) {
    var display = e.target.closest && e.target.closest(".js-name-display");
    if (!display) return;
    var form = display.closest("form");
    var input = form && form.querySelector(".js-name-input");
    if (!input) return;
    input.dataset.original = input.value;
    delete input.dataset.cancel;
    display.classList.add("hidden");
    input.classList.remove("hidden");
    input.focus();
    input.select();
  });

  // Enter commits (routes through blur); Escape cancels.
  document.addEventListener("keydown", function (e) {
    var ni = closestClass(e.target, ".js-name-input");
    if (ni && !ni.classList.contains("hidden")) {
      if (e.key === "Enter") {
        e.preventDefault();
        ni.blur();
      } else if (e.key === "Escape") {
        e.preventDefault();
        ni.dataset.cancel = "1";
        ni.value = ni.dataset.original || "";
        ni.blur();
      }
      return;
    }
    var nw = closestClass(e.target, ".js-name-new");
    if (nw) {
      if (e.key === "Enter") {
        e.preventDefault();
        nw.blur();
      } else if (e.key === "Escape") {
        e.preventDefault();
        nw.dataset.cancel = "1";
        nw.blur();
      }
    }
  });

  // Blur = commit point (capture phase; blur does not bubble).
  document.addEventListener(
    "blur",
    function (e) {
      var ni = closestClass(e.target, ".js-name-input");
      if (ni && !ni.classList.contains("hidden")) {
        var form = ni.closest("form");
        var display = form && form.querySelector(".js-name-display");
        ni.classList.add("hidden");
        if (display) display.classList.remove("hidden");
        if (ni.dataset.cancel === "1") {
          delete ni.dataset.cancel;
          return;
        }
        if (ni.value.trim() !== "" && ni.value !== ni.dataset.original) {
          form.requestSubmit();
        }
        return;
      }
      var nw = closestClass(e.target, ".js-name-new");
      if (nw) {
        var row = nw.closest("[data-newrow]");
        if (nw.dataset.cancel === "1" || nw.value.trim() === "") {
          if (row) row.remove();
          return;
        }
        var f = nw.closest("form");
        if (f) f.requestSubmit();
      }
    },
    true
  );
})();
