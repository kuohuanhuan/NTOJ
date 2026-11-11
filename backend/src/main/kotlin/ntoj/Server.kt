@file:OptIn(ExperimentalSerializationApi::class)

package ntoj

import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.OutgoingContent
import io.ktor.server.application.Application
import io.ktor.server.application.ApplicationCall
import io.ktor.server.request.contentLength
import io.ktor.server.request.contentType
import io.ktor.server.request.header
import io.ktor.server.request.host
import io.ktor.server.request.httpMethod
import io.ktor.server.request.path
import io.ktor.server.request.port
import io.ktor.server.request.receiveChannel
import io.ktor.server.response.header
import io.ktor.server.response.respond
import io.ktor.server.response.respondText
import io.ktor.server.routing.Route
import io.ktor.server.routing.route
import io.ktor.server.routing.routing
import io.ktor.utils.io.readRemaining
import java.net.URI
import java.time.Instant
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.io.readByteArray
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNamingStrategy
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.JsonUnquotedLiteral
import org.slf4j.LoggerFactory

/** An API error rendered as {"error": {"code", "message", "extra"?}}. */
class ApiError(val status: Int, val code: String, message: String, val extra: Map<String, Any?> = emptyMap()) : Exception(message)

fun badRequest(message: String) = ApiError(400, "bad_request", message)
fun unauthorized() = ApiError(401, "unauthorized", "Sign in required")
fun forbidden() = ApiError(403, "forbidden", "Permission denied")
fun notFound() = ApiError(404, "not_found", "Not found")
fun conflict(message: String) = ApiError(409, "conflict", message)
fun tooMany() = ApiError(429, "rate_limited", "Too many requests, please slow down")
fun tooLarge() = ApiError(413, "too_large", "Request body too large")

/** Request bodies: snake_case keys, unknown fields rejected, explicit nulls fall back to defaults. */
val json = Json {
    namingStrategy = JsonNamingStrategy.SnakeCase
    coerceInputValues = true
    encodeDefaults = true
}

/** Rows read back from the database, which carry columns the code does not model. */
val dbJson = Json(json) { ignoreUnknownKeys = true }

/** Embeds JSON text produced by PostgreSQL without re-parsing it. */
fun raw(text: String): JsonElement = JsonUnquotedLiteral(text)

/** Converts plain Kotlin values into a JSON tree. */
fun toJson(v: Any?): JsonElement = when (v) {
    null -> JsonNull
    is JsonElement -> v
    is String -> JsonPrimitive(v)
    is Number -> JsonPrimitive(v)
    is Boolean -> JsonPrimitive(v)
    is Instant -> JsonPrimitive(v.toString())
    is Map<*, *> -> JsonObject(v.entries.associate { (k, value) -> k.toString() to toJson(value) })
    is Iterable<*> -> JsonArray(v.map(::toJson))
    else -> throw IllegalArgumentException("cannot encode ${v::class} as JSON")
}

fun obj(vararg fields: Pair<String, Any?>): JsonObject = JsonObject(fields.associate { (k, v) -> k to toJson(v) })

/** The authenticated caller. */
data class Account(val id: Int, val name: String, val role: String, val sessionId: Long = 0) {
    val admin get() = role == "admin"
}

class Page(val limit: Int, val offset: Int, val page: Int)

const val MAX_JSON_BODY = 1 shl 20
private const val API_CSP = "default-src 'none'; frame-ancestors 'none'"

/** Carries one request through a handler. */
class Ctx(val s: Server, val call: ApplicationCall, val ip: String) {
    /** Null for guests. */
    var acct: Account? = null
    val acctId get() = acct?.id ?: 0
    val admin get() = acct?.admin == true
    val db get() = s.db

    fun securityHeaders(csp: String = API_CSP) {
        call.response.header("X-Content-Type-Options", "nosniff")
        call.response.header("X-Frame-Options", "DENY")
        call.response.header("Referrer-Policy", "same-origin")
        call.response.header("Cross-Origin-Resource-Policy", "same-origin")
        call.response.header("Content-Security-Policy", csp)
    }

    suspend fun respond(status: Int, body: JsonElement) {
        securityHeaders()
        call.response.header(HttpHeaders.CacheControl, "no-store")
        call.respondText(body.toString(), ContentType.Application.Json, HttpStatusCode.fromValue(status))
    }

