// Behaviour for the always-open quick-add transaction row (#tx-quickadd).
//
// A successful save re-renders the row server-side and htmx swaps it in out of
// band, which is what clears it: every field comes back at the same default it
// had on page load. The fields are templUI widgets whose value lives in a hidden
// input carrying no value attribute, so there is nothing for JS to restore them
// from — the defaults exist only in the Go template. That leaves three things
// for JS:
//
//   1. Put the caret back in Amount once the fresh row lands, so a run of
//      receipts is a run of Enters with no reaching for the mouse.
//   2. Swap From and To in transfer mode.
//   3. Esc clears the row.
//
// Field visibility per type is pure CSS (see input.css) and needs nothing here.
(function () {
  "use strict";

  var FORM = "tx-quickadd";

  function form() {
    return document.getElementById(FORM);
  }

  function field(name) {
    var f = form();
    return f ? f.elements[name] : null;
  }

  function focusAmount() {
    var a = field("amount");
    if (a) {
      a.focus();
      a.select();
    }
  }

  // htmx fires this on the newly swapped-in element, so the fresh row is already
  // in the document and getElementById finds it rather than the replaced node.
  // A failed save swaps nothing, so the typed values stay put and this never runs.
  document.body.addEventListener("htmx:oobAfterSwap", function (e) {
    if (!e.target || e.target.id !== FORM) return;
    focusAmount();
  });

  // The account pickers are templUI SelectBoxes, so the value lives in a hidden
  // input and the visible label is a separate element. selectbox.js watches
  // those hidden inputs and re-syncs the label and the selected item when the
  // value changes, so swapping is just swapping the two values — but the events
  // are dispatched explicitly rather than relying on its setter override, which
  // only fires when the component has already bound that input.
  function setSelectValue(input, value) {
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  }

  // Scoped to whichever transaction form the button sits in, so the same
  // handler serves the quick-add row and the edit sheet.
  document.addEventListener("click", function (e) {
    var swap = e.target.closest && e.target.closest("[data-tx-swap]");
    if (!swap) return;
    var f = swap.closest("[data-tx-form]");
    if (!f) return;
    var from = f.elements["account_id"];
    var to = f.elements["transfer_to"];
    if (!from || !to) return;
    var v = from.value;
    setSelectValue(from, to.value);
    setSelectValue(to, v);
  });

  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    var f = form();
    if (!f || !f.contains(e.target)) return;
    // The typed fields only: the pickers keep their values, matching what Esc
    // has always cleared here.
    ["amount", "payee", "notes"].forEach(function (name) {
      var el = field(name);
      if (el) el.value = "";
    });
    focusAmount();
  });
})();
