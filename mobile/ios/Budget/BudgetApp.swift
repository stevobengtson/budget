import SwiftUI

@main
struct BudgetApp: App {
    @StateObject private var session = Session(
        api: APIClient(baseURL: URL(string: "http://localhost:8080")!)
    )

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(session)
                .task { await session.bootstrap() }
        }
    }
}
