package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Resend sends email via the Resend REST API (https://api.resend.com/emails).
type Resend struct {
	apiKey   string
	from     string
	endpoint string
	client   *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{
		apiKey:   apiKey,
		from:     from,
		endpoint: "https://api.resend.com/emails",
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (r *Resend) Send(ctx context.Context, m Message) error {
	payload := map[string]any{
		"from":    r.from,
		"to":      []string{m.To},
		"subject": m.Subject,
		"html":    m.HTML,
		"text":    m.Text,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
