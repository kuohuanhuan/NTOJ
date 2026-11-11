package ntoj

import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import org.bouncycastle.crypto.generators.Argon2BytesGenerator
import org.bouncycastle.crypto.params.Argon2Parameters

/** Password hashing and session-token primitives. */
object Auth {
    // Argon2id parameters (OWASP baseline: 19 MiB, t=2, p=1).
    private const val TIME = 2
    private const val MEMORY_KB = 19 * 1024
    private const val THREADS = 1
    private const val KEY_LEN = 32
    private const val SALT_LEN = 16
    private const val VERSION = Argon2Parameters.ARGON2_VERSION_13

    private val random = SecureRandom()
    private val b64 = Base64.getEncoder().withoutPadding()
    private val b64d = Base64.getDecoder()

    private fun derive(password: String, salt: ByteArray, memoryKb: Int, time: Int, threads: Int, len: Int): ByteArray {
        val params = Argon2Parameters.Builder(Argon2Parameters.ARGON2_id)
            .withVersion(VERSION).withMemoryAsKB(memoryKb).withIterations(time).withParallelism(threads).withSalt(salt).build()
        val out = ByteArray(len)
        Argon2BytesGenerator().apply { init(params) }.generateBytes(password.toByteArray(Charsets.UTF_8), out)
        return out
    }

    /** Returns a PHC-formatted argon2id hash. */
    fun hashPassword(password: String): String {
        val salt = ByteArray(SALT_LEN).also(random::nextBytes)
        val key = derive(password, salt, MEMORY_KB, TIME, THREADS, KEY_LEN)
        return "\$argon2id\$v=$VERSION\$m=$MEMORY_KB,t=$TIME,p=$THREADS\$${b64.encodeToString(salt)}\$${b64.encodeToString(key)}"
    }

    private val PARAMS = Regex("""^m=(\d{1,9}),t=(\d{1,4}),p=(\d{1,3})$""")

    /** Reports whether [password] matches the PHC [hash]. */
    fun verifyPassword(hash: String, password: String): Boolean {
        val parts = hash.split('$')
        if (parts.size != 6 || parts[1] != "argon2id" || parts[2] != "v=$VERSION") return false
        val (memory, time, threads) = PARAMS.matchEntire(parts[3])?.destructured?.toList()?.map { it.toInt() } ?: return false
        // Bound the cost so a corrupted row cannot be used to exhaust memory.
        if (memory !in 8..(1 shl 20) || time !in 1..16 || threads !in 1..16) return false
        return try {
            val salt = b64d.decode(parts[4])
            val want = b64d.decode(parts[5])
            want.isNotEmpty() && MessageDigest.isEqual(derive(password, salt, memory, time, threads, want.size), want)
        } catch (_: IllegalArgumentException) {
            false
        }
    }

    private val dummyHash = hashPassword("ntoj-dummy-password")

    /** Burns one hash computation, so unknown accounts cost as much as known ones. */
    fun verifyDummy(password: String) {
        verifyPassword(dummyHash, password)
    }

    /** Returns the policy violation of [password], or null when it is acceptable. */
    fun passwordProblem(password: String): String? = when {
        password.codePointCount(0, password.length) < 8 -> "Password must be at least 8 characters"
        password.toByteArray(Charsets.UTF_8).size > 256 -> "Password is too long"
        else -> null
    }

    /** A fresh random session token and the hash stored server-side. */
    fun newToken(): Pair<String, ByteArray> {
        val raw = ByteArray(32).also(random::nextBytes)
        val token = Base64.getUrlEncoder().withoutPadding().encodeToString(raw)
        return token to hashToken(token)
    }

    /** Maps a presented token to its stored form. */
    fun hashToken(token: String): ByteArray = sha256(token)

    fun sha256(text: String): ByteArray = MessageDigest.getInstance("SHA-256").digest(text.toByteArray(Charsets.UTF_8))
}
