package dev.photosync.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * Manual folder picker.
 *
 * The selection is a whitelist stored on the phone: the desktop client and the
 * web gallery both only ever see enabled folders, so a stray `--folder` cannot
 * escape it.
 */
@Composable
fun FolderPickerContent(state: UiState, vm: MainViewModel) {
    val all = state.allFolders
    val selected = state.selectedFolders
    val filtering = selected.isNotEmpty()
    var query by remember { mutableStateOf("") }

    Text("选择同步相册", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
    Spacer(Modifier.height(6.dp))
    Text(
        if (filtering) "只同步勾选的 ${selected.size} 个相册"
        else "当前同步全部 ${all.size} 个相册",
        style = MaterialTheme.typography.bodyMedium,
        color = if (filtering) MaterialTheme.colorScheme.primary
        else MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(14.dp))

    // ---------------------------------------------------------------- actions
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        Box(
            Modifier
                .weight(1f)
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.primary)
                .clickable { vm.selectAllFolders() }
                .padding(vertical = 12.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text("全选（同步全部）", style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onPrimary)
        }
        Box(
            Modifier
                .weight(1f)
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(12.dp))
                .clickable { vm.clearFolderSelection() }
                .padding(vertical = 12.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text("清空选择", style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }

    Spacer(Modifier.height(16.dp))

    // ---------------------------------------------------------------- list
    val visible = if (query.isBlank()) all
    else all.filter { it.first.contains(query, ignoreCase = true) }

    if (visible.isEmpty()) {
        Text(
            if (all.isEmpty()) "还没有读取到相册，请授予权限后重试。" else "没有匹配「$query」的相册",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(vertical = 20.dp),
        )
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        visible.forEach { (path, count) ->
            val checked = !filtering || path in selected
            FolderCheckRow(
                path = path,
                count = count,
                checked = checked,
                onToggle = {
                    // When nothing is filtered, everything is implicitly on; the
                    // first un-check must pin the rest so the state is explicit.
                    vm.toggleFolder(path)
                },
                onOnly = { vm.toggleFolderOnly(path) },
            )
        }
    }

    Spacer(Modifier.height(18.dp))
    Card2(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(14.dp)) {
            Text(
                "说明",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                "· 被排除的相册不会出现在电脑端和网页相册里，也不会被传输。\n" +
                    "· 电脑端按相同目录结构保存，未勾选的相册不会创建。\n" +
                    "· 修改后立即生效，无需重启服务。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    Spacer(Modifier.height(10.dp))
}

@Composable
private fun FolderCheckRow(
    path: String,
    count: Int,
    checked: Boolean,
    onToggle: () -> Unit,
    onOnly: () -> Unit,
) {
    val bg by animateColorAsState(
        if (checked) MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.45f)
        else MaterialTheme.colorScheme.surfaceContainer,
        label = "foldbg",
    )
    val borderColor by animateColorAsState(
        if (checked) MaterialTheme.colorScheme.primary.copy(alpha = 0.55f)
        else MaterialTheme.colorScheme.outlineVariant,
        label = "foldborder",
    )

    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .border(1.dp, borderColor, RoundedCornerShape(14.dp))
            .clickable { onToggle() }
            .padding(start = 12.dp, end = 8.dp, top = 11.dp, bottom = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CheckMark(checked)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(
                path,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                "$count 个",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Box(
            Modifier
                .clip(RoundedCornerShape(9.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .clickable { onOnly() }
                .padding(horizontal = 10.dp, vertical = 6.dp),
        ) {
            Text(
                "仅此项",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun CheckMark(checked: Boolean) {
    val fill by animateColorAsState(
        if (checked) MaterialTheme.colorScheme.primary else Color.Transparent,
        label = "checkfill",
    )
    Box(
        Modifier
            .size(22.dp)
            .clip(RoundedCornerShape(7.dp))
            .background(fill)
            .border(
                1.5.dp,
                if (checked) MaterialTheme.colorScheme.primary
                else MaterialTheme.colorScheme.outline,
                RoundedCornerShape(7.dp),
            ),
        contentAlignment = Alignment.Center,
    ) {
        if (checked) {
            androidx.compose.foundation.Canvas(Modifier.size(13.dp)) {
                val w = size.width
                val h = size.height
                val path = androidx.compose.ui.graphics.Path().apply {
                    moveTo(w * 0.14f, h * 0.52f)
                    lineTo(w * 0.40f, h * 0.78f)
                    lineTo(w * 0.88f, h * 0.24f)
                }
                drawPath(
                    path,
                    color = Color.White,
                    style = androidx.compose.ui.graphics.drawscope.Stroke(
                        width = w * 0.18f,
                        cap = androidx.compose.ui.graphics.StrokeCap.Round,
                        join = androidx.compose.ui.graphics.StrokeJoin.Round,
                    ),
                )
            }
        }
    }
}
