package mail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capture stands in for a MeSend server, recording the one request it receives.
type capture struct {
	path   string
	auth   string
	body   map[string]any
	status int
	reply  string
}

func (c *capture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path = r.URL.Path
		c.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.body)
		status := c.status
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if c.reply != "" {
			_, _ = io.WriteString(w, c.reply)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMeSendPostsTheExpectedRequest(t *testing.T) {
	c := &capture{}
	srv := c.server(t)

	m := NewMeSend(srv.URL, "ms_test_key", "support@pigglet.ca")
	err := m.Send(context.Background(), Message{
		To:      "user@example.com",
		Subject: "Your Pigglet sign-in code",
		HTML:    "<p>123456</p>",
		Text:    "123456",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if c.path != "/api/v1/emails" {
		t.Errorf("path = %q, want /api/v1/emails", c.path)
	}
	if c.auth != "Bearer ms_test_key" {
		t.Errorf("auth = %q", c.auth)
	}
	if got, _ := c.body["from"].(string); got != "support@pigglet.ca" {
		t.Errorf("from = %q", got)
	}
	// The API takes a list even for a single recipient.
	to, ok := c.body["to"].([]any)
	if !ok || len(to) != 1 || to[0] != "user@example.com" {
		t.Errorf("to = %#v, want a one-element list", c.body["to"])
	}
	for _, k := range []string{"subject", "html", "text"} {
		if s, _ := c.body[k].(string); s == "" {
			t.Errorf("%s is empty in the payload", k)
		}
	}
}

// A successful send answers 201, not 200. Treating only 200 as success would
// make every delivered email look like a failure.
func TestMeSendAccepts201(t *testing.T) {
	c := &capture{status: http.StatusCreated, reply: `{"id":"<x@pigglet.ca>"}`}
	srv := c.server(t)

	m := NewMeSend(srv.URL, "k", "support@pigglet.ca")
	if err := m.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"}); err != nil {
		t.Fatalf("201 should be a success, got %v", err)
	}
}

// The server explains refusals in an {"error": "..."} body. Surfacing that text
// is the difference between a five-second fix and an investigation: the reason
// is nearly always actionable.
func TestMeSendSurfacesTheServersReason(t *testing.T) {
	c := &capture{status: http.StatusBadRequest, reply: `{"error":"domain pigglet.ca not found"}`}
	srv := c.server(t)

	m := NewMeSend(srv.URL, "k", "support@pigglet.ca")
	err := m.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if err == nil {
		t.Fatal("a 400 must be an error")
	}
	if !strings.Contains(err.Error(), "domain pigglet.ca not found") {
		t.Errorf("the server's reason should reach the caller, got: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("the status should be included too, got: %v", err)
	}
}

// A non-JSON body (a proxy error page, say) must still produce a useful error
// rather than an empty one.
func TestMeSendHandlesNonJSONErrorBody(t *testing.T) {
	c := &capture{status: http.StatusBadGateway, reply: "<html>502 Bad Gateway</html>"}
	srv := c.server(t)

	m := NewMeSend(srv.URL, "k", "support@pigglet.ca")
	err := m.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("want a 502 error carrying the body, got %v", err)
	}
}

// The base URL is configuration because the service may move off this box. A
// trailing slash is the obvious way to write it and must not produce a double
// slash in the path.
func TestMeSendNormalizesTheBaseURL(t *testing.T) {
	c := &capture{}
	srv := c.server(t)

	m := NewMeSend(srv.URL+"/", "k", "support@pigglet.ca")
	if err := m.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if c.path != "/api/v1/emails" {
		t.Errorf("path = %q, want /api/v1/emails", c.path)
	}
}

// MeSend validates `from` as a bare address, so the display-name form Resend
// accepts is rejected with a 400 before any mail is attempted.
//
// This is a configuration trap, not a code path: the driver passes `from`
// through untouched, and the failure appears only against a real server. The
// test documents the constraint next to the code that depends on it, so the
// next person to set mail.from finds it here rather than in a support ticket.
func TestMeSendFromMustBeABareAddress(t *testing.T) {
	const displayName = "Pigglet <support@pigglet.ca>"

	c := &capture{status: http.StatusBadRequest,
		reply: `{"error":"Key: 'SendEmailRequest.From' Error:Field validation for 'From' failed on the 'email' tag"}`}
	srv := c.server(t)

	m := NewMeSend(srv.URL, "k", displayName)
	err := m.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if err == nil {
		t.Fatal("a display-name From is refused by MeSend; the driver must report that")
	}
	if !strings.Contains(err.Error(), "'email' tag") {
		t.Errorf("the validation reason should reach the caller, got: %v", err)
	}
	// And the driver must not have quietly rewritten it — what is configured is
	// what gets sent, so the fix is visibly a config one.
	if got, _ := c.body["from"].(string); got != displayName {
		t.Errorf("from = %q, want it passed through verbatim", got)
	}
}
