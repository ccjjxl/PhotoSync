package dev.photosync.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.photosync.AppGraph
import dev.photosync.core.MediaItem
import dev.photosync.core.MediaRepository
import dev.photosync.core.Settings
import dev.photosync.server.ServerEngine
import dev.photosync.util.Net
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class UiState(
    val permission: MediaRepository.PermissionState = MediaRepository.PermissionState.Denied,
    val running: Boolean = false,
    val port: Int = Settings.DEFAULT_PORT,
    val urls: List<String> = emptyList(),
    val ifaces: List<Net.Address> = emptyList(),
    val photos: Int = 0,
    val videos: Int = 0,
    val bytes: Long = 0,
    val recent: List<MediaItem> = emptyList(),
    val folders: List<Pair<String, Int>> = emptyList(),
    val settings: Settings.Snapshot? = null,
    val deviceName: String = "",
    val deviceId: String = "",
    val folderName: String = "",
    val servedBytes: Long = 0,
    val servedFiles: Long = 0,
    val thumbCacheBytes: Long = 0,
    val loadingLibrary: Boolean = true,
    val error: String? = null,
    val toast: String? = null,
)

class MainViewModel(app: Application) : AndroidViewModel(app) {

    private val graph = AppGraph.get(app)
    private val repo = graph.repository
    private val settings = graph.settings
    private val engine: ServerEngine = graph.server

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    init {
        _state.update {
            it.copy(
                settings = settings.snapshot(),
                port = settings.snapshot().port,
                permission = repo.permissionState(),
                deviceName = settings.effectiveDeviceName(),
                deviceId = settings.snapshot().deviceId,
                folderName = settings.folderName(),
            )
        }
        viewModelScope.launch {
            engine.status.collect { st ->
                _state.update {
                    it.copy(running = st.running, port = st.port, urls = st.urls, error = st.lastError)
                }
            }
        }
        viewModelScope.launch {
            while (true) {
                engine.refreshConnectionCount()
                val st = engine.status.value
                _state.update {
                    it.copy(
                        servedBytes = engine.totalBytesServed,
                        servedFiles = engine.totalFilesServed,
                        ifaces = Net.localAddresses(),
                        urls = if (st.running) {
                            Net.localAddresses().map { a -> "http://${a.ip}:${it.port}" }
                        } else it.urls,
                    )
                }
                delay(1000)
            }
        }
        refreshPermission()
        if (repo.permissionState() == MediaRepository.PermissionState.Granted) loadLibrary()

        // Keep the library view in sync with new photos taken while running.
        repo.observeChanges {
            if (repo.permissionState() == MediaRepository.PermissionState.Granted) {
                loadLibrary(quiet = true)
            }
        }
    }

    fun refreshPermission() {
        val p = repo.permissionState()
        _state.update { it.copy(permission = p) }
        if (p == MediaRepository.PermissionState.Granted && _state.value.photos == 0) loadLibrary()
    }

    fun onPermissionResult() {
        refreshPermission()
        loadLibrary()
    }

    fun loadLibrary(quiet: Boolean = false) {
        if (loading) return
        loading = true
        viewModelScope.launch {
            _state.update { it.copy(loadingLibrary = !quiet) }
            val snap = settings.snapshot()
            val stats = withContext(Dispatchers.IO) { repo.stats(snap.includeVideos) }
            val folders = withContext(Dispatchers.IO) { repo.folders(60) }
            val recent = withContext(Dispatchers.IO) {
                repo.query(
                    MediaRepository.Query(
                        includeVideos = snap.includeVideos,
                        limit = 24,
                        ascending = false,
                    ),
                )
            }
            _state.update {
                it.copy(
                    photos = stats.photos,
                    videos = stats.videos,
                    bytes = stats.bytes,
                    folders = folders,
                    recent = recent,
                    loadingLibrary = false,
                    thumbCacheBytes = graph.thumbnails.diskCacheSize(),
                )
            }
            loading = false
        }
    }

    private var loading = false

    fun startServer() {
        viewModelScope.launch {
            graph.server.start()
            dev.photosync.service.SyncService.start(getApplication())
            delay(150)
            val st = engine.status.value
            _state.update {
                it.copy(
                    running = st.running,
                    urls = Net.localAddresses().map { a -> "http://${a.ip}:${st.port}" },
                    error = st.lastError,
                )
            }
        }
    }

    fun stopServer() {
        graph.stopServer()
        _state.update { it.copy(running = false) }
    }

    fun toggleServer() = if (_state.value.running) stopServer() else startServer()

    fun setPort(port: Int) {
        settings.setPort(port)
        _state.update { it.copy(port = settings.snapshot().port, settings = settings.snapshot()) }
        if (engine.running) {
            engine.stop()
            engine.start()
        }
    }

    fun setIncludeVideos(v: Boolean) {
        settings.setIncludeVideos(v)
        _state.update { it.copy(settings = settings.snapshot()) }
        loadLibrary()
    }

    fun setOpenAccess(v: Boolean) {
        settings.setOpenAccess(v)
        _state.update { it.copy(settings = settings.snapshot()) }
    }

    fun setDeviceName(name: String) {
        settings.setDeviceName(name)
        _state.update {
            it.copy(
                settings = settings.snapshot(),
                deviceName = settings.effectiveDeviceName(),
                folderName = settings.folderName(),
                toast = "设备名已更新为 ${settings.effectiveDeviceName()}",
            )
        }
        // Discovery answers carry the name, restart so clients see it immediately.
        if (engine.running) engine.refresh()
    }

    fun rotateCode() {
        settings.rotatePairingCode()
        _state.update { it.copy(settings = settings.snapshot(), toast = "配对码已更新") }
    }

    fun rotateToken() {
        settings.rotateToken()
        _state.update { it.copy(settings = settings.snapshot(), toast = "访问令牌已重置，请重新配对") }
    }

    fun clearThumbCache() {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { graph.thumbnails.clearDiskCache() }
            _state.update { it.copy(thumbCacheBytes = 0, toast = "缩略图缓存已清理") }
        }
    }

    fun openPermissionSettings() = repo.openPermissionSettings()

    fun consumeToast() = _state.update { it.copy(toast = null) }

    fun copyToClipboard(text: String) {
        val cm = getApplication<Application>().getSystemService(android.content.ClipboardManager::class.java)
        cm?.setPrimaryClip(android.content.ClipData.newPlainText("PhotoSync", text))
        _state.update { it.copy(toast = "已复制：$text") }
    }
}
