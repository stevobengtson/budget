import Foundation

// Mirrors the JSON from GET /api/v1/budget. All *Cents fields are integer cents.
struct BudgetResponse: Codable {
    let month: String
    let prevMonth: String
    let nextMonth: String
    let summary: BudgetSummary
    let groups: [BudgetGroup]
}

struct BudgetSummary: Codable {
    let incomeCents: Int64
    let budgetedCents: Int64
    let remainingCents: Int64
}

struct BudgetGroup: Codable, Identifiable {
    let id: Int64
    let name: String
    let categories: [BudgetCategory]
}

struct BudgetCategory: Codable, Identifiable {
    let id: Int64
    let name: String
    let assignedCents: Int64
    let spentCents: Int64
    let availableCents: Int64
    let goalCents: Int64?
    let rolloverMode: String
}
