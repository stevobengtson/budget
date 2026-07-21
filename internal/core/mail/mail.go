package mail

import "context"

// Message is a single email. HTML and Text are alternative bodies.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer sends a Message. Implementations must be safe for concurrent use.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}
