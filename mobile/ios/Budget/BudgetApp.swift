import SwiftUI

@main
struct BudgetApp: App {
    @StateObject private var session = Session(api: APIClient(baseURL: Self.baseURL))

    // Dev builds talk to the local server (Simulator shares the Mac's localhost);
    // release builds talk to production.
    private static var baseURL: URL {
        #if DEBUG
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
