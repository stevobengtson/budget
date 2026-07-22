import Foundation

enum Money {
    // Formats integer cents as the app-wide currency string, matching the web's
    // money.Format exactly: "$1,234.56", "-$1,234.56".
    static func format(_ cents: Int64) -> String {
        let negative = cents < 0
        let value = negative ? -cents : cents
        let dollars = value / 100
        let frac = value % 100

        let grouping = NumberFormatter()
        grouping.numberStyle = .decimal
        grouping.groupingSeparator = ","
        let dollarStr = grouping.string(from: NSNumber(value: dollars)) ?? String(dollars)

        return String(format: "%@$%@.%02lld", negative ? "-" : "", dollarStr, frac)
    }

    // plain renders cents as an editable decimal without symbol/grouping, for
    // prefilling an amount field: 150050 -> "1500.50".
    static func plain(_ cents: Int64) -> String {
        let negative = cents < 0
        let value = negative ? -cents : cents
        return String(format: "%@%lld.%02lld", negative ? "-" : "", value / 100, value % 100)
    }
}
