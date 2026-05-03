package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
)

func main() {
	var dbPath string
	flag.StringVar(&dbPath, "db", "./data/budget.db", "path to SQLite database")
	flag.Parse()

	conn, dialect, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	sd := store.DialectSQLite
	if dialect == db.DialectPostgres {
		sd = store.DialectPostgres
	}
	s := store.NewWithDialect(conn, sd)
	ctx := context.Background()

	groups, _ := s.ListGroups(ctx)
	for _, g := range groups {
		if g.Name != "Income" {
			fmt.Println("database already has data — run 'make db-reset' first")
			os.Exit(1)
		}
	}

	if err := seed(ctx, s); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Println("seeded successfully")
}

// ── helpers ──────────────────────────────────────────────────────────────────

func ptr64(v int64) *int64    { return &v }
func ptrStr(s string) *string { return &s }
func ptrTime(t time.Time) *time.Time { return &t }

// cents converts a dollar amount to integer cents.
func cents(d float64) int64 { return int64(d * 100) }

// day returns midnight UTC of the given day within a "YYYY-MM" month string.
func day(month string, d int) time.Time {
	t, _ := time.Parse("2006-01", month)
	return t.AddDate(0, 0, d-1)
}

// ── seed ─────────────────────────────────────────────────────────────────────

