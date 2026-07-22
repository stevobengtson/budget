import SwiftUI

@main
struct BudgetApp: App {
    @StateObject private var session = Session(api: APIClient(baseURL: Self.baseURL))

    // The Simulator shares the Mac's localhost, so it targets the local dev
    // server; a real device (and any archive/TestFlight build) targets production.
    private static var baseURL: URL {
        #if targetEnvironment(simulator)
        URL(string: "http://localhost:8080")!
        #else
        URL(string: "https://pigglet.ca")!
        #endif
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(session)
                .task { await session.bootstrap() }
        }
    }
}
