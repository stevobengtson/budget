// Drag-to-reorder for expense category groups. Each group is a
// <tbody data-group data-group-id> inside the budget table (#budget-table,
// class js-group-sort). SortableJS is bound to the table itself because the
// group tbodies' only shared parent is the table; the `draggable` selector +
// grip handle make the group tbodies the sole draggable items, and onMove keeps
// them inside their band (never above Uncategorized or below the totals).
// On drop we persist the new order via htmx (no swap — reordering changes no
// money, so there's nothing to re-render).
(function () {
  function persistOrder(table) {
    var ids = Array.prototype.map.call(
      table.querySelectorAll("tbody[data-group]"),
      function (tb) {
        return tb.getAttribute("data-group-id");
      }
    );
    htmx.ajax("POST", "/budget/groups/reorder", {
      swap: "none",
      values: { ids: ids.join(",") },
    });
  }

  function initGroupSort(table) {
    if (!window.Sortable || table.dataset.sortableInit) return;
    table.dataset.sortableInit = "1";
    Sortable.create(table, {
      draggable: "tbody[data-group]",
      handle: ".js-group-handle",
      animation: 150,
      ghostClass: "opacity-50",
      // Only allow displacing another group, so a group can't be dropped among
      // the income/credit/uncategorized/totals tbodies.
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
