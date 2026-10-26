package dev.photosync.server

import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.net.InetSocketAddress
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.nio.channels.ServerSocketChannel
import java.nio.channels.SocketChannel
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.zip.GZIPOutputStream

/**
 * Tiny, dependency-free HTTP/1.1 server tuned for large sequential transfers.
 *
 * - one thread per connection (keep-alive), accept loop on its own thread
 * - file bodies are sent with [FileChannel.transferTo] (sendfile on supported kernels)
 * - supports Range / If-None-Match / Content-Encoding: gzip / Expect: 100-continue
 */
class HttpServer(
    private val port: Int,
    private val handler: (HttpRequest) -> HttpResponse,
) {
    private var serverChannel: ServerSocketChannel? = null
    private val running = AtomicBoolean(false)
    private val pool = Executors.newCachedThreadPool { r ->
        Thread(r, "photosync-http").apply { isDaemon = true }
    }
    private val connections = AtomicLong(0)

    val isRunning: Boolean get() = running.get()
    val activeConnections: Long get() = connections.get()

    fun start() {
        if (!running.compareAndSet(false, true)) return
        val ch = ServerSocketChannel.open()
        ch.setOption(java.net.StandardSocketOptions.SO_REUSEADDR, true)
        ch.bind(InetSocketAddress("0.0.0.0", port), 64)
        ch.configureBlocking(true)
        serverChannel = ch
        pool.execute {
            while (running.get()) {
                try {
                    val socket = ch.accept()
                    socket.configureBlocking(true)
                    connections.incrementAndGet()
                    pool.execute { serve(socket) }
                } catch (_: Throwable) {
                    if (!running.get()) break
                }
            }
        }
    }

    fun stop() {
        if (!running.compareAndSet(true, false)) return
        runCatching { serverChannel?.close() }
        serverChannel = null
    }

    private fun serve(socket: SocketChannel) {
        try {
            socket.setOption(java.net.StandardSocketOptions.TCP_NODELAY, true)
            socket.socket().soTimeout = 60_000
            val input = socket.socket().getInputStream()
            val output = socket.socket().getOutputStream()

            while (running.get()) {
                val req = readRequest(input) ?: return
                if (req.expectContinue) {
                    output.write("HTTP/1.1 100 Continue\r\n\r\n".toByteArray())
                    output.flush()
                }
                val res = try {
                    handler(req)
                } catch (t: Throwable) {
                    HttpResponse.text(500, "internal error: ${t.message}\n")
                }
                val keepAlive = writeResponse(socket, output, req, res)
                if (!keepAlive) return
            }
        } catch (_: Throwable) {
            // client went away
        } finally {
            connections.decrementAndGet()
            runCatching { socket.close() }
        }
    }

    // ---------------------------------------------------------------- request

    private fun readRequest(input: InputStream): HttpRequest? {
        val line = readLine(input, 8192) ?: return null
        if (line.isEmpty()) return null
        val parts = line.split(' ')
        if (parts.size < 3) return null
        val method = parts[0].uppercase()
        val rawTarget = parts[1]

        val headers = HashMap<String, String>(16)
        var total = 0
        while (true) {
            val h = readLine(input, 8192) ?: return null
            if (h.isEmpty()) break
            total += h.length
            if (total > 64 * 1024) return null
            val idx = h.indexOf(':')
            if (idx <= 0) continue
            val name = h.substring(0, idx).trim().lowercase()
            val value = h.substring(idx + 1).trim()
            headers[name] = if (headers.containsKey(name)) headers[name] + "," + value else value
        }

        val qIdx = rawTarget.indexOf('?')
        val path = decode((if (qIdx >= 0) rawTarget.substring(0, qIdx) else rawTarget))
        val query = if (qIdx >= 0) parseQuery(rawTarget.substring(qIdx + 1)) else emptyMap()

        var body: ByteArray? = null
        val len = headers["content-length"]?.toLongOrNull() ?: 0L
        if (len in 1..MAX_BODY) {
            body = ByteArray(len.toInt())
            var read = 0
            while (read < body.size) {
                val n = input.read(body, read, body.size - read)
                if (n < 0) return null
                read += n
            }
        }
        return HttpRequest(
            method = method,
            path = path,
            query = query,
            headers = headers,
            body = body,
            expectContinue = headers["expect"]?.equals("100-continue", true) == true,
        )
    }

    private fun readLine(input: InputStream, max: Int): String? {
        val sb = StringBuilder(96)
        while (true) {
            val b = input.read()
            if (b < 0) return if (sb.isEmpty()) null else sb.toString()
            if (b == '\n'.code) {
                if (sb.isNotEmpty() && sb.last() == '\r') sb.setLength(sb.length - 1)
                return sb.toString()
            }
            sb.append(b.toChar())
            if (sb.length > max) return null
        }
    }

    private fun parseQuery(q: String): Map<String, String> {
        if (q.isEmpty()) return emptyMap()
        val out = HashMap<String, String>(8)
        for (pair in q.split('&')) {
            if (pair.isEmpty()) continue
            val i = pair.indexOf('=')
            if (i < 0) out[decode(pair)] = "" else out[decode(pair.substring(0, i))] = decode(pair.substring(i + 1))
        }
        return out
    }

    private fun decode(s: String): String =
        if (s.indexOf('%') < 0 && s.indexOf('+') < 0) s
        else java.net.URLDecoder.decode(s, "UTF-8")

    // --------------------------------------------------------------- response

    private fun writeResponse(
        socket: SocketChannel,
        output: java.io.OutputStream,
        req: HttpRequest,
        res: HttpResponse,
    ): Boolean {
        val keepAlive = req.wantsKeepAlive && !res.closeConnection

        // Headers are collected separately because the status line (and therefore
        // 200 vs 206) is only known after range negotiation for file bodies.
        val hdr = StringBuilder(256)
        res.headers.forEach { (k, v) -> hdr.append(k).append(": ").append(v).append("\r\n") }
        hdr.append("Server: PhotoSync/1.0\r\n")
        hdr.append("Date: ").append(httpDate()).append("\r\n")
        hdr.append("Connection: ").append(if (keepAlive) "keep-alive" else "close").append("\r\n")

        var status = res.status
        val noBody = status == 204 || status == 304 || req.method == "HEAD"

        fun statusLine(code: Int): String =
            "HTTP/1.1 " + code + " " + reason(code) + "\r\n"

        when (val b = res.body) {
            is Body.Bytes -> {
                var bytes = b.bytes
                if (!noBody && res.gzip && bytes.size > 512 && req.acceptsGzip) {
                    bytes = gzip(bytes)
                    hdr.append("Content-Encoding: gzip\r\n")
                }
                hdr.append("Content-Length: ").append(bytes.size).append("\r\n\r\n")
                output.write((statusLine(status) + hdr.toString()).toByteArray())
                if (!noBody) output.write(bytes)
                output.flush()
            }

            is Body.File -> {
                var offset = 0L
                var length = b.length
                var partial = false
                val range = req.headers["range"]
                if (range != null && b.allowRange) {
                    val parsed = parseRange(range, b.length)
                    if (parsed == null) {
                        val msg = "range not satisfiable\n".toByteArray()
                        val r = StringBuilder()
                            .append(statusLine(416))
                            .append("Content-Range: bytes */").append(b.length).append("\r\n")
                            .append("Content-Type: text/plain; charset=utf-8\r\n")
                            .append("Content-Length: ").append(msg.size).append("\r\n")
                            .append("Connection: close\r\n\r\n")
                        output.write(r.toString().toByteArray())
                        output.write(msg)
                        output.flush()
                        return false
                    }
                    offset = parsed.first
                    length = parsed.second
                    partial = true
                    status = 206
                }
                hdr.append("Content-Length: ").append(length).append("\r\n")
                hdr.append("Accept-Ranges: bytes\r\n")
                if (partial) {
                    hdr.append("Content-Range: bytes ").append(offset).append('-')
                        .append(offset + length - 1).append('/').append(b.length).append("\r\n")
                }
                hdr.append("\r\n")
                output.write((statusLine(status) + hdr.toString()).toByteArray())
                output.flush()

                if (!noBody && length > 0) {
                    b.channel.position(0)
                    var sent = 0L
                    while (sent < length) {
                        val n = b.channel.transferTo(offset + sent, length - sent, socket)
                        if (n <= 0) {
                            // fall back to a buffered copy (e.g. non-sendfile fds)
                            copyFallback(output, b.channel, offset + sent, length - sent)
                            sent = length
                            break
                        }
                        sent += n
                    }
                    output.flush()
                }
            }

            is Body.Stream -> {
                hdr.append("Content-Length: ").append(b.length).append("\r\n\r\n")
                output.write((statusLine(status) + hdr.toString()).toByteArray())
                if (!noBody) {
                    val buf = ByteArray(64 * 1024)
                    b.input.use { ins ->
                        while (true) {
                            val n = ins.read(buf)
                            if (n <= 0) break
                            output.write(buf, 0, n)
                        }
                    }
                }
                output.flush()
            }

            Body.Empty -> {
                hdr.append("Content-Length: 0\r\n\r\n")
                output.write((statusLine(status) + hdr.toString()).toByteArray())
                output.flush()
            }
        }
        return keepAlive
    }

    private fun copyFallback(out: java.io.OutputStream, ch: FileChannel, offset: Long, length: Long) {
        ch.position(offset)
        val ch2 = java.nio.channels.Channels.newChannel(out)
        var sent = 0L
        while (sent < length) {
            val n = ch.transferTo(offset + sent, length - sent, ch2)
            if (n <= 0) break
            sent += n
        }
    }

    private fun parseRange(header: String, total: Long): Pair<Long, Long>? {
        if (!header.startsWith("bytes=")) return null
        val spec = header.removePrefix("bytes=").substringBefore(',').trim()
        val dash = spec.indexOf('-')
        if (dash < 0) return null
        val a = spec.substring(0, dash).trim()
        val b = spec.substring(dash + 1).trim()
        return try {
            if (a.isEmpty()) {
                val suffix = b.toLong()
                if (suffix <= 0) return null
                val start = (total - suffix).coerceAtLeast(0)
                start to (total - start)
            } else {
                val start = a.toLong()
                var end = if (b.isEmpty()) total - 1 else b.toLong().coerceAtMost(total - 1)
                if (start > end || start >= total) return null
                start to (end - start + 1)
            }
        } catch (_: Throwable) {
            null
        }
    }

    private fun gzip(data: ByteArray): ByteArray {
        val bos = ByteArrayOutputStream(data.size / 4 + 64)
        GZIPOutputStream(bos, 8192).use { it.write(data) }
        return bos.toByteArray()
    }

    private fun reason(code: Int): String = when (code) {
        200 -> "OK"
        206 -> "Partial Content"
        304 -> "Not Modified"
        400 -> "Bad Request"
        401 -> "Unauthorized"
        403 -> "Forbidden"
        404 -> "Not Found"
        405 -> "Method Not Allowed"
        416 -> "Range Not Satisfiable"
        500 -> "Internal Server Error"
        503 -> "Service Unavailable"
        else -> "OK"
    }

    private fun httpDate(): String {
        val fmt = java.text.SimpleDateFormat("EEE, dd MMM yyyy HH:mm:ss 'GMT'", java.util.Locale.US)
        fmt.timeZone = java.util.TimeZone.getTimeZone("GMT")
        return fmt.format(java.util.Date())
    }

    companion object {
        private const val MAX_BODY = 256 * 1024L
    }
}

