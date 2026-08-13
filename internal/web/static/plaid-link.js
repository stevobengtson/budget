// Plaid Link glue. The link modal fragment renders a [data-plaid-link] node
// carrying the link token and the URL to post the resulting public token to.
// This script (loaded once from the layout) watches HTMX swaps for that node,
// lazily loads Plaid's script from cdn.plaid.com, and opens Link.
//
// onSuccess posts public_token + institution metadata (plus the originating
// account id, when the flow started from an account's "connect" menu item)
// back through htmx so the server can swap in the next step.
(function () {
  "use strict";

  // Plaid's stable Link script. The /v2/ in the path is historical naming on
  // Plaid's side, not a version this app chose — do not "upgrade" it to v3
  // (that path does not exist and 403s).
  var CDN = "https://cdn.plaid.com/link/v2/stable/link-initialize.js";
  var loading = null;

  function loadPlaid() {
    if (window.Plaid) return Promise.resolve();
    if (loading) return loading;
    loading = new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.src = CDN;
      s.onload = resolve;
      s.onerror = function () {
        loading = null;
        reject(new Error("plaid script failed to load"));
      };
      document.head.appendChild(s);
    });
    return loading;
  }

  function closeModal() {
    var m = document.getElementById("modal");
    if (m) m.replaceChildren();
  }

  function open(el) {
    if (el.dataset.plaidOpened) return; // one launch per fragment
    el.dataset.plaidOpened = "1";
    loadPlaid().then(
      function () {
        var handler = window.Plaid.create({
          token: el.dataset.token,
          onSuccess: function (publicToken, metadata) {
            var values = {
              public_token: publicToken,
              institution_id: (metadata.institution && metadata.institution.institution_id) || "",
              institution_name: (metadata.institution && metadata.institution.name) || "",
            };
            if (el.dataset.account) values.account = el.dataset.account;
            window.htmx.ajax("POST", el.dataset.post, {
              values: values,
              target: "#modal",
              swap: "innerHTML",
            });
          },
          onExit: function () {
            closeModal();
          },
        });
        handler.open();
      },
      function () {
        closeModal();
      }
    );
  }

  function scan(root) {
    if (!(root instanceof Element)) return;
    var el = root.matches("[data-plaid-link]") ? root : root.querySelector("[data-plaid-link]");
    if (el) open(el);
  }

  document.addEventListener("htmx:afterSwap", function (e) {
    scan(e.detail.elt);
  });
})();
