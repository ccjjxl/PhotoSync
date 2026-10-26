package dev.photosync.core

/**
 * A single media entry as exposed to PC clients.
 * [relPath] is the on-device path relative to the storage root, e.g. "DCIM/Camera/IMG_0001.jpg".
 */
data class MediaItem(
    val id: Long,
    val name: String,
    val relPath: String,
    val folder: String,
    val size: Long,
    val modifiedSec: Long,
    val takenSec: Long,
    val mimeType: String,
    val width: Int,
    val height: Int,
    val isVideo: Boolean,
) {
    val cacheKey: String
        get() = "$id-$size-$modifiedSec"
}
