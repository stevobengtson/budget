import SwiftUI

struct AccountTransactionsView: View {
    @EnvironmentObject private var session: Session
    let account: Account

    @State private var transactions: [TransactionItem]?
    @State private var loading = false
    @State private var error: String?
    @State private var showingAdd = false

    var body: some View {
        Group {
            if let transactions {
                if transactions.isEmpty {
                    Text("No transactions")
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(transactions) { transactionRow($0) }
                }
            } else if loading {
                ProgressView()
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
        .task { await load() }
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

    // subtitle shows "Category · Jul 15", dropping an empty category.
    private func subtitle(_ tx: TransactionItem) -> String {
        let date = Self.shortDate(tx.date)
        return tx.category.isEmpty ? date : "\(tx.category) · \(date)"
    }

    private func load() async {
        loading = true
        error = nil
        do {
            transactions = try await session.loadTransactions(accountId: account.id)
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't load transactions."
        }
        loading = false
    }

    // shortDate turns "2026-07-15" into "Jul 15".
    private static let isoFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()
    private static let displayFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMM d"
        return f
    }()
    private static func shortDate(_ s: String) -> String {
        guard let d = isoFormatter.date(from: s) else { return s }
        return displayFormatter.string(from: d)
    }
}
