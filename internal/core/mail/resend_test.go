package mail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResendSend(t *testing.T) {
	var gotAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	r := NewResend("re_test", "Budget <no@reply.dev>")
	r.endpoint = srv.URL // test hook
	err := r.Send(context.Background(), Message{
		To: "u@example.com", Subject: "Hi", HTML: "<b>hi</b>", Text: "hi",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotAuth != "Bearer re_test" {
		t.Errorf("auth header: got %q", gotAuth)
	}
	if body["from"] != "Budget <no@reply.dev>" || body["subject"] != "Hi" {
		t.Errorf("body: %+v", body)
	}
	if to, ok := body["to"].([]any); !ok || len(to) != 1 || to[0] != "u@example.com" {
		t.Errorf("to: %+v", body["to"])
	}
}
