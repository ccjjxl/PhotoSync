package dev.photosync.ui

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.photosync.core.MediaItem
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

// ------------------------------------------------------------------ primitives

@Composable
fun Card2(
    modifier: Modifier = Modifier,
    color: Color = MaterialTheme.colorScheme.surfaceContainer,
    border: Boolean = true,
    content: @Composable () -> Unit,
) {
    Box(
        modifier
            .clip(RoundedCornerShape(20.dp))
            .background(color)
            .then(
                if (border) Modifier.border(
                    1.dp,
                    MaterialTheme.colorScheme.outlineVariant,
                    RoundedCornerShape(20.dp),
                ) else Modifier,
            ),
    ) { content() }
}

@Composable
fun SectionTitle(text: String, trailing: (@Composable () -> Unit)? = null) {
    Row(
        Modifier.fillMaxWidth().padding(start = 4.dp, end = 4.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        trailing?.invoke()
    }
}

@Composable
fun StatTile(label: String, value: String, accent: Color, modifier: Modifier = Modifier) {
    Column(
        modifier
            .clip(RoundedCornerShape(16.dp))
            .background(MaterialTheme.colorScheme.surfaceContainer)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(16.dp))
            .padding(vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            value,
            style = MaterialTheme.typography.titleLarge.copy(fontWeight = FontWeight.SemiBold),
            color = accent,
        )
        Spacer(Modifier.height(2.dp))
        Text(
            label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** Small animated "live" dot. */
@Composable
fun PulseDot(color: Color, active: Boolean, size: Int = 10) {
    val alpha by animateFloatAsState(if (active) 1f else 0.35f, label = "dot")
    Box(Modifier.size(size.dp), contentAlignment = Alignment.Center) {
        if (active) {
            Box(
                Modifier
                    .size(size.dp)
                    .clip(RoundedCornerShape(50))
                    .background(color.copy(alpha = 0.18f + 0.12f * alpha)),
            )
        }
        Box(
            Modifier
                .size((size * 0.62f).dp)
                .clip(RoundedCornerShape(50))
                .background(color.copy(alpha = alpha)),
        )
    }
}

/** Circular sync glyph drawn by hand so we don't need an icon dependency. */
@Composable
fun SyncGlyph(modifier: Modifier = Modifier, color: Color = Color.White) {
    androidx.compose.foundation.Canvas(modifier) {
        val w = this.size.width
        val h = this.size.height
        val r = minOf(w, h) / 2f * 0.62f
        val cx = w / 2f
        val cy = h / 2f
        val stroke = minOf(w, h) * 0.11f
        val paint = androidx.compose.ui.graphics.drawscope.Stroke(
            width = stroke,
            cap = androidx.compose.ui.graphics.StrokeCap.Round,
        )
        // two facing arcs
        drawArc(
            color = color, startAngle = 200f, sweepAngle = 130f, useCenter = false,
            topLeft = androidx.compose.ui.geometry.Offset(cx - r, cy - r),
            size = androidx.compose.ui.geometry.Size(r * 2, r * 2), style = paint,
        )
        drawArc(
            color = color.copy(alpha = 0.55f), startAngle = 20f, sweepAngle = 130f, useCenter = false,
            topLeft = androidx.compose.ui.geometry.Offset(cx - r, cy - r),
            size = androidx.compose.ui.geometry.Size(r * 2, r * 2), style = paint,
        )
        // arrow heads
        val head = stroke * 1.5f
        drawPath(
            androidx.compose.ui.graphics.Path().apply {
                moveTo(cx + r * 0.98f, cy - r * 0.42f)
                lineTo(cx + r * 1.32f, cy - r * 0.12f)
                lineTo(cx + r * 0.78f, cy + r * 0.10f)
                close()
            },
            color = color.copy(alpha = 0.9f),
        )
        drawPath(
            androidx.compose.ui.graphics.Path().apply {
                moveTo(cx - r * 0.98f, cy + r * 0.42f)
                lineTo(cx - r * 1.32f, cy + r * 0.12f)
                lineTo(cx - r * 0.78f, cy - r * 0.10f)
                close()
            },
            color = color.copy(alpha = 0.45f),
        )
    }
}

// ------------------------------------------------------------------ thumbnails

/**
 * Decodes a cached JPEG thumbnail off the main thread and keeps a small
 * in-memory bitmap LRU so scrolling the recent grid stays smooth.
 */
private object ThumbMem {
    private val cache = object : android.util.LruCache<String, ImageBitmap>(12 * 1024 * 1024) {
        override fun sizeOf(key: String, value: ImageBitmap): Int = value.width * value.height * 4
    }

    fun get(key: String): ImageBitmap? = cache.get(key)
    fun put(key: String, bitmap: ImageBitmap) = cache.put(key, bitmap)
}

@Composable
fun ThumbImage(
    item: MediaItem,
    size: androidx.compose.ui.unit.Dp,
    modifier: Modifier = Modifier,
    corner: androidx.compose.ui.unit.Dp = 14.dp,
) {
    val key = item.cacheKey
    var bmp by remember(key) { mutableStateOf(ThumbMem.get(key)) }
    LaunchedEffect(key) {
        if (bmp != null) return@LaunchedEffect
        val graph = dev.photosync.AppGraph.peek() ?: return@LaunchedEffect
        bmp = withContext(Dispatchers.IO) {
            runCatching {
                val bytes = graph.thumbnails.getBlocking(item, 320) ?: return@runCatching null
                BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
            }.getOrNull()
        }
        bmp?.let { ThumbMem.put(key, it) }
    }

    Box(
        modifier
            .size(size)
            .clip(RoundedCornerShape(corner))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh),
        contentAlignment = Alignment.Center,
    ) {
        val b = bmp
        if (b != null) {
            Image(b, null, modifier = Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        } else {
            SyncGlyph(Modifier.size(size * 0.32f), MaterialTheme.colorScheme.outline.copy(alpha = 0.6f))
        }
        if (item.isVideo) {
            Box(
                Modifier
                    .align(Alignment.BottomEnd)
                    .padding(4.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(Color.Black.copy(alpha = 0.55f))
                    .padding(horizontal = 5.dp, vertical = 1.dp),
            ) {
                Text("视频", fontSize = 9.sp, color = Color.White)
            }
        }
    }
}

// ------------------------------------------------------------------ formatting

fun humanBytes(b: Long): String = when {
    b >= 1L shl 30 -> "%.2f GB".format(b / (1L shl 30).toDouble())
    b >= 1L shl 20 -> "%.1f MB".format(b / (1L shl 20).toDouble())
    b >= 1L shl 10 -> "%.0f KB".format(b / (1L shl 10).toDouble())
    else -> "$b B"
}

fun humanCount(n: Int): String = if (n >= 10_000) "%.1f万".format(n / 10_000.0) else n.toString()

val Mono = FontFamily.Monospace

@Composable
fun GradientHero(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Box(
        modifier
            .clip(RoundedCornerShape(24.dp))
            .background(
                Brush.linearGradient(
                    listOf(Color(0xFF2C6BFF), Color(0xFF4F8CFF), Color(0xFF6FA8FF)),
                ),
            ),
    ) { content() }
}

@Composable
fun Chip(text: String, modifier: Modifier = Modifier, onClick: (() -> Unit)? = null) {
    Box(
        modifier
            .clip(RoundedCornerShape(50))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(50))
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(horizontal = 11.dp, vertical = 6.dp),
    ) {
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
