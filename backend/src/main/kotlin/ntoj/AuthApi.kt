package ntoj

import io.ktor.http.Cookie
import io.ktor.http.CookieEncoding
import io.ktor.server.request.userAgent
import java.net.URI
import kotlinx.serialization.Serializable

const val SESSION_COOKIE = "ntoj_session"
private const val SESSION_TTL_SECONDS = 30 * 24 * 3600

class Language(val id: String, val name: String)

/** Languages a submission may use. The judge decides how to build each one. */
val LANGUAGES = listOf(
    Language("gcc", "C (GCC, GNU11)"),
    Language("clang", "C (Clang, C11)"),
    Language("g++", "C++ (G++, GNU++17)"),
    Language("clang++", "C++ (Clang++, C++17)"),
    Language("rust", "Rust"),
    Language("python3", "Python 3"),
    Language("java", "Java"),
)

fun validLanguage(id: String) = LANGUAGES.any { it.id == id }

/**
 * Resolves the session cookie. Invalid, expired, disabled or IP-restricted
 * sessions simply yield a guest.
 */
fun authenticate(c: Ctx): Account? {
    val token = c.call.request.cookies.rawCookies[SESSION_COOKIE]
    if (token.isNullOrEmpty() || token.length > 128) return null

    class Row(val acct: Account, val allowedIp: String, val lastIp: String, val stale: Boolean)

    val row = c.db.one(
        """SELECT s.id, a.id, a.name, a.role, a.allowed_ip, a.last_ip, s.last_seen_at < now() - interval '1 minute'
           FROM sessions s JOIN accounts a ON a.id = s.account_id
           WHERE s.token_hash = $1 AND s.expires_at > now() AND NOT a.disabled""",
        Auth.hashToken(token),
    ) { Row(Account(it.getInt(2), it.getString(3), it.getString(4), it.getLong(1)), it.getString(5), it.getString(6), it.getBoolean(7)) }
        ?: return null
    if (row.allowedIp.isNotEmpty() && row.allowedIp != c.ip) return null
    if (row.stale || row.lastIp != c.ip) {
        c.db.exec("UPDATE sessions SET last_seen_at = now(), ip = $2 WHERE id = $1", row.acct.sessionId, c.ip)
        if (row.lastIp != c.ip) {
            c.db.exec("UPDATE accounts SET last_ip = $2 WHERE id = $1", row.acct.id, c.ip)
            c.acct = row.acct
            c.audit("account.ip", "Account IP changed", mapOf("from" to row.lastIp, "to" to c.ip))
            c.acct = null
        }
    }
    return row.acct
}

private fun setSessionCookie(c: Ctx, value: String, maxAge: Int) {
    c.call.response.cookies.append(
        Cookie(
            name = SESSION_COOKIE, value = value, encoding = CookieEncoding.RAW, maxAge = maxAge, path = "/",
            secure = c.s.cfg.cookieSecure, httpOnly = true, extensions = mapOf("SameSite" to "Lax"),
        ),
    )
}

private fun startSession(c: Ctx, accountId: Int) {
    val (token, hash) = Auth.newToken()
    val ua = c.call.request.userAgent().orEmpty().take(256)
    c.db.exec(
        "INSERT INTO sessions (account_id, token_hash, ip, user_agent, expires_at) VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))",
        accountId, hash, c.ip, ua.replace("\u0000", ""), SESSION_TTL_SECONDS,
    )
    // Opportunistic cleanup of this account's expired sessions.
    c.db.exec("DELETE FROM sessions WHERE account_id = $1 AND expires_at < now()", accountId)
    setSessionCookie(c, token, SESSION_TTL_SECONDS)
}

suspend fun getSession(c: Ctx) {
    val site = obj(
        "title" to c.s.cfg.siteTitle,
        "allow_signup" to c.s.cfg.allowSignup,
        "languages" to LANGUAGES.map { mapOf("id" to it.id, "name" to it.name) },
    )
    val acct = c.acct ?: return c.ok("site" to site, "account" to null, "lockdown" to null)
    val account = c.db.jsonRow(
        """SELECT a.id, a.name, a.email, a.role, a.photo_url, a.cover_url, a.motto, a.last_language,
               (SELECT count(*) FROM questions q WHERE q.account_id = a.id AND q.reply IS NOT NULL AND NOT q.reply_seen) AS unread_replies
           FROM accounts a WHERE a.id = $1""",
        acct.id,
    )
    c.ok("site" to site, "account" to raw(account), "lockdown" to lockdownContest(c).takeIf { it != 0 })
}

fun cleanText(s: String): String = s.replace("\u0000", "").trim()

fun runeCount(s: String) = s.codePointCount(0, s.length)

