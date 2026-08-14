// Passkey sign-in and enrolment.
//
// Strictly progressive enhancement. The sign-in page is a plain form POST that
// works with JavaScript disabled, and it has to stay that way: everything here
// is feature-detected and, when unsupported, degrades SILENTLY. A browser
// without WebAuthn should see the ordinary password form, not an error about a
// capability the user never asked for.
(function () {
	var supported = typeof window.PublicKeyCredential !== "undefined" &&
		typeof navigator.credentials !== "undefined";
	if (!supported) return;

	// WebAuthn speaks ArrayBuffers; JSON speaks base64url. These two functions
	// are the whole impedance mismatch.
	function fromB64url(s) {
		var pad = s.replace(/-/g, "+").replace(/_/g, "/");
		while (pad.length % 4) pad += "=";
		var raw = atob(pad);
		var out = new Uint8Array(raw.length);
		for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
		return out.buffer;
	}

	function toB64url(buf) {
		var bytes = new Uint8Array(buf);
		var s = "";
		for (var i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
		return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
	}

	// decodeOptions walks the server's JSON turning the base64url fields the
	// spec defines into the buffers the API demands.
	function decodeOptions(options) {
		var pk = options.publicKey || options;
		if (pk.challenge) pk.challenge = fromB64url(pk.challenge);
		if (pk.user && pk.user.id) pk.user.id = fromB64url(pk.user.id);
		["allowCredentials", "excludeCredentials"].forEach(function (key) {
			if (!Array.isArray(pk[key])) return;
			pk[key] = pk[key].map(function (c) {
				return Object.assign({}, c, { id: fromB64url(c.id) });
			});
		});
		return pk;
	}

	function encodeCredential(cred) {
		var r = cred.response;
		var out = {
			id: cred.id,
			rawId: toB64url(cred.rawId),
			type: cred.type,
			clientExtensionResults: cred.getClientExtensionResults(),
			response: { clientDataJSON: toB64url(r.clientDataJSON) },
		};
		if (r.attestationObject) {
			// Registration.
			out.response.attestationObject = toB64url(r.attestationObject);
			if (r.getTransports) out.transports = r.getTransports();
		} else {
			// Assertion.
			out.response.authenticatorData = toB64url(r.authenticatorData);
			out.response.signature = toB64url(r.signature);
			out.response.userHandle = r.userHandle ? toB64url(r.userHandle) : null;
		}
		return out;
	}

	function post(url, body) {
		return fetch(url, {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				// Ask for JSON explicitly. Without this the step-up middleware
				// answers with the HTML re-auth card, which this code cannot
				// render — the failure was a bare console 403 and nothing on
				// screen.
				"Accept": "application/json",
			},
			body: body ? JSON.stringify(body) : null,
			credentials: "same-origin",
		});
	}

	// reauthRequired detects the step-up refusal and puts the prompt on screen.
	//
	// The security screen locks its sensitive actions behind a recently proved
	// factor. Enrolling a passkey is one of them, so a session older than the
	// window is refused — and the user needs to be told that, and given the
	// prompt, rather than watching a button do nothing.
	function reauthRequired(response, data) {
		if (response.status !== 403) return false;
		if (!data || !data.error || data.error.code !== "reauth_required") return false;
		if (window.htmx) {
			// Swap the prompt into the section the user was blocked from. It
			// posts to /account/reauth and refreshes the page on success, which
			// brings the passkey card back with the window open.
			window.htmx.ajax("GET", "/account/reauth?next=/account%3Ftab%3Dsecurity", {
				target: "#account-passkeys",
				swap: "outerHTML",
			});
		} else {
			window.location.href = "/account?tab=security";
		}
		return true;
	}

	// --- Sign-in -----------------------------------------------------------

	// signIn runs the ceremony. mediation "conditional" powers autofill: the
	// browser offers a saved passkey from the email field itself, with no
	// button press, and MUST NOT surface an error if there is nothing to offer.
	function signIn(mediation) {
		return post("/webauthn/login/begin")
			.then(function (r) {
				if (!r.ok) throw new Error("begin failed");
				return r.json();
			})
			.then(function (options) {
				var pk = decodeOptions(options);
				var req = { publicKey: pk };
				if (mediation) req.mediation = mediation;
				return navigator.credentials.get(req);
			})
			.then(function (cred) {
				if (!cred) return null;
				return post("/webauthn/login/finish", encodeCredential(cred));
			})
			.then(function (r) {
				if (!r) return;
				return r.json().then(function (data) {
					if (!r.ok) throw new Error(data && data.error ? data.error.code : "rejected");
					if (data.status === "mfa_required") {
						// The authenticator did not verify the user, so the
						// account's second factor still applies.
						window.location.href = "/login?challenge=" + encodeURIComponent(data.challenge);
						return;
					}
					window.location.href = data.redirect || "/budget";
				});
			});
	}

	var loginButton = document.querySelector("[data-passkey-login]");
	if (loginButton) {
		loginButton.hidden = false;
		loginButton.addEventListener("click", function () {
			loginButton.disabled = true;
			signIn().catch(function () {
				// An aborted prompt is the usual outcome here — the user changed
				// their mind. Re-enable and say nothing.
				loginButton.disabled = false;
			});
		});
	}

	// Conditional mediation, where the browser supports it. Failures are
	// swallowed whole: this runs without the user asking for anything, so it
	// must never produce a visible error.
	if (document.querySelector("[data-passkey-autofill]") &&
		PublicKeyCredential.isConditionalMediationAvailable) {
		PublicKeyCredential.isConditionalMediationAvailable()
			.then(function (available) {
				if (available) signIn("conditional").catch(function () {});
			})
			.catch(function () {});
	}

	// --- Enrolment ---------------------------------------------------------

	var registerButton = document.querySelector("[data-passkey-register]");
	if (registerButton) {
		registerButton.hidden = false;
		registerButton.addEventListener("click", function () {
			registerButton.disabled = true;
			post("/account/passkeys/register/begin")
				.then(function (r) {
					return r.json().catch(function () { return null; }).then(function (data) {
						if (reauthRequired(r, data)) throw new Error("reauth");
						if (!r.ok) throw new Error("begin failed");
						return data;
					});
				})
				.then(function (options) {
					return navigator.credentials.create({ publicKey: decodeOptions(options) });
				})
				.then(function (cred) {
					if (!cred) return null;
					return post("/account/passkeys/register/finish", encodeCredential(cred));
				})
				.then(function (r) {
					if (r && r.ok && window.htmx) {
						// Re-render the card so the new passkey appears without
						// a full page load.
						window.htmx.ajax("GET", "/account/passkeys", { target: "#account-passkeys", swap: "outerHTML" });
					}
				})
				.catch(function () {})
				.then(function () {
					registerButton.disabled = false;
				});
		});
	}
})();
