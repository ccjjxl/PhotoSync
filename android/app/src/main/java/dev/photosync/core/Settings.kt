package dev.photosync.core

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.security.SecureRandom

/**
 * Persisted user preferences + generated secrets.
 */
class Settings(context: Context) {

    private val prefs: SharedPreferences =
        context.getSharedPreferences("photo_sync", Context.MODE_PRIVATE)

    data class Snapshot(
        val port: Int,
        val includeVideos: Boolean,
        val openAccess: Boolean,
        val pairingCode: String,
        val token: String,
        /** Stable random id so the desktop client can tell devices apart. */
        val deviceId: String,
        /** User-editable display name, defaults to the model name. */
        val deviceName: String,
        /**
         * Folders the user chose to sync. Empty set means "every folder",
         * so a fresh install keeps syncing everything without extra setup.
         */
        val selectedFolders: Set<String>,
    )

    /**
     * In-memory mirror of the folder whitelist.
     *
     * SharedPreferences.apply() persists asynchronously and keeps a reference to
     * the Set it was given, so reading straight back can return stale data (and
     * a caller mutating its own Set would corrupt it). Keeping the value here
     * makes reads immediately consistent.
     */
    @Volatile private var folderMemo: Set<String>? = null

    private val _state = MutableStateFlow(snapshot())
    val state: StateFlow<Snapshot> = _state.asStateFlow()

    fun snapshot(): Snapshot {
        ensureSecrets()
        return Snapshot(
            port = prefs.getInt(KEY_PORT, DEFAULT_PORT),
            includeVideos = prefs.getBoolean(KEY_VIDEOS, true),
            openAccess = prefs.getBoolean(KEY_OPEN, false),
            pairingCode = prefs.getString(KEY_CODE, "000000")!!,
            token = prefs.getString(KEY_TOKEN, "")!!,
            deviceId = prefs.getString(KEY_DEVICE_ID, "")!!,
            deviceName = prefs.getString(KEY_DEVICE_NAME, "")!!,
            selectedFolders = currentFolders(),
        )
    }

    private fun ensureSecrets() {
        if (prefs.getString(KEY_TOKEN, null) == null) {
            prefs.edit()
                .putString(KEY_TOKEN, randomHex(24))
                .putString(KEY_CODE, randomCode())
                .apply()
        }
        if (prefs.getString(KEY_DEVICE_ID, null).isNullOrEmpty()) {
            // 8 hex chars: short enough to show in the UI, unique enough to identify a phone.
            prefs.edit().putString(KEY_DEVICE_ID, randomHex(4)).apply()
        }
    }

    private fun publish() {
        _state.value = snapshot()
    }

    fun setPort(port: Int) = prefs.edit().putInt(KEY_PORT, port.coerceIn(1024, 65535)).apply { publish() }
    fun setIncludeVideos(v: Boolean) = prefs.edit().putBoolean(KEY_VIDEOS, v).apply { publish() }
    fun setOpenAccess(v: Boolean) = prefs.edit().putBoolean(KEY_OPEN, v).apply { publish() }

    fun rotatePairingCode() = prefs.edit().putString(KEY_CODE, randomCode()).apply { publish() }

    fun rotateToken() = prefs.edit().putString(KEY_TOKEN, randomHex(24)).apply { publish() }

    /** Folder whitelist, read from memory first for immediate consistency. */
    fun currentFolders(): Set<String> {
        folderMemo?.let { return it }
        val stored = prefs.getStringSet(KEY_SELECTED_FOLDERS, emptySet())?.toSet() ?: emptySet()
        folderMemo = stored
        return stored
    }

    /** Replace the folder whitelist. An empty set disables filtering (sync all). */
    fun setSelectedFolders(folders: Set<String>) {
        val cleaned = folders.map { it.trim().trimEnd('/') }.filter { it.isNotEmpty() }.toSet()
        folderMemo = cleaned
        // Hand over a fresh copy: SharedPreferences retains the reference.
        prefs.edit().putStringSet(KEY_SELECTED_FOLDERS, HashSet(cleaned)).apply()
        publish()
    }

    fun selectAllFolders() = setSelectedFolders(emptySet())

    /** True when the user restricted syncing to specific folders. */
    fun hasFolderFilter(): Boolean = currentFolders().isNotEmpty()

    fun setDeviceName(name: String) {
        val trimmed = name.trim().take(40)
        prefs.edit().putString(KEY_DEVICE_NAME, trimmed).apply { publish() }
    }

    /** Display name, falling back to the hardware model. */
    fun effectiveDeviceName(): String {
        val custom = prefs.getString(KEY_DEVICE_NAME, "").orEmpty()
        if (custom.isNotEmpty()) return custom
        val model = (android.os.Build.MODEL ?: "Android").replace(' ', '-')
        val maker = (android.os.Build.MANUFACTURER ?: "").replace(' ', '-')
        return if (maker.isEmpty() || model.lowercase().startsWith(maker.lowercase())) {
            model
        } else {
            "$maker-$model"
        }
    }

    /** Name used for the on-disk folder on the PC: safe, stable, unique-ish. */
    fun folderName(): String {
        val base = effectiveDeviceName()
            .replace(Regex("[^A-Za-z0-9._-]"), "-")
            .trim('-')
            .ifEmpty { "Android" }
        return "$base-${snapshot().deviceId}"
    }

    companion object {
        const val DEFAULT_PORT = 8737
        private const val KEY_PORT = "port"
        private const val KEY_VIDEOS = "videos"
        private const val KEY_OPEN = "open"
        private const val KEY_CODE = "pair_code"
        private const val KEY_TOKEN = "token"
        private const val KEY_DEVICE_ID = "device_id"
        private const val KEY_DEVICE_NAME = "device_name"
        private const val KEY_SELECTED_FOLDERS = "selected_folders"

        fun randomHex(bytes: Int): String {
            val b = ByteArray(bytes)
            SecureRandom().nextBytes(b)
            return b.joinToString("") { "%02x".format(it) }
        }

        fun randomCode(): String {
            val r = SecureRandom().nextInt(1_000_000)
            return "%06d".format(r)
        }
    }
}
