package dev.photosync.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.photosync.core.MediaRepository
import dev.photosync.util.Net

@Composable
fun HomeScreen(
    state: UiState,
    onToggleServer: () -> Unit,
    onCopy: (String) -> Unit,
    onRefreshLibrary: () -> Unit,
    onGrantPermission: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenSheet: (Sheet) -> Unit,
    onOpenUrl: (String) -> Unit,
) {
    val scroll = rememberScrollState()

    if (state.permission == MediaRepository.PermissionState.Denied) {
        PermissionGate(onGrantPermission, onOpenSettings)
        return
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(scroll)
            .padding(horizontal = 18.dp),
    ) {
        Spacer(Modifier.height(52.dp))

        // ---------------------------------------------------------- header
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Column(Modifier.weight(1f)) {
                Text(
                    "PhotoSync",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    buildString {
                        append(state.deviceName.ifEmpty { Net.deviceName() })
                        if (state.deviceId.isNotEmpty()) append(" · ${state.deviceId}")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                PulseDot(
                    color = if (state.running) MaterialTheme.colorScheme.secondary
                    else MaterialTheme.colorScheme.outline,
                    active = state.running,
                )
                Spacer(Modifier.width(6.dp))
                Text(
                    if (state.running) "运行中" else "已停止",
                    style = MaterialTheme.typography.labelLarge,
                    color = if (state.running) MaterialTheme.colorScheme.secondary
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.width(10.dp))
                RoundIcon(Icons.Filled.Settings) { onOpenSheet(Sheet.Settings) }
            }
        }

        Spacer(Modifier.height(18.dp))

        // ---------------------------------------------------------- hero
        HeroCard(
            state = state,
            onToggleServer = onToggleServer,
            onCopy = onCopy,
            onOpenUrl = onOpenUrl,
            onOpenHelp = { onOpenSheet(Sheet.Help) },
        )

        Spacer(Modifier.height(16.dp))

        // ---------------------------------------------------------- stats
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            StatTile("照片", humanCount(state.photos), MaterialTheme.colorScheme.primary, Modifier.weight(1f))
            StatTile("视频", humanCount(state.videos), MaterialTheme.colorScheme.tertiary, Modifier.weight(1f))
            StatTile("占用", humanBytes(state.bytes), MaterialTheme.colorScheme.secondary, Modifier.weight(1f))
        }

        Spacer(Modifier.height(22.dp))

        // ---------------------------------------------------------- recent
        if (state.recent.isNotEmpty()) {
            SectionTitle("最近拍摄") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (state.loadingLibrary) {
                        Text("读取中…", style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Spacer(Modifier.width(8.dp))
                    }
                    RoundIcon(Icons.Filled.Refresh) { onRefreshLibrary() }
                }
            }
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(state.recent, key = { it.id }) { item ->
                    ThumbImage(item, 84.dp)
                }
            }
            Spacer(Modifier.height(22.dp))
        }

        // ---------------------------------------------------------- folders
        if (state.folders.isNotEmpty()) {
            val filtering = state.selectedFolders.isNotEmpty()
            SectionTitle("同步相册") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        if (filtering) "已选 ${state.selectedFolders.size}/${state.folders.size}"
                        else "全部 ${state.folders.size} 个",
                        style = MaterialTheme.typography.labelMedium,
                        color = if (filtering) MaterialTheme.colorScheme.primary
                        else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.width(8.dp))
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(50))
                            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                            .border(
                                1.dp,
                                MaterialTheme.colorScheme.outlineVariant,
                                RoundedCornerShape(50),
                            )
                            .clickable { onOpenSheet(Sheet.Folders) }
                            .padding(horizontal = 12.dp, vertical = 6.dp),
                    ) {
                        Text(
                            "选择",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
            }
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                val shown = if (filtering) {
                    state.folders.filter { it.first in state.selectedFolders }
                } else {
                    state.folders
                }
                shown.take(6).forEach { (path, count) ->
                    FolderRow(path, count, enabled = true)
                }
                val hidden = state.folders.size - shown.take(6).size
                if (hidden > 0) {
                    Text(
                        if (filtering) "另有 $hidden 个相册未选择同步…"
                        else "另有 $hidden 个相册…",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier
                            .padding(start = 4.dp, top = 2.dp)
                            .clickable { onOpenSheet(Sheet.Folders) },
                    )
                }
            }
            Spacer(Modifier.height(22.dp))
        }

        // ---------------------------------------------------------- transfer
        if (state.running) {
            SectionTitle("本次会话")
            Card2(Modifier.fillMaxWidth()) {
                Row(
                    Modifier.fillMaxWidth().padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            humanBytes(state.servedBytes),
                            style = MaterialTheme.typography.titleLarge,
                            color = MaterialTheme.colorScheme.secondary,
                        )
                        Text(
                            "已传输 ${state.servedFiles} 个文件",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Column(horizontalAlignment = Alignment.End) {
                        Text(
                            "缩略图缓存",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Text(
                            humanBytes(state.thumbCacheBytes),
                            style = MaterialTheme.typography.titleMedium,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
            }
            Spacer(Modifier.height(22.dp))
        }

        state.error?.let {
            Card2(Modifier.fillMaxWidth(), color = MaterialTheme.colorScheme.errorContainer) {
                Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    Spacer(Modifier.width(10.dp))
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .background(MaterialTheme.colorScheme.surface.copy(alpha = 0.6f))
                            .clickable { onOpenSheet(Sheet.Settings) }
                            .padding(horizontal = 12.dp, vertical = 7.dp),
                    ) {
                        Text("改端口", style = MaterialTheme.typography.labelMedium)
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
        }

        Text(
            "在电脑上运行 sync-client，它会自动发现本机并增量传输照片。\n也可用浏览器直接打开上方地址浏览、下载。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        Spacer(Modifier.height(40.dp))
    }
}

@Composable
private fun RoundIcon(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    onClick: () -> Unit,
) {
    Box(
        Modifier
            .size(36.dp)
            .clip(RoundedCornerShape(50))
            .background(MaterialTheme.colorScheme.surfaceContainer)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(50))
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(17.dp))
    }
}

@Composable
private fun HeroCard(
    state: UiState,
    onToggleServer: () -> Unit,
    onCopy: (String) -> Unit,
    onOpenUrl: (String) -> Unit,
    onOpenHelp: () -> Unit,
) {
    val primaryUrl = state.urls.firstOrNull()
    val glow by animateFloatAsState(if (state.running) 1f else 0.35f, label = "glow")

    GradientHero(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(20.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(
                        if (state.running) "同步服务已开启" else "同步服务已关闭",
                        style = MaterialTheme.typography.titleLarge,
                        color = Color.White,
                    )
                    Spacer(Modifier.height(3.dp))
                    Text(
                        if (state.running) "等待电脑端连接 · 端口 ${state.port}"
                        else "点击右侧按钮开启局域网共享",
                        style = MaterialTheme.typography.bodySmall,
                        color = Color.White.copy(alpha = 0.82f),
                    )
                }
                Spacer(Modifier.width(14.dp))
                ToggleButton(running = state.running, onClick = onToggleServer, glow = glow)
            }

            if (state.running && primaryUrl != null) {
                Spacer(Modifier.height(18.dp))
                Box(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(14.dp))
                        .background(Color.White.copy(alpha = 0.16f))
                        .clickable { onCopy(primaryUrl) }
                        .padding(horizontal = 14.dp, vertical = 12.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(
                                primaryUrl.replace("http://", ""),
                                style = MaterialTheme.typography.titleMedium.copy(fontFamily = Mono),
                                color = Color.White,
                            )
                            Text(
                                "点击复制地址",
                                style = MaterialTheme.typography.labelSmall,
                                color = Color.White.copy(alpha = 0.7f),
                            )
                        }
                        SmallPill("浏览器打开") { onOpenUrl(primaryUrl) }
                    }
                }

                state.ifaces.drop(1).forEach { a ->
                    Spacer(Modifier.height(6.dp))
                    Text(
                        "其他网卡 ${a.iface}: ${a.ip}:${state.port}",
                        style = MaterialTheme.typography.labelSmall,
                        color = Color.White.copy(alpha = 0.62f),
                    )
                }

                Spacer(Modifier.height(14.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        "配对码",
                        style = MaterialTheme.typography.labelMedium,
                        color = Color.White.copy(alpha = 0.72f),
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(
                        state.settings?.pairingCode ?: "------",
                        style = MaterialTheme.typography.titleMedium.copy(
                            fontFamily = Mono,
                            letterSpacing = 3.sp,
                        ),
                        color = Color.White,
                    )
                    if (state.settings?.openAccess == true) {
                        Spacer(Modifier.width(10.dp))
                        Text(
                            "免配对",
                            style = MaterialTheme.typography.labelSmall,
                            color = Color(0xFFFFE08A),
                        )
                    }
                    Spacer(Modifier.weight(1f))
                    SmallPill("说明") { onOpenHelp() }
                }
            } else if (!state.running) {
                Spacer(Modifier.height(16.dp))
                Text(
                    "开启后，同一 Wi-Fi 下的 Linux / Windows 电脑即可发现设备并同步照片，" +
                        "支持全量与增量传输、断点续传。",
                    style = MaterialTheme.typography.bodySmall,
                    color = Color.White.copy(alpha = 0.78f),
                )
            }
        }
    }
}

