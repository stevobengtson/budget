import SwiftUI

struct AddTransactionView: View {
    @EnvironmentObject private var session: Session
    @Environment(\.dismiss) private var dismiss

    let account: Account
    // When set, the form edits this existing transaction instead of adding one.
    let editing: TransactionItem?
    let onSaved: () -> Void

    @State private var amountCents: Int64 = 0
    @State private var isExpense = true
    @State private var categoryId: Int64?
    @State private var payee = ""
    @State private var notes = ""
    @State private var date = Date()

    @State private var categories: [CategoryOption] = []
    @State private var saving = false
    @State private var error: String?

    init(account: Account, editing: TransactionItem? = nil, onSaved: @escaping () -> Void) {
        self.account = account
        self.editing = editing
        self.onSaved = onSaved
        if let tx = editing {
            // amountCents is inflow − outflow: negative = expense, positive = income.
            _amountCents = State(initialValue: abs(tx.amountCents))
            _isExpense = State(initialValue: tx.amountCents < 0)
            _categoryId = State(initialValue: tx.categoryId)
            _payee = State(initialValue: tx.payee)
            _notes = State(initialValue: tx.notes)
            if let d = Self.isoFormatter.date(from: tx.date) {
                _date = State(initialValue: d)
            }
        }
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Picker("Type", selection: $isExpense) {
                    Text("Expense").tag(true)
                    Text("Income").tag(false)
                }
                .pickerStyle(.segmented)

                Text(Money.format(amountCents))
                    .font(.system(size: 44, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(amountCents == 0 ? .secondary : .primary)
                    .frame(maxWidth: .infinity)

                metadata

                if let error {
                    Text(error).foregroundStyle(Color.appDestructive).font(.footnote)
                }

                Spacer(minLength: 0)

                AmountKeypad(onDigit: appendDigit, onBackspace: backspace)
            }
            .padding()
            .background(Color.appBackground.ignoresSafeArea())
            // Keep the form anchored when the OS keyboard slides in for the
            // payee/notes fields: without this, keyboard avoidance shoves the
            // whole card (tabs + amount) up under the title.
            .ignoresSafeArea(.keyboard, edges: .bottom)
            .navigationTitle(editing == nil ? "Add Transaction" : "Edit Transaction")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editing == nil ? "Add" : "Save") { Task { await save() } }
                        .disabled(saving || amountCents == 0)
                }
            }
            .task { await loadCategories() }
        }
    }

    private var metadata: some View {
        VStack(spacing: 12) {
            HStack {
                Text("Date")
                Spacer()
                DatePicker("", selection: $date, displayedComponents: .date).labelsHidden()
            }
            Divider()
            HStack {
                Text("Category")
                Spacer()
                Menu {
                    Button("None") { categoryId = nil }
                    ForEach(categories) { category in
                        Button(category.name) { categoryId = category.id }
                    }
                } label: {
                    Text(selectedCategoryName).foregroundStyle(.secondary)
                }
            }
            Divider()
            TextField("Payee", text: $payee)
            Divider()
            TextField("Notes", text: $notes)
        }
        .padding()
        .background(RoundedRectangle(cornerRadius: 12).fill(Color.appCard))
    }

    private var selectedCategoryName: String {
        categories.first { $0.id == categoryId }?.name ?? "None"
    }

    // Cents accumulator, driven by the on-screen keypad: each digit shifts in
    // from the right; backspace drops the last one.
    private func appendDigit(_ n: Int) {
        guard amountCents < 100_000_000 else { return } // cap absurd input (~$1M)
        amountCents = amountCents * 10 + Int64(n)
    }

    private func backspace() {
        amountCents /= 10
    }

    private func loadCategories() async {
        categories = (try? await session.loadCategories()) ?? []
    }

    private func save() async {
        saving = true
        error = nil
        do {
            let type = isExpense ? "expense" : "income"
            let amount = Money.plain(amountCents)
            let isoDate = Self.isoFormatter.string(from: date)
            if let tx = editing {
                try await session.updateTransaction(
                    accountId: account.id, transactionId: tx.id, amount: amount, type: type,
                    categoryId: categoryId, payee: payee, notes: notes, date: isoDate
                )
            } else {
                try await session.createTransaction(
                    accountId: account.id, amount: amount, type: type,
                    categoryId: categoryId, payee: payee, notes: notes, date: isoDate
                )
            }
            onSaved()
            dismiss()
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't save the transaction."
        }
        saving = false
    }

    private static let isoFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()
}

// AmountKeypad is a numbers-only pad (calculator layout) plus a backspace, for
// entering an amount without the OS keyboard.
private struct AmountKeypad: View {
    let onDigit: (Int) -> Void
    let onBackspace: () -> Void

    private let rows: [[Int]] = [[7, 8, 9], [4, 5, 6], [1, 2, 3]]

    var body: some View {
        VStack(spacing: 8) {
            ForEach(rows, id: \.self) { row in
                HStack(spacing: 8) {
                    ForEach(row, id: \.self) { n in
                        digitKey(n)
                    }
                }
            }
            HStack(spacing: 8) {
                Color.clear.frame(maxWidth: .infinity)
                digitKey(0)
                Button(action: onBackspace) {
                    Image(systemName: "delete.left")
                        .font(.title2)
                        .frame(maxWidth: .infinity, minHeight: 56)
                }
                .buttonStyle(.bordered)
            }
        }
    }

    private func digitKey(_ n: Int) -> some View {
        Button { onDigit(n) } label: {
            Text("\(n)")
                .font(.title.weight(.medium))
                .frame(maxWidth: .infinity, minHeight: 56)
        }
        .buttonStyle(.bordered)
    }
}
