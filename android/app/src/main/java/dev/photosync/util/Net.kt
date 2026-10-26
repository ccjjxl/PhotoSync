package dev.photosync.util

import java.net.Inet4Address
import java.net.NetworkInterface

object Net {

    data class Address(val ip: String, val iface: String, val isWifi: Boolean)

    fun localAddresses(): List<Address> {
        val out = mutableListOf<Address>()
        runCatching {
            NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
                .filter { it.isUp && !it.isLoopback && !it.isVirtual }
                .forEach { nif ->
                    val name = nif.name.lowercase()
                    val wifi = name.startsWith("wlan") || name.startsWith("ap") || name.startsWith("wl")
                    nif.inetAddresses.toList()
                        .filterIsInstance<Inet4Address>()
                        .filter { !it.isLoopbackAddress && it.isSiteLocalAddress }
                        .forEach { out += Address(it.hostAddress ?: "", nif.name, wifi) }
                }
        }
        return out.sortedByDescending { it.isWifi }
    }

    fun primaryAddress(): String? = localAddresses().firstOrNull()?.ip

    fun deviceName(): String = (android.os.Build.MODEL ?: "Android").replace(' ', '-') +
        "-" + android.os.Build.MANUFACTURER.replace(' ', '-')
}
