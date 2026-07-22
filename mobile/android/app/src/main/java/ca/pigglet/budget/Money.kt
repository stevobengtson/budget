package ca.pigglet.budget

import java.text.NumberFormat
import java.util.Locale

object Money {
    // Formats integer cents matching the web's money.Format exactly:
    // "$1,234.56", "-$1,234.56".
    fun format(cents: Long): String {
        val negative = cents < 0
        val value = if (negative) -cents else cents
        val dollars = value / 100
        val frac = value % 100
        val grouped = NumberFormat.getIntegerInstance(Locale.US).format(dollars)
        val sign = if (negative) "-" else ""
        return sign + "$" + grouped + "." + frac.toString().padStart(2, '0')
    }
}
