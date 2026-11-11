package ntoj

import java.net.InetAddress

/** Runtime configuration, loaded from environment variables. */
data class Config(
    val listenHost: String = "0.0.0.0",
    val listenPort: Int = 8080,
    val databaseUrl: String = "",
    val dataDir: String = "./data",
    val siteTitle: String = "NTOJ",
    val cookieSecure: Boolean = true,
    val allowSignup: Boolean = true,
    val trustedProxies: List<Cidr> = emptyList(),
    val allowedOrigins: List<String> = emptyList(),
    val judgeToken: String = "",
    /** Seconds between two submissions of one account outside contests. */
    var submitCooldown: Int = 5,
    // Optional first-run bootstrap of an administrator account.
    val adminEmail: String = "",
    val adminName: String = "admin",
    val adminPassword: String = "",
) {
    companion object {
        fun load(env: Map<String, String> = System.getenv()): Config {
            fun get(key: String, def: String = "") = env[key]?.takeIf { it.isNotEmpty() } ?: def
            fun bool(key: String, def: Boolean) = when (get(key).lowercase()) {
                "" -> def
                "1", "t", "true" -> true
                "0", "f", "false" -> false
                else -> throw IllegalArgumentException("$key must be true or false")
            }
            fun list(key: String) = get(key).split(",").map { it.trim() }.filter { it.isNotEmpty() }

            val databaseUrl = get("DATABASE_URL")
            require(databaseUrl.isNotEmpty()) { "DATABASE_URL is required" }
            val listen = get("LISTEN_ADDR", ":8080")
            val port = listen.substringAfterLast(':').toIntOrNull()
            require(port != null && port in 1..65535) { "LISTEN_ADDR must look like host:port or :port" }
            val cooldown = get("SUBMIT_COOLDOWN", "5").toIntOrNull()
            require(cooldown != null && cooldown >= 0) { "SUBMIT_COOLDOWN must be a non-negative integer" }
            val judgeToken = get("JUDGE_TOKEN")
            require(judgeToken.isEmpty() || judgeToken.length >= 32) { "JUDGE_TOKEN must be at least 32 characters" }
            return Config(
                listenHost = listen.substringBeforeLast(':').ifEmpty { "0.0.0.0" },
                listenPort = port,
                databaseUrl = databaseUrl,
                dataDir = get("DATA_DIR", "./data"),
                siteTitle = get("SITE_TITLE", "NTOJ"),
                cookieSecure = bool("COOKIE_SECURE", true),
                allowSignup = bool("ALLOW_SIGNUP", true),
                trustedProxies = list("TRUSTED_PROXIES").map {
                    Cidr.parse(it) ?: throw IllegalArgumentException("TRUSTED_PROXIES: \"$it\" is not an IP or CIDR")
                },
                allowedOrigins = list("ALLOWED_ORIGINS"),
                judgeToken = judgeToken,
                submitCooldown = cooldown,
                adminEmail = get("ADMIN_EMAIL"),
                adminName = get("ADMIN_NAME", "admin"),
                adminPassword = get("ADMIN_PASSWORD"),
            )
        }
    }
}

private val IPV4 = Regex("""^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$""")
private val IPV6 = Regex("""^[0-9a-fA-F:.]{2,45}$""")

/**
 * Parses an IP literal without ever touching DNS. IPv4-mapped IPv6 addresses
 * come back as plain IPv4, so one client always has one textual form.
 */
fun parseIp(text: String): InetAddress? {
    val s = text.trim()
    val v4 = IPV4.matchEntire(s)
    if (v4 != null) {
        if (v4.groupValues.drop(1).any { it.toInt() > 255 }) return null
    } else if (!(s.contains(':') && IPV6.matches(s))) {
        return null
    }
    return try {
        InetAddress.getByName(s)
    } catch (_: Exception) {
        null
    }
}

class Cidr(private val base: ByteArray, private val bits: Int) {
    fun contains(ip: InetAddress): Boolean {
        val addr = ip.address
        if (addr.size != base.size) return false
        for (i in 0 until bits) {
            val mask = 0x80 shr (i % 8)
            if ((addr[i / 8].toInt() and mask) != (base[i / 8].toInt() and mask)) return false
        }
        return true
    }

    companion object {
        fun parse(text: String): Cidr? {
            val ip = parseIp(text.substringBefore('/')) ?: return null
            val max = ip.address.size * 8
            val bits = if ('/' in text) text.substringAfter('/').toIntOrNull() ?: return null else max
            return if (bits in 0..max) Cidr(ip.address, bits) else null
        }
    }
}
