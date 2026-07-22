import Foundation

// Session is the app's auth state. It owns the token, drives which screen shows
// (loading / login / signed-in), and is injected as an @EnvironmentObject.
@MainActor
final class Session: ObservableObject {
    enum State: Equatable {
        case loading
        case signedOut
        case signedIn(User)
    }

    @Published private(set) var state: State = .loading
    @Published private(set) var addOns: [String] = []
    @Published var loginError: String?

    private let api: APIClient
    private var token: String?

    // hasAddOn reports whether the signed-in user has an add-on enabled (e.g.
    // "paydown"), used to show optional tabs.
    func hasAddOn(_ slug: String) -> Bool { addOns.contains(slug) }

    // loadBudget fetches the budget for a month (nil = current) using the stored
    // token, keeping the token itself encapsulated here.
    func loadBudget(month: String?) async throws -> BudgetResponse {
        guard let token else { throw APIError(code: "unauthorized", message: "Not signed in.") }
        return try await api.budget(month: month, token: token)
    }

    func assign(categoryId: Int64, month: String, amount: String) async throws -> BudgetResponse {
        guard let token else { throw APIError(code: "unauthorized", message: "Not signed in.") }
        return try await api.assign(categoryId: categoryId, month: month, amount: amount, token: token)
    }

    init(api: APIClient) {
        self.api = api
    }

    // bootstrap restores a saved session on launch: if a stored token still
    // authenticates, go straight to the signed-in state; otherwise sign out.
    func bootstrap() async {
        guard let saved = KeychainToken.load() else {
            state = .signedOut
            return
        }
        do {
            let me = try await api.me(token: saved)
            token = saved
            addOns = me.addOns
            state = .signedIn(me.user)
        } catch {
            KeychainToken.clear()
            state = .signedOut
        }
    }

    func signIn(email: String, password: String) async {
        loginError = nil
        do {
            let auth = try await api.login(email: email, password: password)
            KeychainToken.save(auth.token)
            token = auth.token
            addOns = auth.addOns
            state = .signedIn(auth.user)
        } catch let error as APIError {
            loginError = error.message
        } catch {
            loginError = "Something went wrong."
        }
    }

    func signOut() async {
        if let token { await api.logout(token: token) }
        KeychainToken.clear()
        token = nil
        addOns = []
        state = .signedOut
    }
}
