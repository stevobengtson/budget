import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var session: Session
    let user: User

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Settings").font(.largeTitle.bold())
                Spacer()
            }
            .padding(.horizontal)
            .padding(.top, 8)

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
        }
    }
}
