import Foundation

// Models mirror the JSON contracts served by internal/web/apiv1.

struct User: Codable, Equatable {
    let id: Int64
    let email: String
    let name: String
}

struct AuthResponse: Codable {
    let token: String
    let user: User
    let subscribed: Bool
    let addOns: [String]
}

struct MeResponse: Codable {
    let user: User
    let subscribed: Bool
    let addOns: [String]
}

// APIError carries the server's error envelope { error: { code, message } } or a
// transport failure, so the UI can show a message and branch on `code`.
struct APIError: Error, LocalizedError {
    let code: String
    let message: String
    var errorDescription: String? { message }

    static let network = APIError(code: "network", message: "Couldn't reach the server.")
}

struct APIClient {
    var baseURL: URL

    private struct ErrorEnvelope: Decodable {
        struct Detail: Decodable { let code: String; let message: String }
        let error: Detail
    }

    func login(email: String, password: String) async throws -> AuthResponse {
        var req = request(path: "api/v1/login", method: "POST", token: nil)
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["email": email, "password": password])
        return try decode(await send(req))
    }

    func me(token: String) async throws -> MeResponse {
        try decode(await send(request(path: "api/v1/me", method: "GET", token: token)))
    }

    func budget(month: String?, token: String) async throws -> BudgetResponse {
        var comps = URLComponents(
            url: baseURL.appendingPathComponent("api/v1/budget"),
            resolvingAgainstBaseURL: false
        )!
        if let month { comps.queryItems = [URLQueryItem(name: "month", value: month)] }
        var req = URLRequest(url: comps.url!)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return try decode(await send(req))
    }

    func logout(token: String) async {
        _ = try? await send(request(path: "api/v1/logout", method: "POST", token: token))
    }

    // MARK: - Plumbing

    private func request(path: String, method: String, token: String?) -> URLRequest {
        var req = URLRequest(url: baseURL.appendingPathComponent(path))
        req.httpMethod = method
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return req
    }

    private func send(_ req: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let data: Data, response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: req)
        } catch {
            throw APIError.network
        }
        guard let http = response as? HTTPURLResponse else { throw APIError.network }
        return (data, http)
    }

    private func decode<T: Decodable>(_ result: (Data, HTTPURLResponse)) throws -> T {
        let (data, http) = result
        guard (200..<300).contains(http.statusCode) else {
            if let env = try? JSONDecoder().decode(ErrorEnvelope.self, from: data) {
                throw APIError(code: env.error.code, message: env.error.message)
            }
            throw APIError(code: "http_\(http.statusCode)", message: "Request failed (\(http.statusCode)).")
        }
        return try JSONDecoder().decode(T.self, from: data)
    }
}
