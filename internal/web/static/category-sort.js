// Drag-to-reorder for categories, within a group and between groups. Each group
// <tbody data-group> gets its own SortableJS instance whose items are the
// category rows (<tr data-category>), all sharing one Sortable group name so a
// row can be dragged from one group's tbody into another. This is nested inside
// the table-level group sort (see group-sort.js); they don't collide because the
// category drag uses its own handle (.js-category-handle) and the group drag
// uses .js-group-handle.
//
// On drop we persist the new order. Category group_id + sort_order are global
// (not per-month), so a move needs no month; but we send the page's month so the
// server can re-render the affected group headers (the "delete empty group"
// control depends on whether a group still has categories). Payload avoids
// duplicate form keys — which htmx.ajax's values object can't express — by
// packing the affected groups into two pipe-delimited fields:
//   groups = "<destGid>|<srcGid>"   cats = "<destIds,csv>|<srcIds,csv>"
// The source pair is only sent on a cross-group move.
(function () {
  function catIds(tbody) {
    return Array.prototype.map.call(
      tbody.querySelectorAll("tr[data-category]"),
      function (tr) {
        return tr.getAttribute("data-category-id");
      }
    );
  }

  function persist(to, from) {
    var table = to.closest("table");
    var month = table ? table.getAttribute("data-month") || "" : "";
    var groups = [to.getAttribute("data-group-id")];
    var cats = [catIds(to).join(",")];
    if (from !== to) {
      groups.push(from.getAttribute("data-group-id"));
      cats.push(catIds(from).join(","));
    }
    htmx.ajax("POST", "/budget/categories/reorder", {
      swap: "none",
      values: { month: month, groups: groups.join("|"), cats: cats.join("|") },
    });
  }

  function initCategorySort(tbody) {
    if (!window.Sortable || tbody.dataset.catSortInit) return;
    tbody.dataset.catSortInit = "1";
    Sortable.create(tbody, {
      draggable: "tr[data-category]",
      handle: ".js-category-handle",
      group: "budget-categories",
      animation: 150,
      ghostClass: "opacity-50",
      // A group's header row (first <tr>, data-group-head) isn't a draggable
      // item, so when dragging a category up to the top of a group — or into an
      // empty group where the header is the only row — SortableJS could try to
      // drop it *above* the header. Block any move that would insert a category
      // before a header, keeping categories pinned below their group title.
      onMove: function (evt) {
        if (evt.related && evt.related.hasAttribute("data-group-head")) {
          return evt.willInsertAfter;
        }
        return true;
      },
      onEnd: function (evt) {
        if (evt.from !== evt.to || evt.oldIndex !== evt.newIndex) {
          persist(evt.to, evt.from);
        }
      },
    });
  }

  function scan(root) {
    if (!root || !root.querySelectorAll) return;
    if (root.matches && root.matches("tbody[data-group]")) initCategorySort(root);
    root.querySelectorAll("tbody[data-group]").forEach(initCategorySort);
  }

  // htmx.onLoad fires on initial load and after every swap, so a group tbody
  // added or re-rendered by an htmx response gets its category sort initialized.
  if (window.htmx) {
    htmx.onLoad(scan);
  } else {
    document.addEventListener("DOMContentLoaded", function () {
      scan(document.body);
    });
  }
})();
