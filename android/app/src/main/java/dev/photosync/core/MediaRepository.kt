package dev.photosync.core

import android.content.ContentResolver
import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import androidx.core.content.ContextCompat
import java.util.concurrent.ConcurrentHashMap

/**
 * MediaStore backed media index. Queries images+videos from the system media database,
 * which is orders of magnitude faster than walking directories.
 */
class MediaRepository(private val context: Context) {

    private val resolver: ContentResolver get() = context.contentResolver

    sealed interface PermissionState {
        data object Granted : PermissionState
        /** API 34+ partial access: only user-selected items are visible. */
        data object Partial : PermissionState
        data object Denied : PermissionState
    }

    fun permissionState(): PermissionState {
        val readImages = if (Build.VERSION.SDK_INT >= 33) "android.permission.READ_MEDIA_IMAGES" else null
        val readVideos = if (Build.VERSION.SDK_INT >= 33) "android.permission.READ_MEDIA_VIDEO" else null

        fun granted(p: String) = ContextCompat.checkSelfPermission(context, p) == PackageManager.PERMISSION_GRANTED

        return if (Build.VERSION.SDK_INT >= 33) {
            val img = granted(readImages!!)
            val vid = granted(readVideos!!)
            val partial = if (Build.VERSION.SDK_INT >= 34)
                granted("android.permission.READ_MEDIA_VISUAL_USER_SELECTED") else false
            when {
                img && vid -> PermissionState.Granted
                img || vid || partial -> PermissionState.Granted
                else -> PermissionState.Denied
            }
        } else {
            if (granted(android.Manifest.permission.READ_EXTERNAL_STORAGE)) PermissionState.Granted
            else PermissionState.Denied
        }
    }

    fun requiredPermissions(): Array<String> = when {
        Build.VERSION.SDK_INT >= 34 -> arrayOf(
            "android.permission.READ_MEDIA_IMAGES",
            "android.permission.READ_MEDIA_VIDEO",
            "android.permission.READ_MEDIA_VISUAL_USER_SELECTED",
        )
        Build.VERSION.SDK_INT >= 33 -> arrayOf(
            "android.permission.READ_MEDIA_IMAGES",
            "android.permission.READ_MEDIA_VIDEO",
        )
        else -> arrayOf(android.Manifest.permission.READ_EXTERNAL_STORAGE)
    }

    fun openPermissionSettings() {
        val intent = Intent(
            android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
            Uri.fromParts("package", context.packageName, null),
        ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
    }

    data class Query(
        val since: Long = 0,
        val includeImages: Boolean = true,
        val includeVideos: Boolean = true,
        val folder: String? = null,
        /** Restrict to these RELATIVE_PATH prefixes; empty = no restriction. */
        val folders: Set<String> = emptySet(),
        val limit: Int = 500,
        val offset: Int = 0,
        val ascending: Boolean = false,
    )

    private val collection: Uri
        get() = if (Build.VERSION.SDK_INT >= 29) {
            MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL)
        } else {
            MediaStore.Files.getContentUri("external")
        }

    private fun projection(): Array<String> {
        val base = mutableListOf(
            MediaStore.Files.FileColumns._ID,
            MediaStore.Files.FileColumns.DISPLAY_NAME,
            MediaStore.Files.FileColumns.SIZE,
            MediaStore.Files.FileColumns.DATE_MODIFIED,
            MediaStore.Files.FileColumns.DATE_TAKEN,
            MediaStore.Files.FileColumns.MIME_TYPE,
            MediaStore.Files.FileColumns.MEDIA_TYPE,
            MediaStore.Files.FileColumns.WIDTH,
            MediaStore.Files.FileColumns.HEIGHT,
        )
        if (Build.VERSION.SDK_INT >= 29) {
            base += MediaStore.Files.FileColumns.RELATIVE_PATH
        } else {
            @Suppress("DEPRECATION")
            base += MediaStore.Files.FileColumns.DATA
        }
        if (Build.VERSION.SDK_INT >= 30) {
            base += MediaStore.Files.FileColumns.IS_TRASHED
        }
        return base.toTypedArray()
    }

