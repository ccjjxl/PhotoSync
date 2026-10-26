import Foundation
import Network

/// A minimal HTTP/1.1 server built on Network.framework.
///
/// There is no built-in HTTP stack in Network.framework, and the desktop client
/// only needs a small subset, so this is intentionally byte-level:
/// GET/HEAD/POST, keep-alive, Range (resume), ETag, Content-Length.
/// Not supported (and not needed): chunked encoding, HTTP/2, TLS.
final class HttpServer: @unchecked Sendable {

    // MARK: - Request / Response

    struct Request {
        let method: String
        let path: String
        let query: [String: String]
        let headers: [String: String]
        let body: Data?

        var wantsKeepAlive: Bool {
            !(headers["connection"]?.lowercased().contains("close") ?? false)
        }

        func q(_ name: String) -> String? { query[name] }
        func qInt(_ name: String, _ fallback: Int = 0) -> Int { Int(query[name] ?? "") ?? fallback }
        func qBool(_ name: String, _ fallback: Bool = false) -> Bool {
            switch query[name]?.lowercased() {
            case "1", "true", "yes": return true
            case "0", "false", "no": return false
            default: return fallback
            }
        }
    }

    enum Body {
        case empty
        case bytes(Data)
        case file(URL)
    }

    struct Response {
        var status: Int
        var headers: [String: String]
        var body: Body

        static func text(_ status: Int, _ text: String,
                         contentType: String = "text/plain; charset=utf-8") -> Response {
            Response(status: status,
                     headers: ["Content-Type": contentType],
                     body: .bytes(Data(text.utf8)))
        }

        static func json(_ status: Int, _ json: String) -> Response {
            Response(status: status,
                     headers: ["Content-Type": "application/json; charset=utf-8"],
                     body: .bytes(Data(json.utf8)))
        }

        static func data(_ data: Data, contentType: String, cacheSeconds: Int = 0) -> Response {
            var headers = ["Content-Type": contentType]
            if cacheSeconds > 0 { headers["Cache-Control"] = "public, max-age=\(cacheSeconds)" }
            return Response(status: 200, headers: headers, body: .bytes(data))
        }

        static func file(_ url: URL, contentType: String, etag: String? = nil) -> Response {
            var headers = ["Content-Type": contentType]
            if let etag { headers["ETag"] = etag }
            return Response(status: 200, headers: headers, body: .file(url))
        }

        static func empty(_ status: Int) -> Response {
            Response(status: status, headers: [:], body: .empty)
        }
    }

    typealias Handler = (Request) -> Response

    // MARK: - State

    private let port: UInt16
    private let handler: Handler
    private let queue = DispatchQueue(label: "dev.photosync.http", qos: .userInitiated,
                                      attributes: .concurrent)
    private var listener: NWListener?

    private(set) var isRunning = false
    /// Called on the main queue when start/stop succeeds or fails.
    var onStateChange: ((Bool, String?) -> Void)?

    /// Live connections, so stop() can close them instead of leaving keep-alive
    /// sockets holding the port.
    private var connections: [NWConnection] = []
    private let connectionLock = NSLock()

    /// Identifies the current listener generation. stop() cancels the listener and
    /// bumps this, so late `.cancelled` callbacks from an old listener cannot
    /// overwrite the state of a freshly started one.
    private var generation: UInt64 = 0

    /// How many times a still-busy port may be retried before reporting failure.
    private var retriesLeft = 5

    /// `NWListener` surfaces EADDRINUSE through NSPOSIXErrorDomain.
    private static func looksLikeAddressInUse(_ error: NWError) -> Bool {
        if case .posix(let code) = error, code == .EADDRINUSE { return true }
        return false
    }

    init(port: UInt16, handler: @escaping Handler) {
        self.port = port
        self.handler = handler
    }

    // MARK: - Lifecycle

