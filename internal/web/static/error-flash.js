// Surfaces failed htmx requests.
//
// htmx is configured (by default) not to swap 4xx/5xx responses — sensible,
// since an error body is not the content you asked for. But nothing was
// listening for the failure either, so every rejected action was invisible:
// the click simply did nothing, with the reason sitting unread in the response
// body. There are dozens of such paths (bad amount, transfer with no
// destination, deleting a group that still has history, a lapsed session).
//
// This puts the server's message on screen. It reuses #flash, the same slot the
// success toasts use, so there is one place messages appear.
//
// The message is inserted with textContent, never innerHTML: the body is
// server-authored but may quote user input back (a payee, an account name), and
// it is displayed, not interpreted.
(function () {
  "use strict";

  var TIMEOUT_MS = 6000;
  var timer = null;

  // The handlers reply with a plain-text reason. Anything that arrives as HTML
  // (an error page, a proxy's output) is not fit to show raw, so it falls back
  // to a generic line rather than dumping markup at the user.
  function messageFor(xhr) {
    var body = (xhr.responseText || "").trim();
    if (!body || body.charAt(0) === "<" || body.length > 200) {
      return xhr.status === 0
        ? "Could not reach the server."
        : "That didn't work (" + xhr.status + ").";
    }
    return body;
  }

  function show(text) {
    var slot = document.getElementById("flash");
    if (!slot) return;
    slot.replaceChildren();

    var el = document.createElement("div");
    el.className =
      "fixed bottom-4 right-4 z-50 flex max-w-md items-start gap-2 rounded-md " +
      "bg-surface-attention px-4 py-3 text-[14px] font-medium " +
      "text-surface-attention-fg shadow-organic-lg";
    el.setAttribute("role", "alert");
    el.textContent = text;
    slot.appendChild(el);

    clearTimeout(timer);
    timer = setTimeout(function () {
      if (slot.firstChild === el) slot.replaceChildren();
    }, TIMEOUT_MS);
  }

  // Some responses carry their own error UI. The settings forms re-render their
  // section with the message inline (wrong password, name required), which reads
  // far better than a toast — and the status stays a truthful 4xx rather than
  // being softened to 200 just to make htmx swap it.
  //
  // htmx will not swap a 4xx by default, so opt these in explicitly, and mark
  // the request so the toast below does not also fire for it.
  document.body.addEventListener("htmx:beforeSwap", function (e) {
    var t = e.detail.target;
    var s = e.detail.xhr.status;
    if (s >= 400 && s < 500 && t && t.closest && t.closest("[data-inline-errors]")) {
      e.detail.shouldSwap = true;
      e.detail.isError = false;
      e.detail.xhr.__handledInline = true;
    }
  });

  // A 4xx/5xx that came back from the server and had nowhere else to go.
  document.body.addEventListener("htmx:responseError", function (e) {
    if (e.detail.xhr.__handledInline) return;
    show(messageFor(e.detail.xhr));
  });

  // The request never completed — offline, DNS, connection reset.
  document.body.addEventListener("htmx:sendError", function () {
    show("Could not reach the server.");
  });
})();
