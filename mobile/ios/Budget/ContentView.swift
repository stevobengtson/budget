import SwiftUI

// Root view: shows the right screen for the current auth state.
struct ContentView: View {
    @EnvironmentObject private var session: Session

    var body: some View {
        switch session.state {
        case .loading:
            ProgressView()
        case .signedOut:
            LoginView()
        case .signedIn(let user):
            MainTabView(user: user)
        }
    }
}