    func start() {
        guard !isRunning else { return }
        generation &+= 1
        let currentGeneration = generation

        do {
            let parameters = NWParameters.tcp
            parameters.allowLocalEndpointReuse = true
            parameters.includePeerToPeer = true

            guard let nwPort = NWEndpoint.Port(rawValue: port) else {
                notifyMain(false, "端口 \(port) 无效")
                return
            }
            let listener = try NWListener(using: parameters, on: nwPort)
            listener.newConnectionHandler = { [weak self] connection in
                self?.accept(connection)
            }
            listener.stateUpdateHandler = { [weak self] state in
                guard let self else { return }
                // Ignore events from a listener that has already been replaced:
                // its .cancelled arrives after the new listener may be .ready.
                guard self.generation == currentGeneration else { return }
                switch state {
                case .ready:
                    self.retriesLeft = 5
                    self.isRunning = true
                    self.notifyMain(true, nil)
                case .failed(let error):
                    self.isRunning = false
                    // A port that is still being released by the previous
                    // listener looks like "address in use" for a few hundred
                    // milliseconds. Retry instead of failing permanently.
                    if self.retriesLeft > 0, Self.looksLikeAddressInUse(error) {
                        self.retriesLeft -= 1
                        self.listener?.cancel()
                        self.listener = nil
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
                            self?.start()
                        }
                        return
                    }
                    self.notifyMain(false, "端口 \(self.port) 启动失败：\(error.localizedDescription)")
                case .cancelled:
                    self.isRunning = false
                default:
                    break
                }
            }
            listener.start(queue: queue)
            self.listener = listener
        } catch {
            isRunning = false
            notifyMain(false, "端口 \(port) 已被占用")
        }
    }

    func stop() {
        retriesLeft = 5           // a user-initiated stop starts a fresh budget
        generation &+= 1          // invalidate pending callbacks from this listener
        listener?.cancel()
        listener = nil

        connectionLock.lock()
        let live = connections
        connections.removeAll()
        connectionLock.unlock()
        for connection in live {
            connection.cancel()
        }

        isRunning = false
    }

    private func notifyMain(_ running: Bool, _ error: String?) {
        DispatchQueue.main.async { [weak self] in
            self?.onStateChange?(running, error)
        }
    }

    // MARK: - Connection

    private func accept(_ connection: NWConnection) {
        connectionLock.lock()
        connections.append(connection)
        connectionLock.unlock()

        connection.start(queue: queue)
        receive(on: connection, buffer: Data())
    }

    private func forget(_ connection: NWConnection) {
        connectionLock.lock()
        connections.removeAll { $0 === connection }
        connectionLock.unlock()
    }

    private func receive(on connection: NWConnection, buffer: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) {
            [weak self] chunk, _, isComplete, error in
            guard let self else { return }
            if error != nil {
                self.forget(connection)
                connection.cancel()
                return
            }

            var accumulated = buffer
            if let chunk { accumulated.append(chunk) }

            guard let headerEnd = accumulated.range(of: Data("\r\n\r\n".utf8)) else {
                if isComplete || accumulated.count > 128 * 1024 {
                    self.forget(connection)
                    connection.cancel()
                    return
                }
                self.receive(on: connection, buffer: accumulated)
                return
            }

            let headerData = Data(accumulated[..<headerEnd.lowerBound])
            guard let request = Self.parseRequest(headerData: headerData,
                                                  fullBuffer: accumulated,
                                                  headerEnd: headerEnd) else {
                self.write(.text(400, "bad request\n"), to: connection) {}
                return
            }

            let response = self.handler(request)
            let keepAlive = request.wantsKeepAlive
            self.write(response, rangeHeader: request.headers["range"], to: connection) {
                if keepAlive {
                    self.receive(on: connection, buffer: Data())
                } else {
                    self.forget(connection)
                    connection.cancel()
                }
            }
        }
    }

    private static func parseRequest(headerData: Data, fullBuffer: Data,
                                     headerEnd: Range<Data.Index>) -> Request? {
        guard let headerText = String(data: headerData, encoding: .utf8) else { return nil }
        var lines = headerText.components(separatedBy: "\r\n")
        guard !lines.isEmpty else { return nil }

        let parts = lines.removeFirst().split(separator: " ")
        guard parts.count >= 3 else { return nil }
        let method = String(parts[0]).uppercased()
        let target = String(parts[1])

        var headers: [String: String] = [:]
        for line in lines {
            guard let colon = line.firstIndex(of: ":") else { continue }
            let name = line[line.startIndex..<colon]
                .trimmingCharacters(in: .whitespaces).lowercased()
            let value = line[line.index(after: colon)...]
                .trimmingCharacters(in: .whitespaces)
            headers[name] = value
        }

        let path: String
        let query: [String: String]
        if let qIndex = target.firstIndex(of: "?") {
            path = String(target[target.startIndex..<qIndex]).removingPercentEncoding ?? ""
            query = parseQuery(String(target[target.index(after: qIndex)...]))
        } else {
            path = target.removingPercentEncoding ?? target
            query = [:]
        }

        var body: Data?
        if let lengthText = headers["content-length"], let length = Int(lengthText), length > 0 {
            let bodyStart = headerEnd.upperBound
            let available = fullBuffer.distance(from: bodyStart, to: fullBuffer.endIndex)
            let take = min(length, available)
            if take > 0 {
                body = fullBuffer.subdata(in: bodyStart..<fullBuffer.index(bodyStart, offsetBy: take))
            }
        }

        return Request(method: method, path: path, query: query, headers: headers, body: body)
    }

    private static func parseQuery(_ raw: String) -> [String: String] {
        var out: [String: String] = [:]
        for pair in raw.split(separator: "&") {
            let kv = pair.split(separator: "=", maxSplits: 1)
            let key = String(kv[0]).removingPercentEncoding ?? String(kv[0])
            let value: String
            if kv.count > 1 {
                let rawValue = String(kv[1]).replacingOccurrences(of: "+", with: " ")
                value = rawValue.removingPercentEncoding ?? rawValue
            } else {
                value = ""
            }
            out[key] = value
        }
        return out
    }

    // MARK: - Response

    /// Parsed `Range: bytes=…` header.
    private enum RangeSpec {
        case satisfiable(start: Int64, length: Int64)
        case unsatisfiable
    }

    private static func parseRange(_ header: String, size: Int64) -> RangeSpec? {
        guard header.lowercased().hasPrefix("bytes=") else { return nil }
        let spec = header.dropFirst("bytes=".count).split(separator: ",").first.map(String.init) ?? ""
        let bounds = spec.split(separator: "-", maxSplits: 1, omittingEmptySubsequences: false)
        guard bounds.count == 2 else { return nil }

        let startText = bounds[0].trimmingCharacters(in: .whitespaces)
        let endText = bounds[1].trimmingCharacters(in: .whitespaces)

        if startText.isEmpty {
            // Suffix form: "bytes=-500" means the last 500 bytes.
            guard let suffix = Int64(endText), suffix > 0 else { return .unsatisfiable }
            let start = max(0, size - suffix)
            return .satisfiable(start: start, length: size - start)
        }
        guard let start = Int64(startText), start < size else { return .unsatisfiable }
        let end = endText.isEmpty ? size - 1 : min(Int64(endText) ?? (size - 1), size - 1)
        guard end >= start else { return .unsatisfiable }
        return .satisfiable(start: start, length: end - start + 1)
    }

    private func write(_ response: Response, rangeHeader: String? = nil,
                       to connection: NWConnection,
                       completion: @escaping () -> Void) {
        switch response.body {
        case .empty:
            var head = statusLine(response.status)
            head += headerLines(response.headers, length: 0, extra: nil)
            connection.send(content: Data(head.utf8), completion: .contentProcessed { _ in
                completion()
            })

        case .bytes(let data):
            var head = statusLine(response.status)
            head += headerLines(response.headers, length: Int64(data.count), extra: nil)
            var payload = Data(head.utf8)
            payload.append(data)
            connection.send(content: payload, completion: .contentProcessed { _ in
                completion()
            })

        case .file(let url):
            writeFile(url, response: response, rangeHeader: rangeHeader,
                      to: connection, completion: completion)
        }
    }

    private func writeFile(_ url: URL, response: Response, rangeHeader: String?,
                           to connection: NWConnection,
                           completion: @escaping () -> Void) {
        guard let handle = try? FileHandle(forReadingFrom: url) else {
            write(.text(404, "not found\n"), to: connection, completion: completion)
            return
        }
        let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
        let size = (attributes?[.size] as? NSNumber)?.int64Value ?? 0

        var status = response.status
        var start: Int64 = 0
        var length = size
        var extra: [(String, String)] = [("Accept-Ranges", "bytes")]

        if let rangeHeader {
            switch Self.parseRange(rangeHeader, size: size) {
            case .satisfiable(let from, let count):
                start = from
                length = count
                status = 206
                extra.append(("Content-Range", "bytes \(from)-\(from + count - 1)/\(size)"))
            case .unsatisfiable:
                let body = Data("range not satisfiable\n".utf8)
                var head = statusLine(416)
                head += "Content-Range: bytes */\(size)\r\n"
                head += "Content-Type: text/plain; charset=utf-8\r\n"
                head += "Content-Length: \(body.count)\r\n"
                head += "Connection: close\r\n\r\n"
                var payload = Data(head.utf8)
                payload.append(body)
                connection.send(content: payload, completion: .contentProcessed { _ in
                    try? handle.close()
                    connection.cancel()
                })
                return
            case .none:
                break
            }
        }

        try? handle.seek(toOffset: UInt64(start))
        var head = statusLine(status)
        head += headerLines(response.headers, length: length, extra: extra)
        connection.send(content: Data(head.utf8), completion: .contentProcessed { _ in
            self.streamFile(handle: handle, remaining: length, on: connection) {
                try? handle.close()
                completion()
            }
        })
    }

    private func streamFile(handle: FileHandle, remaining: Int64, on connection: NWConnection,
                            completion: @escaping () -> Void) {
        guard remaining > 0 else {
            completion()
            return
        }
        let chunkSize = Int(min(Int64(256 * 1024), remaining))
        guard let chunk = try? handle.read(upToCount: chunkSize), !chunk.isEmpty else {
            completion()
            return
        }
        connection.send(content: chunk, completion: .contentProcessed { error in
            if error != nil {
                completion()
                return
            }
            self.streamFile(handle: handle, remaining: remaining - Int64(chunk.count),
                            on: connection, completion: completion)
        })
    }

    private func statusLine(_ status: Int) -> String {
        "HTTP/1.1 \(status) \(Self.reason(status))\r\n"
    }

    private func headerLines(_ headers: [String: String], length: Int64,
                             extra: [(String, String)]?) -> String {
        var out = ""
        for (key, value) in headers {
            out += "\(key): \(value)\r\n"
        }
        for (key, value) in extra ?? [] {
            out += "\(key): \(value)\r\n"
        }
        out += "Server: PhotoSync-iOS/1.0\r\n"
        out += "Connection: keep-alive\r\n"
        out += "Content-Length: \(length)\r\n\r\n"
        return out
    }

    private static func reason(_ status: Int) -> String {
        switch status {
        case 200: return "OK"
        case 206: return "Partial Content"
        case 304: return "Not Modified"
        case 400: return "Bad Request"
        case 401: return "Unauthorized"
        case 403: return "Forbidden"
        case 404: return "Not Found"
        case 405: return "Method Not Allowed"
        case 416: return "Range Not Satisfiable"
        case 500: return "Internal Server Error"
        default: return "OK"
        }
    }
}
