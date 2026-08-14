// Keeps the URL in step with the selected tab, so a refresh, a bookmark, or a
// shared link lands on the tab the user was actually looking at.
//
// The tabs themselves are client-side: one page load renders every panel and
// clicking a trigger only reveals one. Without this the address bar still says
// /account however far the user has navigated, and a refresh silently drops
// them back to the first tab.
//
// history.replaceState, not pushState: switching tabs is view state, not
// navigation. With pushState, Back would walk through every tab the user
// happened to click before finally leaving the page, when what they almost
// always mean is "go back to where I came from".
//
// Opt in per tab group:
//   <div data-url-tabs="tab" data-url-tabs-default="account"> ... </div>
// where the attribute value is the query parameter to write. The default tab
// is written as no parameter at all, keeping the bare /account URL clean.
(function () {
	function sync(group, value) {
		var param = group.getAttribute("data-url-tabs");
		if (!param) return;

		var url = new URL(window.location.href);
		if (value && value !== group.getAttribute("data-url-tabs-default")) {
			url.searchParams.set(param, value);
		} else {
			url.searchParams.delete(param);
		}
		// Only touch history when something actually changed, so repeated
		// clicks on the same tab do not churn the entry.
		if (url.href !== window.location.href) {
			window.history.replaceState(window.history.state, "", url.href);
		}
	}

	document.addEventListener("click", function (e) {
		var trigger = e.target.closest("[data-tui-tabs-trigger]");
		if (!trigger) return;
		var group = trigger.closest("[data-url-tabs]");
		if (!group) return;
		sync(group, trigger.getAttribute("data-tui-tabs-value"));
	});
})();
