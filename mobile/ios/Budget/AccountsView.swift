import SwiftUI

struct AccountsView: View {
    @EnvironmentObject private var session: Session
    @State private var page: AccountsPage?
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                HStack {
                    Text("Accounts").font(.largeTitle.bold())
                    Spacer()
                }
                .padding(.horizontal)
                .padding(.top, 8)

                if let page {
                    monthHeader(page)
                }

                content
            }
            .background(Color.appBackground.ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
        }
        .task(id: session.selectedMonth) { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if let page {
            List(page.accounts) { account in
                NavigationLink {
                    AccountTransactionsView(account: account)
                } label: {
                    accountRow(account)
                }
                .listRowBackground(Color.appCard)
            }
            .scrollContentBackground(.hidden)
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

    private func monthHeader(_ page: AccountsPage) -> some View {
        HStack {
            Button { session.selectedMonth = page.prevMonth } label: { Image(systemName: "chevron.left") }
            Spacer()
            Text(Self.monthLabel(page.month)).font(.headline)
            Spacer()
            Button { session.selectedMonth = page.nextMonth } label: { Image(systemName: "chevron.right") }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    private func accountRow(_ account: Account) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(account.name)
                Text(account.type.capitalized)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Text(Money.format(account.balanceCents))
                .monospacedDigit()
                .foregroundStyle(account.balanceCents < 0 ? Color.moneyNegative : .primary)
        }
    }

    private func load() async {
        loading = true
        error = nil
        do {
            page = try await session.loadAccounts(month: session.selectedMonth)
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't load accounts."
        }
        loading = false
    }

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
    private static func monthLabel(_ key: String) -> String {
        guard let d = monthKeyFormatter.date(from: key) else { return key }
        return monthNameFormatter.string(from: d)
    }
}
