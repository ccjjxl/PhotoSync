package dev.photosync.ui

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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.photosync.core.MediaRepository

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsSheet(
    sheet: Sheet,
    state: UiState,
    onDismiss: () -> Unit,
    vm: MainViewModel,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = MaterialTheme.colorScheme.surface,
        dragHandle = {
            Box(Modifier.fillMaxWidth().padding(vertical = 10.dp), contentAlignment = Alignment.Center) {
                Box(
                    Modifier
                        .size(width = 36.dp, height = 4.dp)
                        .clip(RoundedCornerShape(50))
                        .background(MaterialTheme.colorScheme.outline),
                )
            }
        },
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = 20.dp)
                .padding(bottom = 28.dp),
        ) {
            when (sheet) {
                Sheet.Settings -> SettingsContent(state, vm)
                Sheet.Help -> HelpContent(state)
                Sheet.Library -> LibraryContent(state, vm)
            }
        }
    }
}

@Composable
private fun SettingsContent(state: UiState, vm: MainViewModel) {
    val snap = state.settings
    var portText by remember(snap?.port) { mutableStateOf((snap?.port ?: 8737).toString()) }

    Text("同步设置", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
    Spacer(Modifier.height(20.dp))

    // ---------------------------------------------------------- device identity
    var nameText by remember(state.deviceName) { mutableStateOf(state.deviceName) }
    Column(Modifier.padding(bottom = 18.dp)) {
        Text("设备名称", style = MaterialTheme.typography.titleMedium)
        Text(
            "电脑端按此名称区分不同手机，每台手机的相册会存到独立文件夹",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = nameText,
                onValueChange = { v -> nameText = v.take(40) },
                singleLine = true,
                placeholder = { Text("例如 我的Pixel", style = MaterialTheme.typography.bodyMedium) },
                modifier = Modifier.weight(1f),
                shape = RoundedCornerShape(12.dp),
                textStyle = MaterialTheme.typography.bodyMedium,
            )
            Spacer(Modifier.width(10.dp))
            val canApply = nameText.trim().isNotEmpty() && nameText.trim() != state.deviceName
            Box(
                Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(
                        if (canApply) MaterialTheme.colorScheme.primary
                        else MaterialTheme.colorScheme.surfaceContainerHigh,
                    )
                    .clickable(enabled = canApply) { vm.setDeviceName(nameText) }
                    .padding(horizontal = 18.dp, vertical = 13.dp),
            ) {
                Text(
                    "保存",
                    style = MaterialTheme.typography.labelLarge,
                    color = if (canApply) MaterialTheme.colorScheme.onPrimary
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Spacer(Modifier.height(8.dp))
        Text(
            "设备 ID ${state.deviceId}  ·  电脑端文件夹 ${state.folderName}",
            style = MaterialTheme.typography.labelSmall.copy(fontFamily = Mono),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }

    // ---------------------------------------------------------- port
    Column(Modifier.padding(bottom = 18.dp)) {
        Text("端口", style = MaterialTheme.typography.titleMedium)
        Text(
            "电脑端通过该端口访问，默认 8737；修改后服务会自动重启",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = portText,
                onValueChange = { v -> portText = v.filter { it.isDigit() }.take(5) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                placeholder = { Text("8737", style = MaterialTheme.typography.bodyMedium) },
                modifier = Modifier.weight(1f),
                shape = RoundedCornerShape(12.dp),
                textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = Mono),
            )
            Spacer(Modifier.width(10.dp))
            val canApply = portText.toIntOrNull()?.let { it in 1024..65535 } == true &&
                portText.toIntOrNull() != snap?.port
            Box(
                Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(
                        if (canApply) MaterialTheme.colorScheme.primary
                        else MaterialTheme.colorScheme.surfaceContainerHigh,
                    )
                    .clickable(enabled = canApply) {
                        portText.toIntOrNull()?.let { vm.setPort(it) }
                    }
                    .padding(horizontal = 18.dp, vertical = 13.dp),
            ) {
                Text(
                    "应用",
                    style = MaterialTheme.typography.labelLarge,
                    color = if (canApply) MaterialTheme.colorScheme.onPrimary
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }

    // ---------------------------------------------------------- videos
    SwitchBlock(
        title = "同步视频",
        subtitle = "关闭后只同步照片，可显著加快首次全量同步",
        checked = snap?.includeVideos ?: true,
        onCheckedChange = vm::setIncludeVideos,
    )

    // ---------------------------------------------------------- access
    SwitchBlock(
        title = "免配对访问",
        subtitle = "开启后任意设备无需配对码即可读取数据（仅建议在可信网络中开启）",
        checked = snap?.openAccess ?: false,
        onCheckedChange = vm::setOpenAccess,
    )

    Spacer(Modifier.height(10.dp))
    Text("安全", style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant)
    Spacer(Modifier.height(10.dp))

    Card2(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("配对码", style = MaterialTheme.typography.bodyMedium)
                    Text(
                        snap?.pairingCode ?: "------",
                        style = MaterialTheme.typography.headlineMedium.copy(
                            fontFamily = Mono, letterSpacing = 6.sp,
                        ),
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
                Box(
                    Modifier
                        .clip(RoundedCornerShape(11.dp))
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                        .clickable { vm.rotateCode() }
                        .padding(horizontal = 13.dp, vertical = 9.dp),
                ) {
                    Text("更换", style = MaterialTheme.typography.labelMedium)
                }
            }
            Spacer(Modifier.height(14.dp))
            Text(
                "在电脑端输入配对码即完成连接；令牌已内置加密随机生成。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            Box(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(11.dp))
                    .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(11.dp))
                    .clickable { vm.rotateToken() }
                    .padding(vertical = 11.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text("重置访问令牌", style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.error)
            }
        }
    }

    Spacer(Modifier.height(18.dp))
    Text("存储", style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant)
    Spacer(Modifier.height(10.dp))

    Card2(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("缩略图缓存", style = MaterialTheme.typography.bodyMedium)
                Text(
                    humanBytes(state.thumbCacheBytes) + " · 仅用于加速浏览，可随时清理",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Box(
                Modifier
                    .clip(RoundedCornerShape(11.dp))
                    .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                    .clickable { vm.clearThumbCache() }
                    .padding(horizontal = 13.dp, vertical = 9.dp),
            ) {
                Text("清理", style = MaterialTheme.typography.labelMedium)
            }
        }
    }

    Spacer(Modifier.height(18.dp))
    Spacer(Modifier.height(6.dp))
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(13.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .clickable { vm.openPermissionSettings() }
            .padding(vertical = 13.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text("系统权限设置", style = MaterialTheme.typography.labelLarge)
    }
}

@Composable
private fun HelpContent(state: UiState) {
    Text("如何使用", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
    Spacer(Modifier.height(18.dp))

    StepRow("1", "在手机开启同步服务", "点击主页的圆形按钮，保持 PhotoSync 通知常驻（前台服务），退出应用也不会中断。")
    StepRow("2", "确保同一 Wi-Fi", "手机与电脑需要在同一局域网。家用路由器下通常可直接自动发现。")
    StepRow("3", "电脑端运行客户端", "命令：sync-client --out ~/Pictures/Android\n首次运行输入配对码，之后自动记住令牌。")
    StepRow("4", "增量同步", "客户端记录本地已同步清单，仅传输新增或修改的照片，支持断点续传与多线程加速。")

    Spacer(Modifier.height(8.dp))
    Card2(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("当前地址", style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(6.dp))
            if (state.urls.isEmpty()) {
                Text("未连接 Wi-Fi", style = MaterialTheme.typography.bodyMedium)
            } else {
                state.urls.forEach { url ->
                    Text(url, style = MaterialTheme.typography.bodyMedium.copy(fontFamily = Mono))
                }
            }
            if (state.settings?.openAccess != true) {
                Spacer(Modifier.height(10.dp))
                Text(
                    "配对码：${state.settings?.pairingCode ?: ""}（30 秒内有效输入）",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }

    Spacer(Modifier.height(16.dp))
    Card2(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("API（供自建脚本使用）", style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(8.dp))
            listOf(
                "GET /api/info" to "设备与库概况",
                "POST /api/pair {code}" to "换取 token",
                "GET /api/list?since=ts" to "增量列表",
                "GET /api/file?id=N" to "原始文件（支持 Range）",
                "GET /api/thumb?id=N&size=320" to "缩略图",
            ).forEach { (ep, desc) ->
                Row(Modifier.padding(vertical = 3.dp)) {
                    Text(
                        ep,
                        style = MaterialTheme.typography.bodySmall.copy(fontFamily = Mono),
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        desc,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun LibraryContent(state: UiState, vm: MainViewModel) {
    Text("媒体库", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
    Spacer(Modifier.height(16.dp))
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        StatTile("照片", humanCount(state.photos), MaterialTheme.colorScheme.primary, Modifier.weight(1f))
        StatTile("视频", humanCount(state.videos), MaterialTheme.colorScheme.tertiary, Modifier.weight(1f))
        StatTile("占用", humanBytes(state.bytes), MaterialTheme.colorScheme.secondary, Modifier.weight(1f))
    }
    Spacer(Modifier.height(18.dp))
    Text(
        "全部 ${state.folders.size} 个相簿",
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(10.dp))
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        state.folders.forEach { (p, c) ->
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surfaceContainer)
                    .padding(horizontal = 14.dp, vertical = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    p, style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                    maxLines = 1,
                )
                Text(
                    "$c", style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun StepRow(index: String, title: String, body: String) {
    Row(Modifier.padding(bottom = 16.dp)) {
        Box(
            Modifier
                .size(26.dp)
                .clip(RoundedCornerShape(9.dp))
                .background(MaterialTheme.colorScheme.primaryContainer),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                index,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onPrimaryContainer,
                fontWeight = FontWeight.Bold,
            )
        }
        Spacer(Modifier.width(12.dp))
        Column {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(2.dp))
            Text(
                body,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun SwitchBlock(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().padding(bottom = 18.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(2.dp))
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(Modifier.width(14.dp))
        Switch(
            checked = checked,
            onCheckedChange = onCheckedChange,
            colors = SwitchDefaults.colors(
                checkedThumbColor = MaterialTheme.colorScheme.onPrimary,
                checkedTrackColor = MaterialTheme.colorScheme.primary,
            ),
        )
    }
}
