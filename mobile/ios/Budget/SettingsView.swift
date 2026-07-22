import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var session: Session
    let user: User

    var body: some View {
        NavigationStack {
            List {
                Section("Account") {
                    LabeledContent("Name", value: user.name)
                    LabeledContent("Email", value: user.email)
                }
                Section {
                    Button("Sign out", role: .destructive) {
                        Task { await session.signOut() }
                    }
                }
            }
            .navigationTitle("Settings")
        }
    }
}
