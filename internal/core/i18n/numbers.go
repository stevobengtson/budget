package i18n

// Number formatting rules per locale, following CLDR.
//
// These live in the i18n package rather than in money so that the money package
// depends on one small value type instead of on catalogs and contexts, and so
// the date tables next door can sit beside the rules they belong with.

// NumberFormat describes how a locale writes a decimal currency amount.
type NumberFormat struct {
	// Group separates thousands. French Canadian uses U+00A0 NO-BREAK SPACE, not
	// a plain space: a plain space lets an amount wrap across two lines
	// mid-number, and a non-breaking one is what CLDR specifies.
	//
	// Note this is where fr-CA and fr-FR part ways — fr-FR groups with U+202F
	// NARROW NO-BREAK SPACE, fr-CA with U+00A0. Verified against ICU 78.
	Group string
	// Decimal separates dollars from cents.
	Decimal string
	// Symbol is the currency sign.
	Symbol string
	// SymbolAfter puts the sign after the amount, separated by SymbolSpace.
	SymbolAfter bool
	// SymbolSpace sits between amount and sign when SymbolAfter is set.
	SymbolSpace string
}

// Numbers returns the locale's currency formatting rules.
//
// The two forms, verified against CLDR via Intl.NumberFormat (ICU 78), with
// U+00A0 shown as "_":
//
//	en-CA   $1,234,567.89       -$1,234,567.89
//	fr-CA   1_234_567,89_$      -1_234_567,89_$
//
// The separators are written as escapes rather than as literal characters on
// purpose: an invisible U+00A0 in source is indistinguishable from a plain
// space, and that difference is the whole point of this table.
func (l Locale) Numbers() NumberFormat {
	switch l {
	case FrCA:
		return NumberFormat{
			Group:       "\u00a0",
			Decimal:     ",",
			Symbol:      "$",
			SymbolAfter: true,
			SymbolSpace: "\u00a0",
		}
	default:
		return NumberFormat{
			Group:   ",",
			Decimal: ".",
			Symbol:  "$",
		}
	}
}
