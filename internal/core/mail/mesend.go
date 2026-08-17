package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MeSend sends email via a self-hosted MeSend server.
//
// The API is Resend-shaped — Bearer token, the same JSON body — with two
// differences that matter here: the endpoint is our own host rather than a
// fixed vendor URL, and a successful send answers 201 rather than 200.
type MeSend struct {
	apiKey   string
	from     string
	endpoint string
	client   *http.Client
}

// NewMeSend builds the driver. baseURL is the server's public origin, e.g.
// "https://mesend.plainlysoftware.com"; the API path is appended here so that
// moving the service to another host is a one-value config change.
func NewMeSend(baseURL, apiKey, from string) *MeSend {
	return &MeSend{
		apiKey:   apiKey,
		from:     from,
		endpoint: strings.TrimRight(baseURL, "/") + "/api/v1/emails",
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (m *MeSend) Send(ctx context.Context, msg Message) error {
	payload := map[string]any{
		"from":    m.from,
		"to":      []string{msg.To},
		"subject": msg.Subject,
		"html":    msg.HTML,
		"text":    msg.Text,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("mesend send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 201 on success, so anything under 300 is a send.
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mesend status %d: %s", resp.StatusCode, errorText(resp.Body))
	}
	return nil
}

// errorText pulls the message out of MeSend's {"error": "..."} body, falling
// back to the raw text. The reason is almost always actionable — an unverified
// domain, a key without send permission, a rejected From address — and burying
// it behind a bare status code turns a five-second fix into an investigation.
func errorText(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, 8<<10))
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		return envelope.Error
	}
	return strings.TrimSpace(string(body))
}
