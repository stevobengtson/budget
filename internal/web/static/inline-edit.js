// Inline click-to-edit for the budget "Assigned" cells.
//
// Each cell renders a display <button> (.js-assign-display) plus a pre-filled
// <input> (.js-assign-input) that starts hidden, both inside the assign <form>.
// Clicking the display swaps to the input; blur or Enter saves via the form's
// htmx POST (which swaps the whole row back to display state); Escape cancels.
//
// Listeners are delegated on document so they survive htmx row swaps.
(function () {
  function inputFor(el) {
    return el.closest ? el.closest(".js-assign-input") : null;
  }

  // Click a display cell -> reveal its input, focused and selected.
  document.addEventListener("click", function (e) {
    var display = e.target.closest && e.target.closest(".js-assign-display");
    if (!display) return;
    var form = display.closest("form");
    var input = form && form.querySelector(".js-assign-input");
    if (!input) return;
    input.dataset.original = input.value;
    delete input.dataset.cancel;
    display.classList.add("hidden");
    input.classList.remove("hidden");
    input.focus();
    input.select();
  });

  // Enter commits (routes through blur -> single save); Escape cancels.
  document.addEventListener("keydown", function (e) {
    var input = inputFor(e.target);
    if (!input || input.classList.contains("hidden")) return;
    if (e.key === "Enter") {
      e.preventDefault();
      input.blur();
    } else if (e.key === "Escape") {
      e.preventDefault();
      input.dataset.cancel = "1";
      input.value = input.dataset.original || "";
      input.blur();
    }
  });

  // Blur = commit point. Revert UI to display; save only if changed and not
  // cancelled. Capture phase because blur does not bubble.
  document.addEventListener(
    "blur",
    function (e) {
      var input = inputFor(e.target);
      if (!input || input.classList.contains("hidden")) return;
      var form = input.closest("form");
      var display = form && form.querySelector(".js-assign-display");
      input.classList.add("hidden");
      if (display) display.classList.remove("hidden");
      if (input.dataset.cancel === "1") {
        delete input.dataset.cancel;
        return; // Escape: no save
      }
      if (input.value !== input.dataset.original) {
        form.requestSubmit(); // htmx posts, then swaps the row back to display
      }
    },
    true
  );
})();
