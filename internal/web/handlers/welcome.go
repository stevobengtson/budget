package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// The first-run wizard: language, then the starter budget, then the first
// account. Reached by the requireOnboarded gate, which sends any signed-in user
// with a NULL onboarded_at here.
//
// Only two things are written before the last step, and neither touches the
// budget: the language choice (a preference, saved immediately so the remaining
// steps render in it) and, at Finish, everything else in one go. An abandoned
// wizard therefore leaves no half-seeded budget behind, and restarting it costs
// the user nothing but the language question they have already answered.

// WelcomeStart renders step 1. It is also where the gate redirects to, so it
// doubles as "resume the wizard".
func (h *Handlers) WelcomeStart(c *gin.Context) {
	ctx := c.Request.Context()
	render(c, http.StatusOK, views.WelcomeLanguage(views.WelcomeData{
		Locale: i18n.LocaleFrom(ctx),
	}))
}

// WelcomeLanguage saves the chosen language and renders step 2 in it.
//
// The locale is written to the user record and the cookie by the same path the
// user menu's language switcher uses, so there is one way a language gets saved.
func (h *Handlers) WelcomeLanguage(c *gin.Context) {
	l, ok := i18n.Parse(c.PostForm("locale"))
	if !ok {
		// Not reachable through the form, which only offers supported locales.
		l = i18n.Default
	}
	if err := h.store.UpdateUserLocale(c.Request.Context(), currentUserID(c), l); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	h.setLocaleCookie(c, l)

	// Render the rest of this response in the language just chosen, not the one
	// the request arrived with.
	ctx := i18n.WithLocale(c.Request.Context(), l)
	c.Request = c.Request.WithContext(ctx)

	render(c, http.StatusOK, views.WelcomeCategories(views.WelcomeData{Locale: l}))
}

// WelcomeCategories carries the kept starter groups into step 3.
func (h *Handlers) WelcomeCategories(c *gin.Context) {
	ctx := c.Request.Context()
	render(c, http.StatusOK, views.WelcomeAccount(views.WelcomeData{
		Locale: i18n.LocaleFrom(ctx),
		Groups: keptGroups(c),
	}))
}

// WelcomeBack re-renders step 2 with the groups the user had kept, so stepping
// back does not discard the choice.
func (h *Handlers) WelcomeBack(c *gin.Context) {
	ctx := c.Request.Context()
	render(c, http.StatusOK, views.WelcomeCategories(views.WelcomeData{
		Locale: i18n.LocaleFrom(ctx),
		Groups: keptGroups(c),
	}))
}

// WelcomeFinish commits the wizard: seeds the kept starter groups, creates the
// first account, and marks the user onboarded.
//
// Order matters. The account is created first because it is the step that can
// fail on user input (a malformed balance), and failing after seeding would
// leave the user re-answering the wizard against a budget that already has its
// categories. Marking onboarded is last, so any earlier failure leaves the
// wizard resumable.
func (h *Handlers) WelcomeFinish(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	loc := i18n.LocaleFrom(ctx)
	groups := keptGroups(c)

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		h.renderWelcomeAccountErr(c, loc, groups, i18n.T(ctx, "err.name_required"))
		return
	}

	a := store.Account{Name: name, Type: store.AccountType(c.PostForm("type"))}
	if v := strings.TrimSpace(c.PostForm("starting_balance")); v != "" {
		cents, err := money.Parse(ctx, v)
		if err != nil {
			h.renderWelcomeAccountErr(c, loc, groups, i18n.T(ctx, "err.invalid_balance"))
			return
		}
		// Liability convenience, matching the account sheet: a positive figure
		// typed against a card or loan is what is owed, so it is stored negative.
		if (a.Type == store.TypeCredit || a.Type == store.TypeLoan) && cents > 0 {
			cents = -cents
		}
		a.StartingBalanceCents = cents
	}

	if _, err := h.store.CreateAccount(ctx, uid, a); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SeedNewUser(ctx, uid, loc, groups); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.MarkOnboarded(ctx, uid); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/budget")
}

func (h *Handlers) renderWelcomeAccountErr(c *gin.Context, loc i18n.Locale, groups []string, msg string) {
	render(c, http.StatusBadRequest, views.WelcomeAccount(views.WelcomeData{
		Locale: loc, Groups: groups, Err: msg,
	}))
}

// keptGroups reads the starter-budget groups the user kept. An answered step
// with everything unticked posts no values at all, which must mean "no expense
// groups" — not "unanswered" — so it returns a non-nil empty slice, which
// SeedNewUser distinguishes from nil ("all of them").
func keptGroups(c *gin.Context) []string {
	groups := c.PostFormArray("groups")
	if groups == nil {
		return []string{}
	}
	return groups
}
