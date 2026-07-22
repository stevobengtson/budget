import SwiftUI

struct BudgetView: View {
    @EnvironmentObject private var session: Session

    @State private var budget: BudgetResponse?
    @State private var month: String?          // nil = current month
    @State private var loading = false
    @State private var error: String?

    // Inline assign editor.
    @State private var editing: BudgetCategory?
    @State private var editAmount = ""

    var body: some View {
        Group {
            if let budget {
                loaded(budget)
            } else if loading {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error {
                failure(error)
            } else {
                Color.clear
            }
        }
        .task(id: month) { await load() }
        .alert("Assign to \(editing?.name ?? "")", isPresented: showingEditor) {
            TextField("Amount", text: $editAmount)
                .keyboardType(.decimalPad)
            Button("Cancel", role: .cancel) {}
            Button("Save") {
                // Capture synchronously: SwiftUI dismisses the alert on tap, which
                // clears `editing` before an async Task would read it.
                guard let categoryId = editing?.id else { return }
                let amount = editAmount
                Task { await save(categoryId: categoryId, amount: amount) }
            }
        }
        .alert(error ?? "Something went wrong", isPresented: showingError) {
            Button("OK", role: .cancel) {}
        }
    }

    // MARK: - States

    @ViewBuilder
    private func loaded(_ budget: BudgetResponse) -> some View {
        VStack(spacing: 0) {
            HStack {
                Text("Budget").font(.largeTitle.bold())
                Spacer()
            }
            .padding(.horizontal)
            .padding(.top, 8)

            monthHeader(budget)
            List {
                Section {
                    summaryRow("Income", budget.summary.incomeCents, tint: .primary)
                    summaryRow("Assigned", budget.summary.budgetedCents, tint: .primary)
                    summaryRow("Left to assign", budget.summary.remainingCents,
                               tint: budget.summary.remainingCents < 0 ? .red : .green)
                }
                ForEach(budget.groups) { group in
                    Section(group.name) {
                        if group.categories.isEmpty {
                            Text("No categories").foregroundStyle(.secondary)
                        } else {
                            ForEach(group.categories) { cat in
                                Button { beginEdit(cat) } label: { categoryRow(cat) }
                                    .buttonStyle(.plain)
                            }
                        }
                    }
                }
            }
            .listStyle(.insetGrouped)
        }
    }

    private func failure(_ message: String) -> some View {
        VStack(spacing: 12) {
            Text(message).foregroundStyle(.secondary)
            Button("Retry") { Task { await load() } }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Pieces

    private func monthHeader(_ budget: BudgetResponse) -> some View {
        HStack {
            Button { month = budget.prevMonth } label: { Image(systemName: "chevron.left") }
            Spacer()
            Text(monthLabel(budget.month)).font(.headline)
            Spacer()
            Button { month = budget.nextMonth } label: { Image(systemName: "chevron.right") }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    private func summaryRow(_ label: String, _ cents: Int64, tint: Color) -> some View {
        HStack {
            Text(label)
            Spacer()
            Text(Money.format(cents)).foregroundStyle(tint).monospacedDigit()
        }
    }

    private func categoryRow(_ cat: BudgetCategory) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(cat.name)
                Text("Assigned \(Money.format(cat.assignedCents)) · Spent \(Money.format(cat.spentCents))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Text(Money.format(cat.availableCents))
                .monospacedDigit()
                .foregroundStyle(availableColor(cat.availableCents))
        }
        .contentShape(Rectangle())
    }

    private func availableColor(_ cents: Int64) -> Color {
        if cents > 0 { return .green }
        if cents < 0 { return .red }
        return .secondary
    }

    // MARK: - Editing

    private var showingEditor: Binding<Bool> {
        Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })
    }

    // Surface errors as an alert only once a budget is on screen (assign or
    // month-nav failures). The initial-load failure keeps its retry screen.
    private var showingError: Binding<Bool> {
        Binding(get: { error != nil && budget != nil }, set: { if !$0 { error = nil } })
    }

    private func beginEdit(_ cat: BudgetCategory) {
        editing = cat
        editAmount = Money.plain(cat.assignedCents)
    }

    private func save(categoryId: Int64, amount: String) async {
        guard let month = budget?.month else { return }
        do {
            budget = try await session.assign(categoryId: categoryId, month: month, amount: amount)
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't save the assignment."
        }
    }

    // MARK: - Data

    private func load() async {
        loading = true
        error = nil
        do {
            budget = try await session.loadBudget(month: month)
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't load the budget."
        }
        loading = false
    }

    // monthLabel turns "2026-07" into "July 2026".
    private func monthLabel(_ key: String) -> String {
        let parser = DateFormatter()
        parser.dateFormat = "yyyy-MM"
        guard let date = parser.date(from: key) else { return key }
        let out = DateFormatter()
        out.dateFormat = "MMMM yyyy"
        return out.string(from: date)
    }
}
