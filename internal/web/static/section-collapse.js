// Manual collapse/expand of budget sections (Income, Credit, ...). A section's
// header holds a toggle marked data-collapse="<key>"; clicking it flips the
// <key>-collapsed class on #budget-table — CSS (input.css) does the hiding — and
// records the state in a budget_<key>_collapsed cookie so the server re-renders
// it the same way next load. This is a bespoke, table-row collapse (each section
// is several <tbody>s in a shared table), separate from templUI's div-based
// collapsible.
//
// Optional total sync: if the section provides a live value element
// (.js-<key>-total-value, e.g. Income's totals row, updated by edits) and a
// header element (.js-<key>-total-header, shown only when collapsed), the header
// is refreshed from the live value as we collapse so it isn't stale. Sections
// whose total never changes client-side (e.g. Credit) simply omit the value
// element and the sync no-ops.
//
// Transient expansions (Add Income, copy-from-prev) intentionally don't go
// through here, so they never write a cookie and aren't remembered.
(function () {
  "use strict";

  function setCookie(key, collapsed) {
    document.cookie =
      "budget_" +
      key +
      "_collapsed=" +
      collapsed +
      ";path=/;max-age=31536000;samesite=lax";
  }

  // --- Sections (Income, Credit): one class on #budget-table, one cookie each.

  function toggleSection(toggle) {
    var key = toggle.getAttribute("data-collapse");
    var table = document.getElementById("budget-table");
    if (!table) return;
    var collapsed = table.classList.toggle(key + "-collapsed");
    if (collapsed) {
      var src = table.querySelector(".js-" + key + "-total-value");
      var dst = table.querySelector(".js-" + key + "-total-header");
      if (src && dst) dst.textContent = src.textContent.trim();
    }
    setCookie(key, collapsed);
  }

  // --- Expense groups: a class per group <tbody>, all ids in one cookie.

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
  // budget_table.templ): positive emerald, negative destructive, zero muted.
  function availColor(el, cents) {
    el.classList.remove(
      "text-emerald-600",
      "dark:text-emerald-400",
      "text-destructive",
      "text-muted-foreground"
    );
    if (cents > 0) el.classList.add("text-emerald-600", "dark:text-emerald-400");
    else if (cents < 0) el.classList.add("text-destructive");
    else el.classList.add("text-muted-foreground");
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
    var tbody = document.getElementById("group-" + id);
    if (!tbody) return;

    var collapsed = tbody.classList.toggle("group-collapsed");
    if (collapsed) {
      // Sum the group's category Available (raw cents in data-available) so the
      // header total reflects edits made while it was expanded.
      var total = 0;
      tbody.querySelectorAll("tr[data-category]").forEach(function (tr) {
        total += parseInt(tr.getAttribute("data-available") || "0", 10);
      });
      var el = tbody.querySelector(".js-group-total-value");
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
    if (group) {
      toggleGroup(group);
      return;
    }
    var section = e.target.closest("[data-collapse]");
    if (section) toggleSection(section);
  });
})();