private val PRINTABLE = setOf(
    Character.UPPERCASE_LETTER, Character.LOWERCASE_LETTER, Character.TITLECASE_LETTER, Character.MODIFIER_LETTER,
    Character.OTHER_LETTER, Character.NON_SPACING_MARK, Character.ENCLOSING_MARK, Character.COMBINING_SPACING_MARK,
    Character.DECIMAL_DIGIT_NUMBER, Character.LETTER_NUMBER, Character.OTHER_NUMBER, Character.CONNECTOR_PUNCTUATION,
    Character.DASH_PUNCTUATION, Character.START_PUNCTUATION, Character.END_PUNCTUATION, Character.INITIAL_QUOTE_PUNCTUATION,
    Character.FINAL_QUOTE_PUNCTUATION, Character.OTHER_PUNCTUATION, Character.MATH_SYMBOL, Character.CURRENCY_SYMBOL,
    Character.MODIFIER_SYMBOL, Character.OTHER_SYMBOL,
).map { it.toInt() }.toSet()

/** Display names: 1 to 32 visible characters; no control or invisible formatting characters. */
fun checkName(name: String) {
    if (runeCount(name) !in 1..32) throw badRequest("Name must be 1 to 32 characters")
    if (!name.codePoints().allMatch { it == ' '.code || Character.getType(it) in PRINTABLE }) {
        throw badRequest("Name contains invalid characters")
    }
}

private val EMAIL = Regex("""^[^\s@<>()\[\],;:\\"]+@[^\s@<>()\[\],;:\\"]+$""")

fun checkEmail(email: String) {
    if (email.length > 254 || !EMAIL.matches(email)) throw badRequest("Invalid email address")
}

/** Accepts an empty string or an absolute https URL. */
fun validImageUrl(raw: String): Boolean {
    if (raw.isEmpty()) return true
    if (raw.length > 512) return false
    return try {
        val u = URI(raw)
        u.scheme == "https" && !u.host.isNullOrEmpty() && u.userInfo == null
    } catch (_: Exception) {
        false
    }
}

@Serializable
private class SignUpInput(val email: String = "", val name: String = "", val password: String = "")

suspend fun signUp(c: Ctx) {
    if (c.acct != null) throw conflict("Already signed in")
    if (!c.s.cfg.allowSignup) throw ApiError(403, "signup_disabled", "Registration is closed")
    val input = c.bind<SignUpInput>()
    if (!c.s.limiter.allow("signup:${c.ip}", 10, 3600)) throw tooMany()
    val email = cleanText(input.email)
    val name = cleanText(input.name)
    checkEmail(email)
    checkName(name)
    Auth.passwordProblem(input.password)?.let { throw badRequest(it) }
    val hash = Auth.hashPassword(input.password)
    val id = try {
        c.db.one("INSERT INTO accounts (email, name, password_hash, last_ip) VALUES ($1, $2, $3, $4) RETURNING id", email, name, hash, c.ip) { it.getInt(1) }!!
    } catch (e: java.sql.SQLException) {
        if (e.isUniqueViolation()) throw conflict("This email is already registered")
        throw e
    }
    c.acct = Account(id, name, "user")
    c.audit("account.signup", "Account created")
    startSession(c, id)
    c.created("id" to id)
}

@Serializable
private class SignInInput(val email: String = "", val password: String = "")

suspend fun signIn(c: Ctx) {
    if (c.acct != null) throw conflict("Already signed in")
    val input = c.bind<SignInInput>()
    val email = cleanText(input.email)
    if (email.length > 254 || input.password.length > 256) throw badRequest("Invalid credentials")
    if (!c.s.limiter.allow("signin-ip:${c.ip}", 30, 300) || !c.s.limiter.allow("signin-acct:${email.lowercase()}", 10, 300)) throw tooMany()
    val failed = ApiError(401, "invalid_credentials", "Incorrect email or password")

    class Row(val id: Int, val hash: String, val allowedIp: String, val disabled: Boolean, val name: String, val role: String)

    val row = c.db.one("SELECT id, password_hash, allowed_ip, disabled, name, role FROM accounts WHERE lower(email) = lower($1)", email) {
        Row(it.getInt(1), it.getString(2), it.getString(3), it.getBoolean(4), it.getString(5), it.getString(6))
    }
    if (row == null) {
        Auth.verifyDummy(input.password)
        c.audit("signin.failure", "Sign-in failed: unknown account", mapOf("email" to email))
        throw failed
    }
    if (!Auth.verifyPassword(row.hash, input.password) || row.disabled) {
        c.audit("signin.failure", "Sign-in failed", mapOf("account_id" to row.id))
        throw failed
    }
    if (row.allowedIp.isNotEmpty() && row.allowedIp != c.ip) {
        c.audit("signin.failure", "Sign-in rejected: IP not allowed", mapOf("account_id" to row.id))
        throw ApiError(403, "ip_not_allowed", "This account cannot sign in from your IP address")
    }
    c.acct = Account(row.id, row.name, row.role)
    c.audit("signin.success", "Signed in")
    startSession(c, row.id)
    c.ok("id" to row.id)
}

