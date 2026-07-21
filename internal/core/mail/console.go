package mail

import (
	"context"
	"log"
)

// Console logs emails instead of sending them. Use in local dev so the flow
// works without a Resend key.
type Console struct{}

func NewConsole() *Console { return &Console{} }

// TODO: In future lets create an HTML file as well that we can load in the browser
func (c *Console) Send(_ context.Context, m Message) error {
	log.Printf("[mail:console] to=%s subject=%q\n---\n%s\n---", m.To, m.Subject, m.Text)
	return nil
}
