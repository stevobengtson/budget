import Foundation

// Mirrors GET /api/v1/accounts.
struct Account: Codable, Identifiable {
    let id: Int64
    let name: String
    let type: String
    let balanceCents: Int64
}

// Mirrors GET /api/v1/accounts/:id/transactions. amountCents is inflow − outflow
// (positive = money in).
struct TransactionItem: Codable, Identifiable {
    let id: Int64
    let date: String
    let payee: String
    let category: String
    let categoryId: Int64?
    let notes: String
    let amountCents: Int64
    let cleared: Bool
    let isTransfer: Bool
}

// Mirrors GET /api/v1/accounts?month=… — accounts with balances as of month-end.
struct AccountsPage: Codable {
    let month: String
    let prevMonth: String
    let nextMonth: String
    let accounts: [Account]
}

// Mirrors GET /api/v1/accounts/:id/transactions?month=… — a month of ledger rows.
struct TransactionsPage: Codable {
    let month: String
    let prevMonth: String
    let nextMonth: String
    let transactions: [TransactionItem]
}

// Mirrors GET /api/v1/categories — an option in the transaction category picker.
struct CategoryOption: Codable, Identifiable {
    let id: Int64
    let name: String
    let group: String
}
