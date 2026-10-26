package dev.photosync.server

import dev.photosync.util.Net
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.util.concurrent.atomic.AtomicBoolean

/**
 * UDP responder so the desktop client can find this phone without typing an IP.
 *
 *   client -> 255.255.255.255:8738   "PHOTOSYNC/1 DISCOVER"
 *   phone  -> client                "PHOTOSYNC/1 OFFER {json}"
 *
 * Broadcast only, no multicast group needed -> works on every home router.
 */
class DiscoveryResponder(
    private val portProvider: () -> Int,
    private val deviceNameProvider: () -> String,
    private val deviceIdProvider: () -> String,
    private val folderNameProvider: () -> String,
    private val openAccessProvider: () -> Boolean,
    private val folderFilterProvider: () -> Boolean = { false },
) {
    private val running = AtomicBoolean(false)
    private var socket: DatagramSocket? = null

    val isRunning: Boolean get() = running.get()

    fun start() {
        if (!running.compareAndSet(false, true)) return
        Thread({
            val sock = runCatching {
                DatagramSocket(DISCOVERY_PORT).apply {
                    broadcast = true
                    reuseAddress = true
                    soTimeout = 2000
                }
            }.getOrNull()
            if (sock == null) {
                running.set(false)
                return@Thread
            }
            socket = sock
            val buf = ByteArray(BUFFER)
            while (running.get()) {
                try {
                    val packet = DatagramPacket(buf, buf.size)
                    sock.receive(packet)
                    val msg = String(packet.data, 0, packet.length).trim()
                    if (!msg.startsWith(MAGIC)) continue
                    val cmd = msg.removePrefix(MAGIC).trim().uppercase()
                    if (!cmd.startsWith("DISCOVER")) continue

                    // Never let one malformed reply kill the whole responder:
                    // skip this probe, log why, and keep listening.
                    val reply = runCatching { buildOffer() }
                        .onFailure {
                            android.util.Log.w("PhotoSync", "discovery offer failed", it)
                        }
                        .getOrNull() ?: continue
                    val bytes = reply.toByteArray()
                    sock.send(DatagramPacket(bytes, bytes.size, packet.address, packet.port))
                } catch (_: java.net.SocketTimeoutException) {
                    // loop again, lets us notice stop()
                } catch (_: Throwable) {
                    if (running.get()) Thread.sleep(200)
                }
            }
            runCatching { sock.close() }
        }, "photosync-discovery").apply { isDaemon = true }.start()
    }

    fun stop() {
        if (!running.compareAndSet(true, false)) return
        runCatching { socket?.close() }
        socket = null
    }

    private fun buildOffer(): String = json {
        field("magic", MAGIC)
        field("type", "OFFER")
        field("app", "PhotoSync")
        field("version", dev.photosync.BuildConfig.VERSION_NAME)
        field("api", ServerEngine.API_VERSION)
        field("device", deviceNameProvider())
        field("deviceId", deviceIdProvider())
        field("folder", folderNameProvider())
        field("model", android.os.Build.MODEL ?: "Android")
        field("port", portProvider())
        field("openAccess", openAccessProvider())
        field("folderFilter", folderFilterProvider())
        field("host", "android")
        arr("addresses") {
            Net.localAddresses().forEach { a ->
                obj {
                    field("ip", a.ip)
                    field("iface", a.iface)
                    field("wifi", a.isWifi)
                }
            }
        }
    }

    companion object {
        const val DISCOVERY_PORT = 8738
        const val MAGIC = "PHOTOSYNC/1"
        private const val BUFFER = 512
    }
}
