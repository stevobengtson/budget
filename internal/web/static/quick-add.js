// Behaviour for the always-open quick-add transaction row (#tx-quickadd).
//
// The row is deliberately not re-rendered by its own save: htmx swaps only the
// row list (#tx-rows), which sits outside the form, so the fields keep their DOM
// identity and focus survives the round trip. That leaves three things for JS:
//
//   1. After a successful save, clear Amount and Payee but KEEP Date and
//      Account, then put the caret back in Amount. A run of receipts is then a
//      run of Enters with no reaching for the mouse.
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

  // Reset only the volatile fields. Date and account_id are intentionally left
  // alone — they are the things that stay the same across a batch of entries.
  function resetForNextEntry() {
    var amount = field("amount");
    var payee = field("payee");
    if (amount) amount.value = "";
    if (payee) payee.value = "";
    focusAmount();
  }

  document.body.addEventListener("htmx:afterRequest", function (e) {
    if (!e.target || e.target.id !== FORM) return;
    // Leave the typed values in place on a failed save so nothing is retyped.
    if (e.detail && e.detail.successful) resetForNextEntry();
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
    var amount = field("amount");
    var payee = field("payee");
    if (amount) amount.value = "";
    if (payee) payee.value = "";
    focusAmount();
  });
})();