    suspend fun ok(body: JsonElement) = respond(200, body)
    suspend fun ok(vararg fields: Pair<String, Any?>) = respond(200, obj(*fields))
    suspend fun created(vararg fields: Pair<String, Any?>) = respond(201, obj(*fields))

    /** Responds with JSON text that PostgreSQL already produced. */
    suspend fun okRaw(text: String) = respond(200, raw(text))

    suspend fun paged(items: String, total: Int, page: Page) =
        ok("items" to raw(items), "total" to total, "page" to page.page, "per_page" to page.limit)

    suspend fun noContent() {
        securityHeaders()
        call.respond(HttpStatusCode.NoContent)
    }

    suspend fun respondContent(content: OutgoingContent, csp: String) {
        securityHeaders(csp)
        call.respond(content)
    }

    /** Reads the request body, refusing anything above [limit] bytes. */
    suspend fun bodyBytes(limit: Int = MAX_JSON_BODY): ByteArray {
        if ((call.request.contentLength() ?: 0) > limit) throw tooLarge()
        val bytes = call.receiveChannel().readRemaining(limit + 1L).readByteArray()
        if (bytes.size > limit) throw tooLarge()
        return bytes
    }

    /** Decodes a JSON request body, rejecting unknown fields and non-JSON payloads. */
    suspend inline fun <reified T> bind(): T {
        if (!call.request.contentType().match(ContentType.Application.Json)) {
            throw ApiError(415, "unsupported_media_type", "Content-Type must be application/json")
        }
        val text = bodyBytes().toString(Charsets.UTF_8)
        return try {
            json.decodeFromString<T>(text)
        } catch (_: SerializationException) {
            throw badRequest("Invalid JSON body")
        } catch (_: IllegalArgumentException) {
            throw badRequest("Invalid JSON body")
        }
    }

    fun pathInt(name: String): Int {
        val n = call.parameters[name]?.takeIf { it.length <= 10 && it.all(Char::isDigit) }?.toLongOrNull()
        if (n == null || n <= 0 || n > Int.MAX_VALUE) throw notFound()
        return n.toInt()
    }

    fun query(name: String): String = call.request.queryParameters[name] ?: ""

    /** An optional non-negative integer query parameter. */
    fun queryInt(name: String): Int? {
        val v = query(name)
        if (v.isEmpty()) return null
        val n = v.takeIf { it.length <= 10 && it.all(Char::isDigit) }?.toLongOrNull()
        if (n == null || n > Int.MAX_VALUE) throw badRequest("Invalid $name")
        return n.toInt()
    }

    /** LIMIT and OFFSET from ?page=&per_page=. */
    fun page(defaultPer: Int): Page {
        val page = query("page").toIntOrNull()?.takeIf { it in 1..1_000_000 } ?: 1
        val limit = query("per_page").toIntOrNull()?.takeIf { it in 1..100 } ?: defaultPer
        return Page(limit, (page - 1) * limit, page)
    }

    /** Records a security-relevant action. Failures are logged, not thrown. */
    fun audit(type: String, message: String, params: Map<String, Any?> = emptyMap(), contestId: Int? = null) {
        try {
            db.exec(
                "INSERT INTO audit_logs (type, message, params, operator_id, operator_ip, contest_id) VALUES ($1, $2, $3, $4, $5, $6)",
                type, message, Jsonb(toJson(params).toString()), acct?.id, ip, contestId,
            )
        } catch (e: Exception) {
            s.log.error("audit log failed for {}", type, e)
        }
    }
}

typealias Handler = suspend (Ctx) -> Unit

enum class Level { GUEST, USER, ADMIN }

class Server(val db: Db, val cfg: Config) {
    val log = LoggerFactory.getLogger("ntoj")!!
    val limiter = Limiter()
    var now: () -> Instant = Instant::now

    /**
     * The peer address, honouring X-Forwarded-For only when the direct peer is
     * a configured trusted proxy.
     */
    fun clientIp(call: ApplicationCall): String {
        var ip = parseIp(call.request.local.remoteAddress) ?: return ""
        if (!trusted(ip)) return ip.hostAddress
        val hops = call.request.headers.getAll("X-Forwarded-For").orEmpty().joinToString(",").split(",")
        for (hop in hops.asReversed()) {
            ip = parseIp(hop) ?: break
            if (!trusted(ip)) break
        }
        return ip.hostAddress
    }