    /** Query a page of media items. */
    fun query(q: Query): List<MediaItem> {
        val projection = projection()
        val selection = StringBuilder()
        val args = mutableListOf<String>()

        val types = mutableListOf<Int>()
        if (q.includeImages) types += MediaStore.Files.FileColumns.MEDIA_TYPE_IMAGE
        if (q.includeVideos) types += MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO
        selection.append(MediaStore.Files.FileColumns.MEDIA_TYPE)
            .append(" IN (").append(types.joinToString(",")).append(")")
        selection.append(" AND ").append(MediaStore.Files.FileColumns.SIZE).append(" > 0")
        if (q.since > 0) {
            selection.append(" AND ").append(MediaStore.Files.FileColumns.DATE_MODIFIED).append(" >= ?")
            args += q.since.toString()
        }
        if (Build.VERSION.SDK_INT >= 30) {
            selection.append(" AND ").append(MediaStore.Files.FileColumns.IS_TRASHED).append(" = 0")
        }
        appendFolderFilter(selection, args, q.folders)

        val order = MediaStore.Files.FileColumns.DATE_MODIFIED + if (q.ascending) " ASC" else " DESC"
        val bundle = Bundle().apply {
            putString(ContentResolver.QUERY_ARG_SQL_SELECTION, selection.toString())
            putStringArray(ContentResolver.QUERY_ARG_SQL_SELECTION_ARGS, args.toTypedArray())
            putString(ContentResolver.QUERY_ARG_SQL_SORT_ORDER, order)
            putInt(ContentResolver.QUERY_ARG_LIMIT, q.limit.coerceIn(1, 5000))
            putInt(ContentResolver.QUERY_ARG_OFFSET, q.offset.coerceAtLeast(0))
        }

        val out = ArrayList<MediaItem>(q.limit)
        runCatching {
            resolver.query(collection, projection, bundle, null)?.use { c ->
                val idxId = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns._ID)
                val idxName = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DISPLAY_NAME)
                val idxSize = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.SIZE)
                val idxMod = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DATE_MODIFIED)
                val idxTaken = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DATE_TAKEN)
                val idxMime = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.MIME_TYPE)
                val idxType = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.MEDIA_TYPE)
                val idxW = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.WIDTH)
                val idxH = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.HEIGHT)
                val idxPath = c.getColumnIndex(
                    if (Build.VERSION.SDK_INT >= 29) MediaStore.Files.FileColumns.RELATIVE_PATH
                    else MediaStore.Files.FileColumns.DATA
                )

                while (c.moveToNext()) {
                    val rawPath = if (idxPath >= 0 && !c.isNull(idxPath)) c.getString(idxPath) else ""
                    val rel = normalizePath(rawPath, c.getString(idxName) ?: "unknown")
                    val name = c.getString(idxName) ?: rel.substringAfterLast('/')
                    val type = c.getInt(idxType)
                    out += MediaItem(
                        id = c.getLong(idxId),
                        name = name,
                        relPath = rel,
                        folder = folderOf(rel),
                        size = c.getLong(idxSize),
                        modifiedSec = c.getLong(idxMod),
                        takenSec = if (c.isNull(idxTaken)) 0 else c.getLong(idxTaken) / 1000L,
                        mimeType = c.getString(idxMime) ?: "",
                        width = if (c.isNull(idxW)) 0 else c.getInt(idxW),
                        height = if (c.isNull(idxH)) 0 else c.getInt(idxH),
                        isVideo = type == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO,
                    )
                }
            }
        }.onFailure { /* permission revoked mid-flight: return what we have */ }
        return out
    }

    /**
     * Restricts a query to the given folders. Uses `RELATIVE_PATH LIKE 'DCIM/Camera/%'`
     * (or `DATA LIKE '/storage/.../DCIM/Camera/%'` before API 29) so the database
     * does the filtering — a few folders cost the same as none.
     */
    private fun appendFolderFilter(selection: StringBuilder, args: MutableList<String>, folders: Set<String>) {
        if (folders.isEmpty()) return
        val clean = folders.map { it.trim().trimEnd('/') }.filter { it.isNotEmpty() }
        if (clean.isEmpty()) return

        selection.append(" AND (")
        clean.forEachIndexed { i, folder ->
            if (i > 0) selection.append(" OR ")
            if (Build.VERSION.SDK_INT >= 29) {
                selection.append(MediaStore.Files.FileColumns.RELATIVE_PATH).append(" LIKE ?")
                args += "$folder/%"
            } else {
                @Suppress("DEPRECATION")
                selection.append(MediaStore.Files.FileColumns.DATA).append(" LIKE ?")
                args += "%/$folder/%"
            }
        }
        selection.append(")")
    }

    private fun normalizePath(raw: String, name: String): String {
        if (name.isEmpty()) return raw
        if (raw.isEmpty()) return name
        val cleaned = raw.replace('\\', '/').trimStart('/')
        return if (Build.VERSION.SDK_INT >= 29) {
            // RELATIVE_PATH is a directory (e.g. "DCIM/Camera/") — append the file name.
            if (cleaned.isEmpty()) name
            else cleaned.trimEnd('/') + "/" + name
        } else {
            // Legacy DATA column holds the absolute path; strip the storage prefix.
            val marker = cleaned.indexOf("/DCIM/", ignoreCase = true)
            val markerPictures = cleaned.indexOf("/Pictures/", ignoreCase = true)
            val markerMovies = cleaned.indexOf("/Movies/", ignoreCase = true)
            val markerDcimLower = cleaned.indexOf("/dcim/", ignoreCase = true)
            val cut = when {
                marker >= 0 -> marker + 1
                markerPictures >= 0 -> markerPictures + 1
                markerMovies >= 0 -> markerMovies + 1
                markerDcimLower >= 0 -> markerDcimLower + 1
                else -> cleaned.indexOf('/', cleaned.indexOf('/') + 1) + 1
            }
            if (cut in 1 until cleaned.length) cleaned.substring(cut) else cleaned
        }
    }

    private fun folderOf(relPath: String): String {
        val idx = relPath.lastIndexOf('/')
        return if (idx <= 0) "根目录" else relPath.substring(0, idx).trimEnd('/')
    }

    /** Fast aggregate counts, used by the home screen. */
    data class Stats(val photos: Int, val videos: Int, val bytes: Long)

    fun stats(includeVideos: Boolean = true, folders: Set<String> = emptySet()): Stats {
        val projection = arrayOf(
            MediaStore.Files.FileColumns.MEDIA_TYPE,
            MediaStore.Files.FileColumns.SIZE,
        )
        val types = if (includeVideos) "1,3" else "1"
        val selectionSb = StringBuilder("${MediaStore.Files.FileColumns.MEDIA_TYPE} IN ($types)")
            .append(" AND ${MediaStore.Files.FileColumns.SIZE} > 0")
        if (Build.VERSION.SDK_INT >= 30) {
            selectionSb.append(" AND ${MediaStore.Files.FileColumns.IS_TRASHED} = 0")
        }
        val args = mutableListOf<String>()
        appendFolderFilter(selectionSb, args, folders)
        val selection = selectionSb.toString()
        var photos = 0
        var videos = 0
        var bytes = 0L
        runCatching {
            resolver.query(collection, projection, selection, args.toTypedArray(), null)?.use { c ->
                while (c.moveToNext()) {
                    val size = c.getLong(1)
                    if (c.getInt(0) == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO) videos++ else photos++
                    bytes += size
                }
            }
        }
        return Stats(photos, videos, bytes)
    }

    /** Distinct folders with counts, newest first. Cached for a few seconds. */
    private data class FolderCache(val at: Long, val value: List<Pair<String, Int>>)
    private var folderCache: FolderCache? = null

    fun folders(maxFolders: Int = 200): List<Pair<String, Int>> {
        folderCache?.let { if (System.currentTimeMillis() - it.at < 15_000) return it.value }

        val projection = projection()
        val selection = "${MediaStore.Files.FileColumns.MEDIA_TYPE} IN (1,3)" +
            " AND ${MediaStore.Files.FileColumns.SIZE} > 0"
        val order = MediaStore.Files.FileColumns.DATE_MODIFIED + " DESC"
        val counts = ConcurrentHashMap<String, Int>()
        var scanned = 0
        runCatching {
            resolver.query(collection, projection, selection, null, order)?.use { c ->
                val idxName = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DISPLAY_NAME)
                val idxPath = c.getColumnIndex(
                    if (Build.VERSION.SDK_INT >= 29) MediaStore.Files.FileColumns.RELATIVE_PATH
                    else MediaStore.Files.FileColumns.DATA
                )
                while (c.moveToNext()) {
                    // Cap the scan so a 50k-item library still reports quickly.
                    if (scanned++ > 60_000) break
                    val raw = if (idxPath >= 0 && !c.isNull(idxPath)) c.getString(idxPath) else ""
                    val rel = normalizePath(raw, c.getString(idxName) ?: "")
                    val f = folderOf(rel)
                    counts[f] = (counts[f] ?: 0) + 1
                }
            }
        }
        val list = counts.entries
            .sortedByDescending { it.value }
            .take(maxFolders)
            .map { it.key to it.value }
        folderCache = FolderCache(System.currentTimeMillis(), list)
        return list
    }

    fun contentUri(id: Long): Uri =
        ContentUris.withAppendedId(collection, id)

    private val pathIndex: MutableMap<String, Long> = ConcurrentHashMap()
    private val pathIndexAt = java.util.concurrent.atomic.AtomicLong(0)

    /** Resolve a relative path to a MediaStore id, building a cheap name index on demand. */
    fun findByPath(relPath: String): MediaItem? {
        val wanted = relPath.trimStart('/')
        if (System.currentTimeMillis() - pathIndexAt.get() > 60_000L && pathIndex.isEmpty()) {
            rebuildPathIndex()
        }
        pathIndex[wanted]?.let { return findById(it) }
        val name = wanted.substringAfterLast('/')
        if (name.isEmpty()) return null
        val selection = "${MediaStore.Files.FileColumns.DISPLAY_NAME} = ?"
        return runCatching {
            resolver.query(collection, arrayOf(MediaStore.Files.FileColumns._ID), selection, arrayOf(name), null)
                ?.use { c ->
                    while (c.moveToNext()) {
                        val item = findById(c.getLong(0))
                        if (item != null && item.relPath.equals(wanted, ignoreCase = true)) return@use item
                    }
                    null
                }
        }.getOrNull()
    }

    private fun rebuildPathIndex() {
        runCatching {
            val projection = arrayOf(
                MediaStore.Files.FileColumns._ID,
                MediaStore.Files.FileColumns.DISPLAY_NAME,
                if (Build.VERSION.SDK_INT >= 29) MediaStore.Files.FileColumns.RELATIVE_PATH
                else MediaStore.Files.FileColumns.DATA,
            )
            resolver.query(collection, projection, null, null, null)?.use { c ->
                val pathIdx = 2
                while (c.moveToNext()) {
                    val raw = if (c.isNull(pathIdx)) "" else c.getString(pathIdx)
                    val rel = normalizePath(raw, c.getString(1) ?: "")
                    pathIndex[rel] = c.getLong(0)
                }
            }
            pathIndexAt.set(System.currentTimeMillis())
        }
    }

    /** Look up a single item by its MediaStore id. */
    fun findById(id: Long): MediaItem? {
        val projection = projection()
        val selection = "${MediaStore.Files.FileColumns._ID} = ?"
        return runCatching {
            resolver.query(collection, projection, selection, arrayOf(id.toString()), null)?.use { c ->
                if (!c.moveToFirst()) return@use null
                val idxName = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DISPLAY_NAME)
                val name = c.getString(idxName) ?: "unknown"
                val idxPath = c.getColumnIndex(
                    if (Build.VERSION.SDK_INT >= 29) MediaStore.Files.FileColumns.RELATIVE_PATH
                    else MediaStore.Files.FileColumns.DATA
                )
                val raw = if (idxPath >= 0 && !c.isNull(idxPath)) c.getString(idxPath) else ""
                val idxTaken = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DATE_TAKEN)
                val idxW = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.WIDTH)
                val idxH = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.HEIGHT)
                val idxType = c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.MEDIA_TYPE)
                MediaItem(
                    id = id,
                    name = name,
                    relPath = normalizePath(raw, name),
                    folder = folderOf(normalizePath(raw, name)),
                    size = c.getLong(c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.SIZE)),
                    modifiedSec = c.getLong(c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.DATE_MODIFIED)),
                    takenSec = if (c.isNull(idxTaken)) 0 else c.getLong(idxTaken) / 1000L,
                    mimeType = c.getString(c.getColumnIndexOrThrow(MediaStore.Files.FileColumns.MIME_TYPE)) ?: "",
                    width = if (c.isNull(idxW)) 0 else c.getInt(idxW),
                    height = if (c.isNull(idxH)) 0 else c.getInt(idxH),
                    isVideo = c.getInt(idxType) == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO,
                )
            }
        }.getOrNull()
    }

    /** Total item count matching the same filters used by [Query]. */
    fun count(since: Long, includeVideos: Boolean, folders: Set<String> = emptySet()): Int {
        val types = if (includeVideos) "1,3" else "1"
        val selection = StringBuilder("${MediaStore.Files.FileColumns.MEDIA_TYPE} IN ($types)")
            .append(" AND ${MediaStore.Files.FileColumns.SIZE} > 0")
        val args = mutableListOf<String>()
        if (since > 0) {
            selection.append(" AND ${MediaStore.Files.FileColumns.DATE_MODIFIED} >= ?")
            args += since.toString()
        }
        if (Build.VERSION.SDK_INT >= 30) {
            selection.append(" AND ${MediaStore.Files.FileColumns.IS_TRASHED} = 0")
        }
        appendFolderFilter(selection, args, folders)
        return runCatching {
            resolver.query(
                collection, arrayOf(MediaStore.Files.FileColumns._ID),
                selection.toString(), args.toTypedArray(), null,
            )?.use { c ->
                if (c.columnCount == 0) 0 else c.count
            } ?: 0
        }.getOrDefault(0)
    }

    fun invalidateCaches() {
        folderCache = null
        pathIndex.clear()
        pathIndexAt.set(0)
    }

    /**
     * Observe MediaStore so the UI reacts to new photos taken while the app is
     * running. Returns a disposer.
     */
    fun observeChanges(onChange: () -> Unit): () -> Unit {
        val handler = android.os.Handler(android.os.Looper.getMainLooper())
        val observer = object : android.database.ContentObserver(handler) {
            override fun onChange(selfChange: Boolean, uri: Uri?) {
                invalidateCaches()
                onChange()
            }
        }
        val registered = runCatching {
            resolver.registerContentObserver(
                MediaStore.Files.getContentUri("external"), true, observer,
            )
            true
        }.getOrElse { false }
        return {
            if (registered) runCatching { resolver.unregisterContentObserver(observer) }
        }
    }
}
