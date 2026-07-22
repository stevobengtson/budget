import SwiftUI

struct AccountsView: View {
    @EnvironmentObject private var session: Session
    @State private var accounts: [Account]?
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        // Keep the NavigationStack for the drill-in to transactions, but hide its
        // bar on the list so a compact custom header matches the other tabs. The
        // pushed transactions screen keeps its own inline nav bar (back + title).
        NavigationStack {
            VStack(spacing: 0) {
                HStack {
                    Text("Accounts").font(.largeTitle.bold())
                    Spacer()
                }
                .padding(.horizontal)
                .padding(.top, 8)

                Group {
                    if let accounts {
                        List(accounts) { account in
                            NavigationLink {
                                AccountTransactionsView(account: account)
                            } label: {
                                accountRow(account)
                            }
                        }
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
            }
            .toolbar(.hidden, for: .navigationBar)
        }
        .task { await load() }
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
                .foregroundStyle(account.balanceCents < 0 ? .red : .primary)
        }
    }

    private func load() async {
        loading = true
        error = nil
        do {
            accounts = try await session.loadAccounts()
        } catch let apiError as APIError {
            self.error = apiError.message
        } catch {
            self.error = "Couldn't load accounts."
        }
        loading = false
    }
}
