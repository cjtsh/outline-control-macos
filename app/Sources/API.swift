import Foundation

/// Any failure the provider can report, with the text we want to show.
struct APIError: Error {
    let text: String
    init(_ text: String) { self.text = text }
}

// MARK: - Wire types

struct ServerView: Decodable, Equatable {
    let id: String
    let name: String
    let host: String
    let port: String
    let method: String

    var address: String { "\(host):\(port)" }
}

struct TunnelStatus: Decodable, Equatable {
    let state: String
    let serverId: String?
    let serverName: String?
    let since: String?
    let lastError: String?
    let mode: String?
    let socks: String
    let backend: String?
    let pid: Int?

    static let empty = TunnelStatus(state: "disconnected", serverId: nil, serverName: nil,
                                    since: nil, lastError: nil, mode: "socks", socks: "",
                                    backend: nil, pid: nil)

    /// "tunnel" means every packet on this Mac takes the exit; "socks" means
    /// only apps pointed at the local proxy do.
    var isFullTunnel: Bool { mode == "tunnel" }

    var isConnected: Bool { state == "connected" }

    var sinceDate: Date? {
        guard let since else { return nil }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: since) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: since)
    }

    var connectedFor: String? {
        guard isConnected, let sinceDate else { return nil }
        let seconds = max(0, Int(Date().timeIntervalSince(sinceDate)))
        if seconds < 60 { return "\(seconds)s" }
        if seconds < 3600 { return "\(seconds / 60)m" }
        let h = seconds / 3600, m = (seconds % 3600) / 60
        return "\(h)h \(m)m"
    }
}

struct HealthView: Decodable, Equatable {
    let id: String
    let reachable: Bool
    let error: String?
}

private struct ServersEnvelope: Decodable { let servers: [ServerView] }
private struct ExitsEnvelope: Decodable { let exits: [HealthView] }
private struct StatusEnvelope: Decodable { let tunnel: TunnelStatus }
private struct ErrorEnvelope: Decodable { let error: String }

// MARK: - Client

/// Thin client for the provider running on 127.0.0.1. The GUI owns no state:
/// every card on screen is a rendering of what the daemon says.
final class OutlineAPI {
    static let port = 9911

    private let base = URL(string: "http://127.0.0.1:\(OutlineAPI.port)")!
    private let token: String
    private let queue = DispatchQueue(label: "sh.cjt.outline-control.api", qos: .userInitiated)

    init(token: String) { self.token = token }

    static func dataDir() -> URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/Outline Control", isDirectory: true)
    }

    static func loadToken() -> String? {
        let path = dataDir().appendingPathComponent("token")
        guard let raw = try? String(contentsOf: path, encoding: .utf8) else { return nil }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    // MARK: transport

    private func send(_ method: String, _ path: String, body: [String: Any]?,
                      completion: @escaping (Result<Data, APIError>) -> Void) {
        guard let url = URL(string: path, relativeTo: base) else {
            completion(.failure(APIError("bad request path")))
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = 8
        req.setValue(token, forHTTPHeaderField: "X-Outline-Token")
        if let body {
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        queue.async {
            URLSession.shared.dataTask(with: req) { data, response, error in
                if let error {
                    completion(.failure(APIError(Self.explain(error))))
                    return
                }
                let code = (response as? HTTPURLResponse)?.statusCode ?? 0
                let payload = data ?? Data()
                if (200..<300).contains(code) {
                    completion(.success(payload))
                    return
                }
                let message = (try? JSONDecoder().decode(ErrorEnvelope.self, from: payload))?.error
                completion(.failure(APIError(message ?? "the provider answered HTTP \(code)")))
            }.resume()
        }
    }

    private func decode<T: Decodable>(_ method: String, _ path: String, body: [String: Any]? = nil,
                                      as type: T.Type, completion: @escaping (Result<T, APIError>) -> Void) {
        send(method, path, body: body) { result in
            switch result {
            case .failure(let message):
                DispatchQueue.main.async { completion(.failure(message)) }
            case .success(let data):
                self.queue.async {
                    do {
                        let value = try JSONDecoder().decode(T.self, from: data)
                        DispatchQueue.main.async { completion(.success(value)) }
                    } catch {
                        DispatchQueue.main.async { completion(.failure(APIError("unexpected reply from the provider"))) }
                    }
                }
            }
        }
    }

    private static func explain(_ error: Error) -> String {
        let ns = error as NSError
        if ns.domain == NSURLErrorDomain && ns.code == NSURLErrorCannotConnectToHost {
            return "The provider is not running."
        }
        if ns.domain == NSURLErrorDomain && ns.code == NSURLErrorNetworkConnectionLost {
            return "The provider stopped answering."
        }
        return error.localizedDescription
    }

    // MARK: endpoints

    func servers(_ completion: @escaping (Result<[ServerView], APIError>) -> Void) {
        decode("GET", "/servers", as: ServersEnvelope.self) { completion($0.map { $0.servers }) }
    }

    func status(_ completion: @escaping (Result<TunnelStatus, APIError>) -> Void) {
        decode("GET", "/status", as: StatusEnvelope.self) { completion($0.map { $0.tunnel }) }
    }

    func health(_ completion: @escaping (Result<[HealthView], APIError>) -> Void) {
        decode("GET", "/health", as: ExitsEnvelope.self) { completion($0.map { $0.exits }) }
    }

    func add(key: String, name: String?, _ completion: @escaping (Result<ServerView, APIError>) -> Void) {
        var body: [String: Any] = ["key": key]
        if let name, !name.isEmpty { body["name"] = name }
        decode("POST", "/servers", body: body, as: ServerView.self, completion: completion)
    }

    func rename(_ id: String, to name: String, _ completion: @escaping (Result<ServerView, APIError>) -> Void) {
        decode("PATCH", "/servers/\(id)", body: ["name": name], as: ServerView.self, completion: completion)
    }

    func forget(_ id: String, _ completion: @escaping (Result<Void, APIError>) -> Void) {
        send("DELETE", "/servers/\(id)", body: nil) { result in
            DispatchQueue.main.async {
                switch result {
                case .success: completion(.success(()))
                case .failure(let message): completion(.failure(message))
                }
            }
        }
    }

    func connect(_ id: String, _ completion: @escaping (Result<TunnelStatus, APIError>) -> Void) {
        decode("POST", "/connect", body: ["id": id], as: TunnelStatus.self, completion: completion)
    }

    func disconnect(_ completion: @escaping (Result<TunnelStatus, APIError>) -> Void) {
        decode("POST", "/disconnect", as: TunnelStatus.self, completion: completion)
    }
}
