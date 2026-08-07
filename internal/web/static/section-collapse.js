// Manual collapse/expand of expense category groups. A group header holds a
// toggle marked data-collapse-group="<id>"; clicking it flips the
// group-collapsed class on that group's <div> — CSS (input.css) does the hiding
// — and records every collapsed id in one budget_groups_collapsed cookie so the
// server re-renders them the same way next load.
//
// Income and Credit used to be collapsible sections here too. The redesign made
// them panels, so the per-section branch (and its budget_<key>_collapsed
// cookies) is gone; only groups remain.
(function () {
  "use strict";

  // A class per group <div>, all collapsed ids in one cookie.

  // fmtMoney mirrors Go's money.Format: optional leading '-', '$', thousands-
  // grouped dollars, '.' and two-digit cents.
  function fmtMoney(cents) {
    var neg = cents < 0;
    if (neg) cents = -cents;
    var dollars = Math.floor(cents / 100).toLocaleString("en-US");
    var frac = String(cents % 100).padStart(2, "0");
    return (neg ? "-" : "") + "$" + dollars + "." + frac;
  }

  // availColor mirrors the Available column's coloring (see availColorClass in
  // budget_table.templ): positive sage, negative terracotta, zero warm neutral.
  // The money-* tokens flip in .dark, so there is no dark: variant to juggle.
  function availColor(el, cents) {
    el.classList.remove(
      "text-money-positive",
      "text-money-negative",
      "text-money-zero"
    );
    if (cents > 0) el.classList.add("text-money-positive");
    else if (cents < 0) el.classList.add("text-money-negative");
    else el.classList.add("text-money-zero");
  }

  function readGroups() {
    var m = document.cookie.match(
      /(?:^|;\s*)budget_groups_collapsed=([^;]*)/
    );
    if (!m || !m[1]) return [];
    return decodeURIComponent(m[1]).split(",").filter(Boolean);
  }

  function writeGroups(ids) {
    document.cookie =
      "budget_groups_collapsed=" +
      ids.join(",") +
      ";path=/;max-age=31536000;samesite=lax";
  }

  function toggleGroup(toggle) {
    var id = toggle.getAttribute("data-collapse-group");
    var group = document.getElementById("group-" + id);
    if (!group) return;

    var collapsed = group.classList.toggle("group-collapsed");
    if (collapsed) {
      // Sum the group's category Available (raw cents in data-available) so the
      // header total reflects edits made while it was expanded.
      var total = 0;
      group.querySelectorAll("[data-category]").forEach(function (row) {
        total += parseInt(row.getAttribute("data-available") || "0", 10);
      });
      var el = group.querySelector(".js-group-total-value");
      if (el) {
        el.textContent = fmtMoney(total);
        availColor(el, total);
      }
    }

    var ids = readGroups();
    var i = ids.indexOf(id);
    if (collapsed && i < 0) ids.push(id);
    else if (!collapsed && i >= 0) ids.splice(i, 1);
    writeGroups(ids);
  }

  document.addEventListener("click", function (e) {
    var group = e.target.closest("[data-collapse-group]");
    if (group) toggleGroup(group);
  });
})();