@Composable
private fun ToggleButton(running: Boolean, onClick: () -> Unit, glow: Float) {
    val bg = if (running) Color.White.copy(alpha = 0.2f) else Color.White
    val fg = if (running) Color.White else Color(0xFF2C6BFF)
    Box(
        Modifier
            .size(64.dp)
            .clip(RoundedCornerShape(50))
            .background(bg)
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        if (running) {
            Box(
                Modifier
                    .size(20.dp)
                    .clip(RoundedCornerShape(4.dp))
                    .background(Color.White.copy(alpha = 0.55f + 0.45f * glow)),
            )
        } else {
            SyncGlyph(Modifier.size(34.dp), fg)
        }
    }
}

@Composable
private fun SmallPill(text: String, onClick: () -> Unit) {
    Box(
        Modifier
            .clip(RoundedCornerShape(50))
            .background(Color.White.copy(alpha = 0.2f))
            .clickable { onClick() }
            .padding(horizontal = 11.dp, vertical = 6.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium, color = Color.White)
    }
}

@Composable
private fun FolderRow(path: String, count: Int, enabled: Boolean = true) {
    Card2(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier
                    .size(34.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(MaterialTheme.colorScheme.primaryContainer),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    path.takeLast(1).uppercase(),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onPrimaryContainer,
                    fontWeight = FontWeight.Bold,
                )
            }
            Spacer(Modifier.width(12.dp))
            Text(
                path,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                "$count",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
fun PermissionGate(onGrant: () -> Unit, onOpenSettings: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(28.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        SyncGlyph(Modifier.size(64.dp), MaterialTheme.colorScheme.primary)
        Spacer(Modifier.height(24.dp))
        Text("需要照片访问权限", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))
        Text(
            "PhotoSync 只读取你的照片与视频，用于同步到电脑；不会上传到任何服务器。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Spacer(Modifier.height(26.dp))
        PrimaryButton("授予权限", onGrant)
        Spacer(Modifier.height(10.dp))
        TextButton2("已拒绝？前往系统设置", onOpenSettings)
    }
}

@Composable
fun PrimaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MaterialTheme.colorScheme.primary)
            .clickable { onClick() }
            .padding(vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimary)
    }
}

@Composable
fun TextButton2(text: String, onClick: () -> Unit) {
    Box(
        Modifier
            .clip(RoundedCornerShape(12.dp))
            .clickable { onClick() }
            .padding(horizontal = 14.dp, vertical = 10.dp),
    ) {
        Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
    }
}