func seed(ctx context.Context, s *store.Store) error {
	now := time.Now()
	monthKey := func(offset int) string {
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
			AddDate(0, offset, 0).Format("2006-01")
	}
	months := [3]string{monthKey(-2), monthKey(-1), monthKey(0)}

	// Find the system Income category created by migration 00005.
	allCats, _ := s.ListCategories(ctx, false)
	var incomeCatID int64
	for _, c := range allCats {
		if c.IsIncome {
			incomeCatID = c.ID
			break
		}
	}

	// ── Groups & Categories ───────────────────────────────────────────────────

	type catDef struct {
		name      string
		order     int64
		goalCents *int64
		goalDue   *time.Time
	}
	type groupDef struct {
		name  string
		order int64
		cats  []catDef
	}

	groupDefs := []groupDef{
		{name: "Housing", order: 0, cats: []catDef{
			{name: "Rent", order: 0, goalCents: ptr64(cents(1850))},
			{name: "Electricity", order: 1},
			{name: "Internet", order: 2},
			{name: "Renter's Insurance", order: 3},
		}},
		{name: "Food", order: 1, cats: []catDef{
			{name: "Groceries", order: 0},
			{name: "Restaurants", order: 1},
			{name: "Coffee & Drinks", order: 2},
		}},
		{name: "Transportation", order: 2, cats: []catDef{
			{name: "Gas", order: 0},
			{name: "Car Insurance", order: 1},
			{name: "Parking", order: 2},
		}},
		{name: "Health & Fitness", order: 3, cats: []catDef{
			{name: "Gym", order: 0},
			{name: "Medical", order: 1},
		}},
		{name: "Entertainment", order: 4, cats: []catDef{
			{name: "Streaming", order: 0},
			{name: "Fun Money", order: 1},
		}},
		{name: "Savings Goals", order: 5, cats: []catDef{
			{name: "Emergency Fund", order: 0, goalCents: ptr64(cents(15000))},
			{
				name:      "Vacation",
				order:     1,
				goalCents: ptr64(cents(3000)),
				goalDue:   ptrTime(time.Date(now.Year(), 9, 1, 0, 0, 0, 0, time.UTC)),
			},
		}},
		{name: "Personal", order: 6, cats: []catDef{
			{name: "Clothing", order: 0},
			{name: "Personal Care", order: 1},
		}},
		{name: "Debt", order: 7, cats: []catDef{
			{name: "Credit Card Payment", order: 0},
			{name: "Car Loan Payment", order: 1},
		}},
	}

	catID := map[string]int64{} // "Group/Category" → id
	for _, gd := range groupDefs {
		gid, err := s.CreateGroup(ctx, gd.name, gd.order)
		if err != nil {
			return fmt.Errorf("group %q: %w", gd.name, err)
		}
		for _, cd := range gd.cats {
			cid, err := s.CreateCategory(ctx, store.Category{
				GroupID:     gid,
				Name:        cd.name,
				SortOrder:   cd.order,
				GoalCents:   cd.goalCents,
				GoalDueDate: cd.goalDue,
			})
			if err != nil {
				return fmt.Errorf("category %q/%q: %w", gd.name, cd.name, err)
			}
			catID[gd.name+"/"+cd.name] = cid
		}
		fmt.Printf("  group %-20s  (%d categories)\n", gd.name, len(gd.cats))
	}

	// ── Accounts ──────────────────────────────────────────────────────────────

	ccPayCat := catID["Debt/Credit Card Payment"]

	checkingID, err := s.CreateAccount(ctx, store.Account{
		Name:                 "Main Checking",
		Type:                 store.TypeChecking,
		StartingBalanceCents: cents(4250),
	})
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}

	savingsID, err := s.CreateAccount(ctx, store.Account{
		Name:                 "High-Yield Savings",
		Type:                 store.TypeSavings,
		StartingBalanceCents: cents(11500),
	})
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}

	chaseID, err := s.CreateAccount(ctx, store.Account{
		Name:                 "Chase Sapphire",
		Type:                 store.TypeCredit,
		StartingBalanceCents: -cents(2840),
		CreditLimitCents:     ptr64(cents(5000)),
		AprBps:               ptr64(2124), // 21.24%
		MonthlyPaymentCents:  ptr64(cents(200)),
		IncludeInPaydown:     true,
		PaymentCategoryID:    &ccPayCat,
	})
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}

	_, err = s.CreateAccount(ctx, store.Account{
		Name:                 "Honda Car Loan",
		Type:                 store.TypeLoan,
		StartingBalanceCents: -cents(9600),
		AprBps:               ptr64(749), // 7.49%
		MonthlyPaymentCents:  ptr64(cents(285)),
		IncludeInPaydown:     true,
	})
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}
	fmt.Println("  accounts created")

	// ── Per-month data ────────────────────────────────────────────────────────

	type txDef struct {
		d     int    // day of month
		acct  int64
		cat   int64  // 0 = none
		payee string
		notes string
		out   float64
		in    float64
	}
	type xferDef struct {
		d    int
		from int64
		to   int64
		amt  float64
		cat  int64  // 0 = none
		note string
	}

	// Per-month varying amounts [Feb, Mar, Apr].
	electricity := [3]float64{78.30, 65.80, 72.40}
	grocery := [3][3]float64{
		{145.20, 98.75, 136.90},
		{137.45, 89.15, 122.30},
		{155.80, 104.20, 0},
	}
	gas := [3][2]float64{{53.40, 47.80}, {61.20, 44.90}, {58.40, 51.10}}
	restaurants := [3][2]struct {
		d     int
		amt   float64
		payee string
	}{
		{{7, 68.50, "Olive Garden"}, {14, 95.20, "The Capital Grille"}},
		{{8, 72.40, "Chipotle"}, {21, 54.80, "Noodles & Company"}},
		{{5, 84.30, "Bonefish Grill"}, {19, 61.50, "Panera Bread"}},
	}
	freelance := [3]float64{350, 0, 750}

	for mi, mo := range months {
		// Income entries (estimated/budgeted).
		if _, err := s.CreateIncome(ctx, store.Income{
			Month: mo, Name: "Salary", AmountCents: cents(5400), SortOrder: 0,
		}); err != nil {
			return err
		}
		if fl := freelance[mi]; fl > 0 {
			if _, err := s.CreateIncome(ctx, store.Income{
				Month: mo, Name: "Freelance", AmountCents: cents(fl), SortOrder: 1,
			}); err != nil {
				return err
			}
		}

		var txs []txDef
		var xfers []xferDef

		// Paycheck deposit.
		txs = append(txs, txDef{d: 1, acct: checkingID, cat: incomeCatID,
			payee: "Acme Corp", notes: "Paycheck", in: 5400})
		if fl := freelance[mi]; fl > 0 {
			txs = append(txs, txDef{d: 16, acct: checkingID, cat: incomeCatID,
				payee: "Upwork", in: fl})
		}

		// Fixed monthly (checking).
		txs = append(txs,
			txDef{d: 1, acct: checkingID, cat: catID["Housing/Rent"],
				payee: "Westside Properties", out: 1850},
			txDef{d: 1, acct: checkingID, cat: catID["Transportation/Car Insurance"],
				payee: "Progressive", out: 142},
			txDef{d: 5, acct: checkingID, cat: catID["Housing/Internet"],
				payee: "Comcast", out: 55},
			txDef{d: 12, acct: checkingID, cat: catID["Housing/Renter's Insurance"],
				payee: "State Farm", out: 18},
			txDef{d: 15, acct: checkingID, cat: catID["Debt/Car Loan Payment"],
				payee: "Honda Financial Services", out: 285},
			txDef{d: 20, acct: checkingID, cat: catID["Health & Fitness/Gym"],
				payee: "Planet Fitness", out: 40},
		)

		// Electricity.
		txs = append(txs, txDef{d: 10, acct: checkingID,
			cat: catID["Housing/Electricity"], payee: "City Electric Co",
			out: electricity[mi]})

		// Groceries (up to 3 trips).
		groceryPayees := []string{"Kroger", "Whole Foods", "Trader Joe's"}
		groceryDays := []int{3, 17, 25}
		for j, amt := range grocery[mi] {
			if amt > 0 {
				txs = append(txs, txDef{d: groceryDays[j], acct: checkingID,
					cat: catID["Food/Groceries"], payee: groceryPayees[j], out: amt})
			}
		}

		// Gas (2 fill-ups, checking).
		gasDays := []int{9, 23}
		gasPayees := []string{"Shell", "Chevron"}
		for j, amt := range gas[mi] {
			txs = append(txs, txDef{d: gasDays[j], acct: checkingID,
				cat: catID["Transportation/Gas"], payee: gasPayees[j], out: amt})
		}

		// Restaurants (Chase).
		for _, r := range restaurants[mi] {
			txs = append(txs, txDef{d: r.d, acct: chaseID,
				cat: catID["Food/Restaurants"], payee: r.payee, out: r.amt})
		}

		// Coffee (Chase).
		txs = append(txs,
			txDef{d: 6, acct: chaseID, cat: catID["Food/Coffee & Drinks"],
				payee: "Starbucks", out: 14.20},
			txDef{d: 20, acct: chaseID, cat: catID["Food/Coffee & Drinks"],
				payee: "Dutch Bros", out: 11.80},
		)

		// Streaming (Chase).
		txs = append(txs,
			txDef{d: 15, acct: chaseID, cat: catID["Entertainment/Streaming"],
				payee: "Netflix", out: 15},
			txDef{d: 15, acct: chaseID, cat: catID["Entertainment/Streaming"],
				payee: "Spotify", out: 10.99},
			txDef{d: 15, acct: chaseID, cat: catID["Entertainment/Streaming"],
				payee: "Hulu", out: 17.99},
		)

		// Credit card payment (transfer: Checking → Chase, tagged with CC Payment).
		xfers = append(xfers, xferDef{
			d: 22, from: checkingID, to: chaseID, amt: 200,
			cat: catID["Debt/Credit Card Payment"], note: "CC payment",
		})

		// Savings transfer (Checking → Savings).
		xfers = append(xfers, xferDef{
			d: 25, from: checkingID, to: savingsID, amt: 300,
		})

		// Create transactions.
		for _, tx := range txs {
			if tx.out == 0 && tx.in == 0 {
				continue
			}
			var catPtr *int64
			if tx.cat != 0 {
				catPtr = ptr64(tx.cat)
			}
			var payeePtr *string
			if tx.payee != "" {
				payeePtr = ptrStr(tx.payee)
			}
			var notesPtr *string
			if tx.notes != "" {
				notesPtr = ptrStr(tx.notes)
			}
			if _, err := s.CreateTransaction(ctx, store.Transaction{
				Date:         day(mo, tx.d),
				AccountID:    tx.acct,
				CategoryID:   catPtr,
				Payee:        payeePtr,
				Notes:        notesPtr,
				OutflowCents: cents(tx.out),
				InflowCents:  cents(tx.in),
				Cleared:      true,
			}); err != nil {
				return fmt.Errorf("transaction %q day %d: %w", tx.payee, tx.d, err)
			}
		}

		// Create transfers.
		for _, xf := range xfers {
			var catPtr *int64
			if xf.cat != 0 {
				catPtr = ptr64(xf.cat)
			}
			var notePtr *string
			if xf.note != "" {
				notePtr = ptrStr(xf.note)
			}
			if _, _, err := s.CreateTransfer(ctx, store.TransferInput{
				Date:          day(mo, xf.d),
				FromAccountID: xf.from,
				ToAccountID:   xf.to,
				AmountCents:   cents(xf.amt),
				CategoryID:    catPtr,
				Notes:         notePtr,
				Cleared:       true,
			}); err != nil {
				return fmt.Errorf("transfer day %d: %w", xf.d, err)
			}
		}

		// Budget assignments.
		assignments := map[string]float64{
			"Housing/Rent":                  1850,
			"Housing/Electricity":           90,
			"Housing/Internet":              55,
			"Housing/Renter's Insurance":    18,
			"Food/Groceries":                400,
			"Food/Restaurants":              150,
			"Food/Coffee & Drinks":          50,
			"Transportation/Gas":            80,
			"Transportation/Car Insurance":  142,
			"Transportation/Parking":        20,
			"Health & Fitness/Gym":          40,
			"Entertainment/Streaming":       45,
			"Entertainment/Fun Money":       50,
			"Savings Goals/Emergency Fund":  200,
			"Savings Goals/Vacation":        100,
			"Personal/Clothing":             50,
			"Personal/Personal Care":        30,
			"Debt/Credit Card Payment":      200,
			"Debt/Car Loan Payment":         285,
		}
		for key, d := range assignments {
			cid, ok := catID[key]
			if !ok {
				continue
			}
			if err := s.SetAssigned(ctx, mo, cid, cents(d)); err != nil {
				return fmt.Errorf("assign %q: %w", key, err)
			}
		}

		fmt.Printf("  month %s  transactions=%d  transfers=%d\n", mo, len(txs), len(xfers))
	}

	return nil
}