suspend fun signOut(c: Ctx) {
    c.acct?.let {
        c.db.exec("DELETE FROM sessions WHERE id = $1", it.sessionId)
        c.audit("signout", "Signed out")
    }
    setSessionCookie(c, "", 0)
    c.noContent()
}

suspend fun listSessions(c: Ctx) {
    val acct = c.acct!!
    c.okRaw(
        c.db.jsonList(
            """SELECT id, ip, user_agent, created_at, last_seen_at, id = $2 AS current
               FROM sessions WHERE account_id = $1 AND expires_at > now() ORDER BY last_seen_at DESC""",
            acct.id, acct.sessionId,
        ),
    )
}

suspend fun revokeSession(c: Ctx) {
    val acct = c.acct!!
    val id = c.pathInt("id")
    if (c.db.exec("DELETE FROM sessions WHERE id = $1 AND account_id = $2", id, acct.id) == 0) throw notFound()
    if (id.toLong() == acct.sessionId) setSessionCookie(c, "", 0)
    c.noContent()
}

suspend fun revokeOtherSessions(c: Ctx) {
    val acct = c.acct!!
    c.db.exec("DELETE FROM sessions WHERE account_id = $1 AND id <> $2", acct.id, acct.sessionId)
    c.noContent()
}

@Serializable
private class ProfileInput(val name: String = "", val photoUrl: String = "", val coverUrl: String = "", val motto: String = "")

suspend fun updateProfile(c: Ctx) {
    val input = c.bind<ProfileInput>()
    val name = cleanText(input.name)
    val motto = cleanText(input.motto)
    val photo = input.photoUrl.trim()
    val cover = input.coverUrl.trim()
    checkName(name)
    if (runeCount(motto) > 100) throw badRequest("Motto must be at most 100 characters")
    if (!validImageUrl(photo) || !validImageUrl(cover)) throw badRequest("Image URLs must be https:// links")
    c.db.exec("UPDATE accounts SET name = $2, photo_url = $3, cover_url = $4, motto = $5 WHERE id = $1", c.acctId, name, photo, cover, motto)
    c.noContent()
}

@Serializable
private class PasswordInput(val old: String = "", val new: String = "")

suspend fun changePassword(c: Ctx) {
    val acct = c.acct!!
    val input = c.bind<PasswordInput>()
    if (!c.s.limiter.allow("password:${c.ip}", 10, 600)) throw tooMany()
    Auth.passwordProblem(input.new)?.let { throw badRequest(it) }
    val hash = c.db.one("SELECT password_hash FROM accounts WHERE id = $1", acct.id) { it.getString(1) }!!
    if (input.old.length > 256 || !Auth.verifyPassword(hash, input.old)) {
        throw ApiError(403, "wrong_password", "Current password is incorrect")
    }
    val newHash = Auth.hashPassword(input.new)
    c.db.tx { t ->
        t.exec("UPDATE accounts SET password_hash = $2 WHERE id = $1", acct.id, newHash)
        // Every other device must sign in again with the new password.
        t.exec("DELETE FROM sessions WHERE account_id = $1 AND id <> $2", acct.id, acct.sessionId)
    }
    c.audit("account.password", "Password changed")
    c.noContent()
}

/** Creates the configured administrator when no admin exists. */
fun bootstrapAdmin(s: Server) {
    val cfg = s.cfg
    if (cfg.adminEmail.isEmpty() || cfg.adminPassword.isEmpty()) return
    if (s.db.one("SELECT 1 FROM accounts WHERE role = 'admin' LIMIT 1") { true } != null) return
    Auth.passwordProblem(cfg.adminPassword)?.let { throw IllegalArgumentException("ADMIN_PASSWORD: $it") }
    s.db.exec(
        """INSERT INTO accounts (email, name, password_hash, role) VALUES ($1, $2, $3, 'admin')
           ON CONFLICT (lower(email)) DO UPDATE SET role = 'admin', disabled = false, password_hash = EXCLUDED.password_hash""",
        cfg.adminEmail, cfg.adminName, Auth.hashPassword(cfg.adminPassword),
    )
    s.log.info("bootstrap administrator created: {}", cfg.adminEmail)
}
