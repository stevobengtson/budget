import SwiftUI

// The signed-in shell: a tab bar mirroring the web's sections. Transactions are
// reached by drilling into an account (Accounts tab), matching the web's
// account-scoped model. Paydown shows only when its add-on is enabled.
struct MainTabView: View {
    @EnvironmentObject private var session: Session
    let user: User

    var body: some View {
        TabView {
            BudgetView()
                .tabItem { Label("Budget", systemImage: "banknote") }

            AccountsView()
                .tabItem { Label("Accounts", systemImage: "building.columns") }

//            if session.hasAddOn("paydown") {
//                PaydownView()
//                    .tabItem { Label("Paydown", systemImage: "chart.line.downtrend.xyaxis") }
//            }
//
            SettingsView(user: user)
                .tabItem { Label("Settings", systemImage: "gearshape") }
        }
    }
}