    private fun trusted(ip: java.net.InetAddress) = cfg.trustedProxies.any { it.contains(ip) }

    /**
     * Cross-site state-changing requests are rejected outright: the session
     * cookie is the only credential, so this is the CSRF boundary. Browsers
     * always send Sec-Fetch-Site or Origin on such requests; clients that send
     * neither (judge workers, scripts) are not browsers and carry no ambient cookie.
     */
    private fun crossOrigin(call: ApplicationCall): Boolean {
        val method = call.request.httpMethod
        if (method == HttpMethod.Get || method == HttpMethod.Head || method == HttpMethod.Options) return false
        val origin = call.request.header(HttpHeaders.Origin).orEmpty()
        if (origin.isNotEmpty() && origin in cfg.allowedOrigins) return false
        val site = call.request.header("Sec-Fetch-Site").orEmpty()
        if (site.isNotEmpty()) return site != "same-origin" && site != "none"
        if (origin.isEmpty()) return false
        val host = call.request.header(HttpHeaders.Host).orEmpty()
        val originHost = try {
            URI(origin).let { if (it.port > 0) "${it.host}:${it.port}" else it.host }
        } catch (_: Exception) {
            null
        }
        return originHost == null || !originHost.equals(host, ignoreCase = true)
    }

    suspend fun writeError(c: Ctx, err: Throwable) {
        val api = when (err) {
            is ApiError -> err
            is CancellationException -> throw err
            else -> {
                log.error("request failed: {} {}", c.call.request.httpMethod.value, c.call.request.path(), err)
                ApiError(500, "internal", "Internal server error")
            }
        }
        val body = LinkedHashMap<String, Any?>()
        body["code"] = api.code
        body["message"] = api.message
        if (api.extra.isNotEmpty()) body["extra"] = api.extra
        try {
            c.respond(api.status, obj("error" to body))
        } catch (e: Exception) {
            log.debug("could not write error response", e) // the response had already started
        }
    }

    /** Runs [block] off the event loop (JDBC blocks) and renders failures as API errors. */
    private suspend fun dispatch(call: ApplicationCall, block: suspend (Ctx) -> Unit) = withContext(Dispatchers.IO) {
        val c = Ctx(this@Server, call, clientIp(call))
        try {
            if (crossOrigin(call)) throw ApiError(403, "cross_origin", "Cross-origin request rejected")
            block(c)
        } catch (e: Throwable) {
            writeError(c, e)
        }
    }

    /**
     * [lockable] marks routes that a contestant may not use while taking part
     * in a running contest that forbids browsing the rest of the site.
     */
    private fun Route.api(method: HttpMethod, path: String, min: Level, lockable: Boolean, h: Handler) {
        route(path, method) {
            handle {
                dispatch(call) { c ->
                    c.acct = authenticate(c)
                    if (min >= Level.USER && c.acct == null) throw unauthorized()
                    if (min >= Level.ADMIN && !c.admin) throw forbidden()
                    if (lockable) checkLockdown(c)
                    h(c)
                }
            }
        }
    }

    /**
     * Judge workers authenticate by bearer token. The endpoints do not exist
     * at all when no token is configured.
     */
    private fun Route.judge(method: HttpMethod, path: String, h: Handler) {
        route(path, method) {
            handle {
                dispatch(call) { c ->
                    if (cfg.judgeToken.isEmpty()) throw notFound()
                    val header = call.request.header(HttpHeaders.Authorization).orEmpty()
                    val ok = header.startsWith("Bearer ") && java.security.MessageDigest.isEqual(
                        Auth.sha256(header.removePrefix("Bearer ")), Auth.sha256(cfg.judgeToken),
                    )
                    if (!ok) throw unauthorized()
                    h(c)
                }
            }
        }
    }

