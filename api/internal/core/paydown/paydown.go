// Package paydown projects monthly debt amortization for credit/loan accounts.
//
// Each month the running balance accrues interest over the days in that
// calendar month using daily compounding at APR/365. After interest, the
// monthly payment is applied. If the payment exceeds the balance the payment
// is reduced to the balance and the loan is closed.
package paydown

import (
	"errors"
	"math"
	"time"
)

// PaymentSource labels where a month's scheduled payment came from.
type PaymentSource int

const (
	SourceDefault PaymentSource = iota
	SourceAssigned
	SourceSpent
)

func (p PaymentSource) String() string {
	switch p {
	case SourceSpent:
		return "spent"
	case SourceAssigned:
		return "assigned"
	}
	return "default"
}

// MonthPayment is one cell of a schedule given to Compute.
type MonthPayment struct {
	Cents  int64
	Source PaymentSource
}

// Row is one month of the projection.
type Row struct {
	Month         time.Time
	StartCents    int64
	InterestCents int64
	PaymentCents  int64
	PaymentSource PaymentSource
	BalanceCents  int64
}

// Plan is the full projection for one account.
type Plan struct {
	AccountID          int64
	AccountName        string
	AprBps             int64
	StartCents         int64
	PaymentCents       int64
	Rows               []Row
	TotalInterestCents int64
	TotalPaidCents     int64
	PayoffMonth        time.Time // zero when not paid off in horizon
	Diverging          bool      // true if payment ≤ first month's interest, debt grows forever
}

// Compute projects monthly amortization. start is the calendar month to begin
// in (day is ignored). The schedule's length caps the projection horizon — one
// MonthPayment per projected month. The plan's PaymentCents is the first
// month's scheduled payment (purely descriptive).
func Compute(accountID int64, name string, aprBps, startCents int64, schedule []MonthPayment, start time.Time) (Plan, error) {
	if aprBps < 0 {
		return Plan{}, errors.New("apr must be non-negative")
	}
	if len(schedule) == 0 {
		return Plan{}, errors.New("schedule must have at least one month")
	}

	firstPay := schedule[0].Cents
	if startCents <= 0 {
		// Already paid off; return empty plan.
		return Plan{
			AccountID:    accountID,
			AccountName:  name,
			AprBps:       aprBps,
			StartCents:   startCents,
			PaymentCents: firstPay,
		}, nil
	}

	apr := float64(aprBps) / 10_000.0 // 1999 → 0.1999
	daily := apr / 365.0

	plan := Plan{
		AccountID:    accountID,
		AccountName:  name,
		AprBps:       aprBps,
		StartCents:   startCents,
		PaymentCents: firstPay,
	}

	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	balance := startCents

	for i, mp := range schedule {
		days := daysInMonth(cur)
		factor := math.Pow(1+daily, float64(days)) - 1
		interest := int64(math.Round(float64(balance) * factor))
		afterInterest := balance + interest

		if i == 0 && mp.Cents > 0 && mp.Cents <= interest {
			plan.Diverging = true
		}

		pay := mp.Cents
		if pay > afterInterest {
			pay = afterInterest
		}
		end := afterInterest - pay

		plan.Rows = append(plan.Rows, Row{
			Month:         cur,
			StartCents:    balance,
			InterestCents: interest,
			PaymentCents:  pay,
			PaymentSource: mp.Source,
			BalanceCents:  end,
		})
		plan.TotalInterestCents += interest
		plan.TotalPaidCents += pay

		balance = end
		if balance <= 0 {
			plan.PayoffMonth = cur
			break
		}
		cur = cur.AddDate(0, 1, 0)
	}
	return plan, nil
}

// FlatSchedule helps callers that want a uniform monthly payment.
func FlatSchedule(paymentCents int64, months int) []MonthPayment {
	out := make([]MonthPayment, months)
	for i := range out {
		out[i] = MonthPayment{Cents: paymentCents, Source: SourceDefault}
	}
	return out
}

func daysInMonth(t time.Time) int {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	return first.AddDate(0, 1, -1).Day()
}
