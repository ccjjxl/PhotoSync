package dev.photosync.util

import java.net.Inet4Address
import java.net.NetworkInterface

object Net {

    data class Address(val ip: String, val iface: String, val isWifi: Boolean)

    /**
     * Interface name prefixes that are never useful to a desktop client.
     *
     * `vgate0` / `tun0` / `ppp0` are VPN tunnels, `rmnet*` is cellular data, and
     * `dummy*` is a placeholder. Showing them produced addresses like
     * "vgate0: 10.x.x.x" that the PC could never connect to.
     */
    private val IGNORED_PREFIXES = listOf(
        "tun", "tap", "ppp", "rmnet", "vgate", "v4-", "ipsec",
        "p2p", "dummy", "sit", "ip6tnl", "gre", "clat",
    )

    fun localAddresses(): List<Address> {
        val out = mutableListOf<Address>()
        runCatching {
            NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
                .filter { it.isUp && !it.isLoopback && !it.isVirtual }
                .filter { nif ->
                    val name = nif.name.lowercase()
                    IGNORED_PREFIXES.none { name.startsWith(it) }
                }
                .forEach { nif ->
                    val name = nif.name.lowercase()
                    val wifi = name.startsWith("wlan") || name.startsWith("ap") || name.startsWith("wl")
                    nif.inetAddresses.toList()
                        .filterIsInstance<Inet4Address>()
                        .filter { !it.isLoopbackAddress && it.isSiteLocalAddress }
                        .forEach { out += Address(it.hostAddress ?: "", nif.name, wifi) }
                }
        }
        // Wi-Fi first, then wired/USB tethering; keeps the primary address stable.
        return out.sortedWith(compareByDescending<Address> { it.isWifi }.thenBy { it.iface })
    }

    fun primaryAddress(): String? = localAddresses().firstOrNull()?.ip

    fun deviceName(): String = (android.os.Build.MODEL ?: "Android").replace(' ', '-') +
        "-" + android.os.Build.MANUFACTURER.replace(' ', '-')
}
