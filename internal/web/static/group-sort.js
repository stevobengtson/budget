// Drag-to-reorder for expense category groups. Each group is a
// <div data-group data-group-id> inside the .js-group-list container (the
// budget page's #budget-groups, the estimate editor's #estimate-groups), which
// itself sits inside the .js-group-sort table that carries the page's data
// attributes. The `draggable` selector + grip handle make the group divs the
// sole draggable items.
//
// Sortable MUST be created on .js-group-list, not on .js-group-sort: SortableJS
// only reorders the direct children of its container, and the groups are
// grandchildren of the outer table (the redesign wrapped them so "Add group"
// has an insertion target). Created on the outer div, a drag starts but never
// drops anywhere — which is exactly how group sorting silently broke.
//
// On drop we persist the new order via htmx (no swap — reordering changes no
// money, so there's nothing to re-render). The endpoint comes from the
// table's data-group-reorder-url when set (the Budget Estimate editor),
// defaulting to the budget page's.
(function () {
  function persistOrder(table, list) {
    var ids = Array.prototype.map.call(
      list.querySelectorAll("[data-group]"),
      function (tb) {
        return tb.getAttribute("data-group-id");
      }
    );
    var url = table.dataset.groupReorderUrl || "/budget/groups/reorder";
    htmx.ajax("POST", url, {
      swap: "none",
      values: { ids: ids.join(",") },
    });
  }

  function initGroupSort(table) {
    if (!window.Sortable || table.dataset.sortableInit) return;
    var list = table.querySelector(".js-group-list");
    if (!list) return;
    table.dataset.sortableInit = "1";
    Sortable.create(list, {
      draggable: "[data-group]",
      handle: ".js-group-handle",
      animation: 150,
      ghostClass: "opacity-50",
      onMove: function (evt) {
        return !!(evt.related && evt.related.hasAttribute("data-group"));
      },
      onEnd: function (evt) {
        if (evt.oldIndex !== evt.newIndex) persistOrder(table, list);
      },
    });
  }

  function scan(root) {
    if (!root || !root.querySelectorAll) return;
    if (root.matches && root.matches(".js-group-sort")) initGroupSort(root);
    root.querySelectorAll(".js-group-sort").forEach(initGroupSort);
  }

  // htmx.onLoad fires on the initial load and after every swap, so a table
  // swapped in by an htmx response gets (re-)initialized.
  if (window.htmx) {
    htmx.onLoad(scan);
  } else {
    document.addEventListener("DOMContentLoaded", function () {
      scan(document.body);
    });
  }
})();
