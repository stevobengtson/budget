import SwiftUI

struct AccountTransactionsView: View {
    @EnvironmentObject private var session: Session
    let account: Account

    @State private var page: TransactionsPage?
    @State private var month: String?          // nil = current month
    @State private var loading = false
    @State private var error: String?
    @State private var showingAdd = false

    var body: some View {
        VStack(spacing: 0) {
            if let page {
                monthHeader(page)
            }
            content
        }
        .navigationTitle(account.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { showingAdd = true } label: { Image(systemName: "plus") }
            }
        }
        .sheet(isPresented: $showingAdd) {
            AddTransactionView(account: account) {
                Task { await load() }
            }
        }
        .task(id: month) { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if let page {
            List {
                if page.transactions.isEmpty {
                    Text("No transactions this month").foregroundStyle(.secondary)
                } else {
                    ForEach(page.transactions) { transactionRow($0) }
                }
            }
            .listStyle(.plain)
            .refreshable { await load() }
        } else if loading {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let error {
            VStack(spacing: 12) {
                Text(error).foregroundStyle(.secondary)
                Button("Retry") { Task { await load() } }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            Color.clear
        }
    }

    private func monthHeader(_ page: TransactionsPage) -> some View {
        HStack {
            Button { month = page.prevMonth } label: { Image(systemName: "chevron.left") }
            Spacer()
            Text(Self.monthLabel(page.month)).font(.headline)
            Spacer()
            Button { month = page.nextMonth } label: { Image(systemName: "chevron.right") }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    private func transactionRow(_ tx: TransactionItem) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(tx.payee.isEmpty ? (tx.category.isEmpty ? "—" : tx.category) : tx.payee)
                Text(subtitle(tx))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Text(Money.format(tx.amountCents))
                .monospacedDigit()
                .foregroundStyle(tx.amountCents > 0 ? .green : .primary)
        }
    }

    private func subtitle(_ tx: TransactionItem) -> String {
        let date = Self.shortDate(tx.date)
        return tx.category.isEmpty ? date : "\(tx.category) · \(date)"
    }

    private func load() async {
        loading = true
        error = nil
        do {
            page = try await session.loadTransactions(accountId: account.id, month: month)
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't load transactions."
        }
        loading = false
    }

    // MARK: - Date formatting

    private static let isoFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()
    private static let dayFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMM d"
        return f
    }()
    private static let monthKeyFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM"
        return f
    }()
    private static let monthNameFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMMM yyyy"
        return f
    }()

    private static func shortDate(_ s: String) -> String {
        guard let d = isoFormatter.date(from: s) else { return s }
        return dayFormatter.string(from: d)
    }
    private static func monthLabel(_ key: String) -> String {
        guard let d = monthKeyFormatter.date(from: key) else { return key }
        return monthNameFormatter.string(from: d)
    }
}
