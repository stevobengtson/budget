package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// step pumps Update once and returns the resulting Bootstrap.
func step(b *Bootstrap, msg tea.Msg) *Bootstrap {
	updated, _ := b.Update(msg)
	return updated.(*Bootstrap)
}

func TestBootstrapLoadingView(t *testing.T) {
	b := NewBootstrap(":memory:", 0)
	// Inject a window size so the centered view has room to render.
	b = step(b, tea.WindowSizeMsg{Width: 80, Height: 24})
	out := b.View()
	if !strings.Contains(out, "Loading budget") {
		t.Errorf("loading view missing label, got:\n%s", out)
	}
	if !strings.Contains(out, ":memory:") {
		t.Errorf("loading view missing dsn hint, got:\n%s", out)
	}
}

func TestBootstrapErrorView(t *testing.T) {
	b := NewBootstrap("postgres://x:y@127.0.0.1:1/none", 100*time.Millisecond)
	b = step(b, tea.WindowSizeMsg{Width: 80, Height: 24})
	b = step(b, dbErrMsg{err: errString("connect refused")})

	out := b.View()
	if !strings.Contains(out, "Failed to load budget") {
		t.Errorf("error view missing title, got:\n%s", out)
	}
	if !strings.Contains(out, "connect refused") {
		t.Errorf("error view missing underlying error, got:\n%s", out)
	}
	if !strings.Contains(out, "Press q to quit") {
		t.Errorf("error view missing quit hint, got:\n%s", out)
	}
}

func TestBootstrapErrorQuitOnQ(t *testing.T) {
	b := NewBootstrap(":memory:", 0)
	b = step(b, tea.WindowSizeMsg{Width: 80, Height: 24})
	b = step(b, dbErrMsg{err: errString("boom")})

	_, cmd := b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected a quit cmd on q press, got nil")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("expected tea.Quit msg, got %T", msg)
	}
}

func TestBootstrapDSNCredentialsHidden(t *testing.T) {
	b := NewBootstrap("postgres://alice:supersecret@db.example/budget", 0)
	b = step(b, tea.WindowSizeMsg{Width: 100, Height: 24})
	out := b.View()
	if strings.Contains(out, "supersecret") {
		t.Errorf("password leaked into loading view:\n%s", out)
	}
	if !strings.Contains(out, "***@") {
		t.Errorf("expected masked credentials, got:\n%s", out)
	}
}

// errString is a tiny error type so the tests don't need to pull in
// errors.New for a single-line literal.
type errString string

func (e errString) Error() string { return string(e) }
