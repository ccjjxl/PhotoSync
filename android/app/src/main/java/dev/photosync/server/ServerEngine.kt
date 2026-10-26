package dev.photosync.server

import android.content.Context
import android.os.Build
import dev.photosync.BuildConfig
import dev.photosync.core.MediaItem
import dev.photosync.core.MediaRepository
import dev.photosync.core.Settings
import dev.photosync.util.Net
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.runBlocking
import java.io.File
import java.util.concurrent.atomic.AtomicLong

/**
 * The sync API. Every endpoint is intentionally simple so the desktop client
 * can stay a single static binary.
 *
 *  GET  /api/ping                     -> liveness + api version
 *  POST /api/pair      {code}         -> {token}
 *  GET  /api/info                     -> device + library summary
 *  GET  /api/stats                    -> counts and sizes
 *  GET  /api/folders                  -> [{path,count}]
 *  GET  /api/list?since=&offset=&limit=&images=&videos=   -> page of items
 *  GET  /api/thumb?id=&size=          -> JPEG thumbnail
 *  GET  /api/file?id=|path=           -> original bytes (Range, ETag, gzip n/a)
 *  GET  /                             -> web gallery (usable from any browser)
 */
class ServerEngine(
    private val context: Context,
    private val repository: MediaRepository,
    private val settings: Settings,
    private val thumbnailProvider: ThumbnailProvider = ThumbnailProvider(context, repository, context.cacheDir),
) {
    private val discovery = DiscoveryResponder(
        portProvider = { settings.snapshot().port },
        deviceNameProvider = { settings.effectiveDeviceName() },
        deviceIdProvider = { settings.snapshot().deviceId },
        folderNameProvider = { settings.folderName() },
        openAccessProvider = { settings.snapshot().openAccess },
    )


    data class Status(
        val running: Boolean = false,
        val port: Int = Settings.DEFAULT_PORT,
        val urls: List<String> = emptyList(),
        val uploadsServed: Long = 0,
        val bytesServed: Long = 0,
        val connected: Int = 0,
        val lastError: String? = null,
    )

    private val _status = MutableStateFlow(Status())
    val status: StateFlow<Status> = _status.asStateFlow()

    /** Live connection count, sampled on demand (cheap, avoids a polling loop). */
    fun refreshConnectionCount() {
        val live = (current?.activeConnections ?: 0L).toInt()
        if (_status.value.connected != live) {
            _status.value = _status.value.copy(connected = live)
        }
    }

    private val bytesServed = AtomicLong(0)
    private val uploadsServed = AtomicLong(0)

    val totalBytesServed: Long get() = bytesServed.get()
    val totalFilesServed: Long get() = uploadsServed.get()

    fun resetCounters() {
        bytesServed.set(0)
        uploadsServed.set(0)
    }

    @Volatile private var current: HttpServer? = null

    val running: Boolean get() = current?.isRunning == true

    fun start() {
        val snap = settings.snapshot()
        if (running) return
        val urls = Net.localAddresses().map { "http://${it.ip}:${snap.port}" }
        try {
            val s = HttpServer(snap.port) { route(it) }
            s.start()
            current = s
            discovery.start()
            _status.value = Status(running = true, port = snap.port, urls = urls)
            thumbnailProvider.pruneDiskCache()
        } catch (t: Throwable) {
            _status.value = Status(
                running = false,
                port = snap.port,
                lastError = friendlyBindError(t),
            )
        }
    }

    private fun friendlyBindError(t: Throwable): String = when (t) {
        is java.net.BindException -> "端口 ${settings.snapshot().port} 已被其他应用占用"
        is SecurityException -> "没有网络权限"
        else -> t.message ?: "服务启动失败"
    }

    fun stop() {
        current?.stop()
        current = null
        discovery.stop()
        _status.value = _status.value.copy(running = false, connected = 0)
    }

    fun refresh() {
        if (running) {
            stop()
            start()
        }
    }


    // ---------------------------------------------------------------- routing

    private fun route(req: HttpRequest): HttpResponse {
        val path = req.path
        return try {
            when {
                path == "/api/ping" -> ping(req)
                path == "/api/pair" -> pair(req)
                else -> {
                    if (!authorized(req)) {
                        return HttpResponse.json(401, """{"error":"unauthorized"}""")
                    }
                    when {
                        path == "/api/info" -> info(req)
                        path == "/api/stats" -> stats(req)
                        path == "/api/folders" -> folders(req)
                        path == "/api/list" -> list(req)
                        path == "/api/thumb" -> thumb(req)
                        path == "/api/file" -> file(req)
                        path == "/" || path == "/index.html" -> HttpResponse.bytes(
                            WebGallery.HTML.toByteArray(), "text/html; charset=utf-8",
                        ).header("Cache-Control", "no-store")
                        path == "/icon.png" -> HttpResponse.bytes(WebGallery.ICON, "image/png", 86400)
                        path.startsWith("/dl/") -> download(req)
                        else -> HttpResponse.json(404, """{"error":"not found"}""")
                    }
                }
            }
        } catch (t: Throwable) {
            HttpResponse.json(500, json { field("error", t.message ?: t.javaClass.simpleName) })
        }
    }

    private fun authorized(req: HttpRequest): Boolean {
        val snap = settings.snapshot()
        if (snap.openAccess) return true
        val provided = req.headers["x-photosync-token"] ?: req.q("token")
        return provided != null && constantTimeEquals(provided, snap.token)
    }

    private fun constantTimeEquals(a: String, b: String): Boolean {
        if (a.length != b.length) return false
        var r = 0
        for (i in a.indices) r = r or (a[i].code xor b[i].code)
        return r == 0
    }

    // -------------------------------------------------------------- endpoints

    private fun ping(req: HttpRequest): HttpResponse = HttpResponse.json(
        200,
        json {
            field("app", "PhotoSync")
            field("version", BuildConfig.VERSION_NAME)
            field("api", API_VERSION)
            field("device", settings.effectiveDeviceName())
            field("deviceId", settings.snapshot().deviceId)
            field("folder", settings.folderName())
            field("openAccess", settings.snapshot().openAccess)
        },
    )

    private fun pair(req: HttpRequest): HttpResponse {
        if (req.method != "POST") return HttpResponse.json(405, """{"error":"POST required"}""")
        val code = JsonReader.string(req.body?.toString(Charsets.UTF_8), "code")
            ?: req.q("code")
            ?: return HttpResponse.json(400, """{"error":"code required"}""")
        val snap = settings.snapshot()
        if (!constantTimeEquals(code, snap.pairingCode)) {
            return HttpResponse.json(403, """{"error":"wrong code"}""")
        }
        return HttpResponse.json(
            200,
            json {
                field("token", snap.token)
                field("device", settings.effectiveDeviceName())
                field("deviceId", snap.deviceId)
                field("folder", settings.folderName())
                field("api", API_VERSION)
            },
        )
    }

    private fun info(req: HttpRequest): HttpResponse {
        val snap = settings.snapshot()
        val st = repository.stats(snap.includeVideos)
        val urls = Net.localAddresses().map { it.ip to it.iface }
        return HttpResponse.json(
            200,
            json {
                field("app", "PhotoSync")
                field("version", BuildConfig.VERSION_NAME)
                field("api", API_VERSION)
                field("device", settings.effectiveDeviceName())
                field("deviceId", settings.snapshot().deviceId)
                field("folder", settings.folderName())
                field("model", Build.MODEL)
                field("manufacturer", Build.MANUFACTURER)
                field("android", Build.VERSION.RELEASE)
                field("sdk", Build.VERSION.SDK_INT)
                field("port", snap.port)
                field("openAccess", snap.openAccess)
                field("includeVideos", snap.includeVideos)
                field("photos", st.photos)
                field("videos", st.videos)
                field("bytes", st.bytes)
                field("server", "PhotoSync/1.0")
                arr("addresses") {
                    urls.forEach { (ip, iface) ->
                        obj { field("ip", ip); field("iface", iface) }
                    }
                }
            },
        )
    }

    private fun stats(req: HttpRequest): HttpResponse {
        val snap = settings.snapshot()
        val st = repository.stats(snap.includeVideos)
        val (hits, misses) = thumbnailProvider.stats()
        return HttpResponse.json(
            200,
            json {
                field("photos", st.photos)
                field("videos", st.videos)
                field("bytes", st.bytes)
                field("thumbHits", hits)
                field("thumbMisses", misses)
                field("thumbCacheBytes", thumbnailProvider.diskCacheSize())
                field("served", bytesServed.get())
                field("servedFiles", uploadsServed.get())
            },
        )
    }

    private fun folders(req: HttpRequest): HttpResponse {
        val list = repository.folders()
        return HttpResponse.json(
            200,
            json {
                arr("folders") {
                    list.forEach { (p, c) -> obj { field("path", p); field("count", c) } }
                }
            },
        )
    }

    private fun list(req: HttpRequest): HttpResponse {
        val since = req.qLong("since", 0)
        val limit = req.qInt("limit", 500).coerceIn(1, 2000)
        val offset = req.qInt("offset", 0).coerceAtLeast(0)
        val images = req.qBool("images", true)
        val videos = req.qBool("videos", settings.snapshot().includeVideos)
        val ascending = req.qBool("asc", true)
        val folder = req.q("folder")

        val q = MediaRepository.Query(
            since = since,
            includeImages = images,
            includeVideos = videos,
            folder = folder,
            limit = limit,
            offset = offset,
            ascending = ascending,
        )
        var items: List<MediaItem> = repository.query(q)
        if (folder != null) items = items.filter { it.folder == folder }

        val total = repository.count(since, videos)
        return HttpResponse.json(
            200,
            json {
                field("since", since)
                field("offset", offset)
                field("limit", limit)
                field("total", total)
                field("count", items.size)
                field("hasMore", items.size == limit)
                arr("items") {
                    items.forEach { entry -> itemJson(this, entry) }
                }
            },
        )
    }

    private fun itemJson(w: JsonWriter, item: MediaItem) {
        w.obj {
            field("id", item.id)
            field("name", item.name)
            field("path", item.relPath)
            field("folder", item.folder)
            field("size", item.size)
            field("mtime", item.modifiedSec)
            field("taken", item.takenSec)
            field("mime", item.mimeType)
            field("width", item.width)
            field("height", item.height)
            field("video", item.isVideo)
            field("key", item.cacheKey)
        }
    }

    private fun thumb(req: HttpRequest): HttpResponse {
        val id = req.qLong("id", -1)
        if (id < 0) return HttpResponse.json(400, """{"error":"id required"}""")
        val size = req.qInt("size", 320)
        val item = repository.findById(id) ?: return HttpResponse.json(404, """{"error":"not found"}""")
        val bytes = runBlocking { thumbnailProvider.get(item, size) }
            ?: return HttpResponse.json(404, """{"error":"no thumbnail"}""")
        return HttpResponse.bytes(bytes, "image/jpeg", cacheSeconds = 86400)
            .header("ETag", "\"${item.cacheKey}-$size\"")
    }

    private fun file(req: HttpRequest): HttpResponse {
        val pathParam = req.q("path")
        val item = if (pathParam != null) {
            repository.findByPath(pathParam)
        } else {
            val id = req.qLong("id", -1)
            if (id < 0) null else repository.findById(id)
        } ?: return HttpResponse.json(404, """{"error":"not found"}""")

        val pfd = context.contentResolver.openFileDescriptor(repository.contentUri(item.id), "r")
            ?: return HttpResponse.json(404, """{"error":"cannot open"}""")
        val channel = java.io.FileInputStream(pfd.fileDescriptor).channel
        val etag = "\"${item.cacheKey}\""

        val inm = req.headers["if-none-match"]
        if (inm != null && inm.contains(item.cacheKey)) {
            runCatching { channel.close() }
            return HttpResponse.empty(304).header("ETag", etag)
        }

        bytesServed.addAndGet(item.size)
        uploadsServed.incrementAndGet()
        return HttpResponse.file(
            channel = channel,
            length = item.size,
            contentType = item.mimeType.ifEmpty { if (item.isVideo) "video/mp4" else "image/jpeg" },
            etag = etag,
        ).header("X-PhotoSync-Key", item.cacheKey)
            .header("X-PhotoSync-Mtime", item.modifiedSec.toString())
            .header("X-PhotoSync-Path", item.relPath)
    }

    /** Path-based download, keeps browsers/curl usage natural: /dl/DCIM/Camera/x.jpg */
    private fun download(req: HttpRequest): HttpResponse {
        val rel = req.path.removePrefix("/dl/").trimStart('/')
        val item = repository.findByPath(rel) ?: return HttpResponse.json(404, """{"error":"not found"}""")
        val pfd = context.contentResolver.openFileDescriptor(repository.contentUri(item.id), "r")
            ?: return HttpResponse.json(404, """{"error":"cannot open"}""")
        val channel = java.io.FileInputStream(pfd.fileDescriptor).channel
        bytesServed.addAndGet(item.size)
        uploadsServed.incrementAndGet()
        return HttpResponse.file(
            channel, item.size,
            item.mimeType.ifEmpty { "application/octet-stream" },
            "\"${item.cacheKey}\"",
        ).header("Content-Disposition", "attachment; filename=\"${item.name}\"")
    }

    companion object {
        const val API_VERSION = 1
    }
}
