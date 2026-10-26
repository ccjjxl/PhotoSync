package dev.photosync.server

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.os.Build
import android.util.LruCache
import dev.photosync.core.MediaItem
import dev.photosync.core.MediaRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicLong

/**
 * Generates and caches thumbnails. Two-stage lookup:
 *  1. in-memory LRU of decoded bitmaps (fast path for browsers scrolling a grid)
 *  2. on-disk JPEG cache keyed by id+size+mtime (survives restarts, ~25x smaller)
 */
class ThumbnailProvider(
    private val context: Context,
    private val repository: MediaRepository,
    cacheDir: File,
) {
    private val dir = File(cacheDir, "thumbs").apply { mkdirs() }
    private val memCache = object : LruCache<String, ByteArray>(24 * 1024 * 1024) {
        override fun sizeOf(key: String, value: ByteArray): Int = value.size
    }
    private val executor = Executors.newFixedThreadPool(4) { r ->
        Thread(r, "photosync-thumb").apply { isDaemon = true }
    }
    private val hits = AtomicLong(0)
    private val misses = AtomicLong(0)

    fun stats(): Pair<Long, Long> = hits.get() to misses.get()

    /** Blocking variant for callers already on a background thread. */
    fun getBlocking(item: MediaItem, size: Int): ByteArray? = kotlinx.coroutines.runBlocking {
        get(item, size)
    }

    suspend fun get(item: MediaItem, size: Int): ByteArray? = withContext(Dispatchers.IO) {
        val bounded = size.coerceIn(96, 1024)
        val key = "${item.cacheKey}-$bounded"
        memCache.get(key)?.let { hits.incrementAndGet(); return@withContext it }

        val diskFile = File(dir, "$key.jpg")
        if (diskFile.exists() && diskFile.length() > 0) {
            hits.incrementAndGet()
            val bytes = runCatching { diskFile.readBytes() }.getOrNull()
            if (bytes != null) { memCache.put(key, bytes); return@withContext bytes }
        }

        misses.incrementAndGet()
        val bytes = generate(item, bounded) ?: return@withContext null
        memCache.put(key, bytes)
        runCatching { diskFile.writeBytes(bytes) }
        bytes
    }

    private fun generate(item: MediaItem, target: Int): ByteArray? = runCatching {
        if (item.isVideo) generateVideoThumb(item, target) else generateImageThumb(item, target)
    }.onFailure {
        android.util.Log.w("PhotoSync", "thumb failed for ${item.relPath}: ${it.message}", it)
    }.getOrNull()

    private fun generateImageThumb(item: MediaItem, target: Int): ByteArray? {
        val uri = repository.contentUri(item.id)
        // Decode through a byte buffer: MediaStore descriptors are not always
        // seekable, which makes BitmapFactory's two-pass decode unreliable.
        val raw = runCatching {
            context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
        }.getOrNull()
        if (raw == null || raw.isEmpty()) {
            android.util.Log.w("PhotoSync", "cannot read ${item.relPath}")
            return null
        }

        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(raw, 0, raw.size, bounds)
        if (bounds.outWidth <= 0) {
            android.util.Log.w("PhotoSync", "decodeBounds failed for ${item.relPath}")
            return null
        }

        var sample = 1
        while ((bounds.outWidth / sample) > target * 2 && (bounds.outHeight / sample) > target * 2) {
            sample *= 2
        }

        val opts = BitmapFactory.Options().apply {
            inSampleSize = sample
            inPreferredConfig = Bitmap.Config.ARGB_8888
            inDither = false
        }
        val decoded = BitmapFactory.decodeByteArray(raw, 0, raw.size, opts)
        if (decoded == null) {
            android.util.Log.w("PhotoSync", "decode failed for ${item.relPath} (${raw.size} bytes)")
            return null
        }

        val scale = target.toFloat() / maxOf(decoded.width, decoded.height)
        val scaled = if (scale < 1f) {
            Bitmap.createScaledBitmap(
                decoded,
                (decoded.width * scale).toInt().coerceAtLeast(1),
                (decoded.height * scale).toInt().coerceAtLeast(1),
                true,
            ).also { if (it !== decoded) decoded.recycle() }
        } else decoded

        return compressJpeg(scaled, 82).also { scaled.recycle() }
    }

    private fun generateVideoThumb(item: MediaItem, target: Int): ByteArray? {
        val retriever = MediaMetadataRetriever()
        return try {
            retriever.setDataSource(context, repository.contentUri(item.id))
            val frame = if (Build.VERSION.SDK_INT >= 27) {
                retriever.getScaledFrameAtTime(1_000_000, MediaMetadataRetriever.OPTION_CLOSEST_SYNC, target, target)
            } else {
                retriever.getFrameAtTime(1_000_000, MediaMetadataRetriever.OPTION_CLOSEST_SYNC)
            } ?: return null
            val bitmap = if (maxOf(frame.width, frame.height) > target) {
                val s = target.toFloat() / maxOf(frame.width, frame.height)
                Bitmap.createScaledBitmap(
                    frame,
                    (frame.width * s).toInt().coerceAtLeast(1),
                    (frame.height * s).toInt().coerceAtLeast(1),
                    true,
                ).also { if (it !== frame) frame.recycle() }
            } else frame
            compressJpeg(bitmap, 78).also { bitmap.recycle() }
        } catch (_: Throwable) {
            null
        } finally {
            runCatching { retriever.release() }
        }
    }

    private fun compressJpeg(bitmap: Bitmap, quality: Int): ByteArray {
        val out = java.io.ByteArrayOutputStream(32 * 1024)
        bitmap.compress(Bitmap.CompressFormat.JPEG, quality, out)
        return out.toByteArray()
    }

    fun clearDiskCache() {
        memCache.evictAll()
        runCatching { dir.listFiles()?.forEach { it.delete() } }
    }

    fun diskCacheSize(): Long = runCatching {
        dir.listFiles()?.sumOf { it.length() } ?: 0L
    }.getOrDefault(0L)

    fun pruneDiskCache(maxBytes: Long = 512L * 1024 * 1024) {
        runCatching {
            val files = dir.listFiles()?.sortedBy { it.lastModified() } ?: return
            var total = files.sumOf { it.length() }
            for (f in files) {
                if (total <= maxBytes) break
                total -= f.length()
                f.delete()
            }
        }
    }

    fun shutdown() = executor.shutdownNow()
}
