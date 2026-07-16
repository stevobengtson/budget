// Persists the open/closed state of collapsibles marked with
// data-persist-collapsible="<name>" into a budget_<name>_open cookie, so the
// server can re-render them in the same state on the next request (full load or
// #budget-region swap).
//
// The read is deferred to a macrotask so it runs after the click/keydown event
// has fully dispatched. templUI's collapsible handler may run before or after
// this one, so reading synchronously here can catch the pre-toggle state; by the
// time the deferred callback runs, data-tui-collapsible-state has settled.
(function () {
  "use strict";

  function persist(root) {
    var name = root.getAttribute("data-persist-collapsible");
    if (!name) return;
    var open = root.getAttribute("data-tui-collapsible-state") === "open";
    document.cookie =
      "budget_" + name + "_open=" + open + ";path=/;max-age=31536000;samesite=lax";
  }

  function onToggle(e) {
    var trigger = e.target.closest('[data-tui-collapsible="trigger"]');
    if (!trigger) return;
    var root = trigger.closest("[data-persist-collapsible]");
    if (!root) return;
    setTimeout(function () {
      persist(root);
    }, 0);
  }

  document.addEventListener("click", onToggle);
  document.addEventListener("keydown", function (e) {
    if (e.key === " " || e.key === "Enter") onToggle(e);
  });
})();