    fun install(app: Application) = app.routing {
        val get = HttpMethod.Get
        val post = HttpMethod.Post
        val put = HttpMethod.Put
        val patch = HttpMethod.Patch
        val delete = HttpMethod.Delete
        val guest = Level.GUEST
        val user = Level.USER
        val admin = Level.ADMIN
        val free = false
        val lock = true

        // Session and own account.
        api(get, "/api/session", guest, free, ::getSession)
        api(post, "/api/auth/signup", guest, free, ::signUp)
        api(post, "/api/auth/signin", guest, free, ::signIn)
        api(post, "/api/auth/signout", guest, free, ::signOut)
        api(get, "/api/account/sessions", user, free, ::listSessions)
        api(delete, "/api/account/sessions/{id}", user, free, ::revokeSession)
        api(delete, "/api/account/sessions", user, free, ::revokeOtherSessions)
        api(patch, "/api/account", user, free, ::updateProfile)
        api(post, "/api/account/password", user, free, ::changePassword)

        // Public site.
        api(get, "/api/accounts/{id}", guest, lock, ::getAccount)
        api(get, "/api/rank", guest, lock, ::userRank)
        api(get, "/api/bulletins", guest, free, ::listBulletins)
        api(get, "/api/bulletins/{id}", guest, free, ::getBulletin)
        api(get, "/api/tags", guest, lock, ::listTags)
        api(get, "/api/problems", guest, lock, ::listProblems)
        api(get, "/api/problems/{id}", guest, lock, ::getProblem)
        api(get, "/api/problems/{id}/rank", guest, lock, ::problemRank)
        api(get, "/api/problems/{id}/files/{name}", guest, free, ::getProblemFile)
        api(post, "/api/submissions", user, free, ::submit)
        api(get, "/api/submissions", guest, free, ::listSubmissions)
        api(get, "/api/submissions/{id}", guest, free, ::getSubmission)
        api(post, "/api/submissions/{id}/rejudge", user, free, ::rejudgeSubmission)
        api(get, "/api/classes", guest, lock, ::listClasses)
        api(get, "/api/classes/{id}", guest, lock, ::getClass)
        api(post, "/api/classes", user, lock, ::createClass)
        api(put, "/api/classes/{id}", user, lock, ::updateClass)
        api(delete, "/api/classes/{id}", user, lock, ::deleteClass)
        api(put, "/api/classes/{id}/collect", user, lock, ::collectClass)
        api(delete, "/api/classes/{id}/collect", user, lock, ::uncollectClass)
        api(get, "/api/boards", guest, lock, ::listBoards)
        api(get, "/api/boards/{id}", guest, lock, ::getBoard)
        api(get, "/api/questions", user, free, ::listQuestions)
        api(post, "/api/questions", user, free, ::askQuestion)
        api(delete, "/api/questions/{id}", user, free, ::deleteQuestion)

        // Contests.
        api(get, "/api/contests", guest, free, ::listContests)
        api(post, "/api/contests", admin, free, ::createContest)
        api(get, "/api/contests/{id}", guest, free, ::getContest)
        api(patch, "/api/contests/{id}", user, free, ::updateContest)
        api(delete, "/api/contests/{id}", admin, free, ::deleteContest)
        api(post, "/api/contests/{id}/register", user, free, ::registerContest)
        api(delete, "/api/contests/{id}/register", user, free, ::withdrawContest)
        api(post, "/api/contests/{id}/start", user, free, ::startContestSession)
        api(get, "/api/contests/{id}/problems", guest, free, ::listContestProblems)
        api(get, "/api/contests/{id}/problems/{pid}", guest, free, ::getContestProblem)
        api(get, "/api/contests/{id}/scoreboard", guest, free, ::contestScoreboard)
        api(get, "/api/contests/{id}/qa", guest, free, ::contestQA)
        api(post, "/api/contests/{id}/questions", user, free, ::askContestQuestion)
        api(get, "/api/contests/{id}/manage", user, free, ::manageContest)
        api(put, "/api/contests/{id}/manage/problems", user, free, ::setContestProblems)
        api(put, "/api/contests/{id}/manage/users/{aid}", user, free, ::setContestUser)
        api(delete, "/api/contests/{id}/manage/users/{aid}", user, free, ::removeContestUser)
        api(post, "/api/contests/{id}/manage/users/{aid}/reset-session", user, free, ::resetContestSession)
        api(post, "/api/contests/{id}/manage/announcements", user, free, ::createAnnouncement)
        api(put, "/api/contests/{id}/manage/announcements/{nid}", user, free, ::updateAnnouncement)
        api(delete, "/api/contests/{id}/manage/announcements/{nid}", user, free, ::deleteAnnouncement)
        api(post, "/api/contests/{id}/manage/questions/{qid}/reply", user, free, ::replyContestQuestion)
        api(post, "/api/contests/{id}/manage/rejudge", user, free, ::rejudgeContest)
        api(get, "/api/contests/{id}/manage/logs", user, free, ::contestLogs)

        // Site administration.
        api(get, "/api/admin/dashboard", admin, free, ::adminDashboard)
        api(get, "/api/admin/accounts", admin, free, ::adminListAccounts)
        api(patch, "/api/admin/accounts/{id}", admin, free, ::adminUpdateAccount)
        api(post, "/api/admin/accounts/{id}/password", admin, free, ::adminResetPassword)
        api(get, "/api/admin/problems", admin, free, ::adminListProblems)
        api(post, "/api/admin/problems", admin, free, ::adminCreateProblem)
        api(get, "/api/admin/problems/{id}", admin, free, ::adminGetProblem)
        api(patch, "/api/admin/problems/{id}", admin, free, ::adminUpdateProblem)
        api(delete, "/api/admin/problems/{id}", admin, free, ::adminDeleteProblem)
        api(post, "/api/admin/problems/{id}/testcases", admin, free, ::adminUploadTestcases)
        api(delete, "/api/admin/problems/{id}/testcases/{tid}", admin, free, ::adminDeleteTestcase)
        api(get, "/api/admin/problems/{id}/testcases/{tid}/{kind}", admin, free, ::adminDownloadTestcase)
        api(put, "/api/admin/problems/{id}/subtasks", admin, free, ::adminSetSubtasks)
        api(post, "/api/admin/problems/{id}/files", admin, free, ::adminUploadFiles)
        api(delete, "/api/admin/problems/{id}/files/{name}", admin, free, ::adminDeleteFile)
        api(post, "/api/admin/problems/{id}/rejudge", admin, free, ::adminRejudgeProblem)
        api(post, "/api/admin/bulletins", admin, free, ::adminCreateBulletin)
        api(put, "/api/admin/bulletins/{id}", admin, free, ::adminUpdateBulletin)
        api(delete, "/api/admin/bulletins/{id}", admin, free, ::adminDeleteBulletin)
        api(get, "/api/admin/boards", admin, free, ::adminListBoards)
        api(post, "/api/admin/boards", admin, free, ::adminCreateBoard)
        api(put, "/api/admin/boards/{id}", admin, free, ::adminUpdateBoard)
        api(delete, "/api/admin/boards/{id}", admin, free, ::adminDeleteBoard)
        api(get, "/api/admin/questions", admin, free, ::adminListQuestions)
        api(post, "/api/admin/questions/{id}/reply", admin, free, ::adminReplyQuestion)
        api(get, "/api/admin/logs", admin, free, ::adminLogs)

        // Judge workers (bearer token, no cookies).
        judge(post, "/api/judge/claim", ::judgeClaim)
        judge(post, "/api/judge/submissions/{id}/result", ::judgeResult)
        judge(get, "/api/judge/testcases/{tid}/{kind}", ::judgeTestcase)

        route("/api/healthz", get) {
            handle {
                val healthy = withContext(Dispatchers.IO) { runCatching { db.count("SELECT 1") }.isSuccess }
                if (healthy) call.respondText("ok") else call.respondText("database unavailable", status = HttpStatusCode.ServiceUnavailable)
            }
        }
        // Everything else under /api is a JSON 404 rather than an empty page.
        route("/api/{...}") {
            handle { dispatch(call) { throw notFound() } }
        }
    }
}

/**
 * A fixed-window in-memory rate limiter. State is per process, which is
 * sufficient to blunt credential stuffing and form spam.
 */
class Limiter {
    private class Bucket(var count: Int, val reset: Long)

    private val buckets = HashMap<String, Bucket>()
    private var sweep = 0L

    /** Whether another event for [key] fits in [max] events per [windowSeconds]. */
    @Synchronized
    fun allow(key: String, max: Int, windowSeconds: Long): Boolean {
        val now = System.currentTimeMillis()
        if (now > sweep) {
            buckets.values.removeIf { now > it.reset }
            sweep = now + 60_000
        }
        val b = buckets[key]?.takeIf { now <= it.reset } ?: Bucket(0, now + windowSeconds * 1000).also { buckets[key] = it }
        b.count++
        return b.count <= max
    }
}
