// Drag-to-reorder for expense category groups. Each group is a
// <div data-group data-group-id> inside #budget-groups (class js-group-sort).
// The `draggable` selector + grip handle make the group divs the sole draggable
// items. Groups are the only children of that container now that income, credit
// and the totals have moved out of the list, so no onMove guard is needed.
// On drop we persist the new order via htmx (no swap — reordering changes no
// money, so there's nothing to re-render). The endpoint comes from the
// container's data-group-reorder-url when set (the Budget Estimate editor),
// defaulting to the budget page's.
(function () {
  function persistOrder(table) {
    var ids = Array.prototype.map.call(
      table.querySelectorAll("[data-group]"),
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
    table.dataset.sortableInit = "1";
    Sortable.create(table, {
      draggable: "[data-group]",
      handle: ".js-group-handle",
      animation: 150,
      ghostClass: "opacity-50",
      onMove: function (evt) {
        return !!(evt.related && evt.related.hasAttribute("data-group"));
      },
      onEnd: function (evt) {
        if (evt.oldIndex !== evt.newIndex) persistOrder(table);
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