// --------------------------------------------------------------------- model

data class HttpRequest(
    val method: String,
    val path: String,
    val query: Map<String, String>,
    val headers: Map<String, String>,
    val body: ByteArray?,
    val expectContinue: Boolean,
) {
    val wantsKeepAlive: Boolean
        get() = headers["connection"]?.lowercase()?.contains("close") != true

    val acceptsGzip: Boolean
        get() = headers["accept-encoding"]?.contains("gzip", true) == true

    fun q(name: String): String? = query[name]
    fun qLong(name: String, def: Long = 0): Long = query[name]?.toLongOrNull() ?: def
    fun qInt(name: String, def: Int = 0): Int = query[name]?.toIntOrNull() ?: def
    fun qBool(name: String, def: Boolean = false): Boolean =
        when (query[name]?.lowercase()) {
            "1", "true", "yes" -> true
            "0", "false", "no" -> false
            else -> def
        }
}

class HttpResponse(
    val status: Int,
    val body: Body = Body.Empty,
    val headers: MutableMap<String, String> = LinkedHashMap(),
    val gzip: Boolean = false,
    val closeConnection: Boolean = false,
) {
    fun header(k: String, v: String): HttpResponse {
        headers[k] = v
        return this
    }

    companion object {
        fun text(status: Int, body: String, contentType: String = "text/plain; charset=utf-8"): HttpResponse =
            HttpResponse(status, Body.Bytes(body.toByteArray()), linkedMapOf("Content-Type" to contentType))

        fun json(status: Int, body: String): HttpResponse =
            HttpResponse(
                status,
                Body.Bytes(body.toByteArray()),
                linkedMapOf("Content-Type" to "application/json; charset=utf-8"),
                gzip = true,
            )

        fun bytes(body: ByteArray, contentType: String, cacheSeconds: Int = 0): HttpResponse {
            val h = linkedMapOf("Content-Type" to contentType)
            if (cacheSeconds > 0) h["Cache-Control"] = "public, max-age=$cacheSeconds"
            return HttpResponse(200, Body.Bytes(body), h)
        }

        fun file(channel: FileChannel, length: Long, contentType: String, etag: String?, cacheSeconds: Int = 0): HttpResponse {
            val h = linkedMapOf(
                "Content-Type" to contentType,
                "Content-Disposition" to "inline",
            )
            if (etag != null) h["ETag"] = etag
            if (cacheSeconds > 0) h["Cache-Control"] = "public, max-age=$cacheSeconds"
            return HttpResponse(200, Body.File(channel, length, allowRange = true), h)
        }

        fun empty(status: Int): HttpResponse = HttpResponse(status)
    }
}

sealed interface Body {
    data object Empty : Body
    class Bytes(val bytes: ByteArray) : Body
    class File(val channel: FileChannel, val length: Long, val allowRange: Boolean) : Body
    class Stream(val input: InputStream, val length: Long) : Body
}
