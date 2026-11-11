package ntoj

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.doubleOrNull
import java.io.File
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.nio.file.Files
import java.time.Duration
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue
import kotlin.test.fail

private const val JUDGE_TOKEN = "test-judge-token-0123456789abcdef-0123456789"

/** A lenient view over a JSON response, so assertions read like the payload. */
class J(val el: JsonElement?) {
    operator fun get(key: String) = J((el as? JsonObject)?.get(key))
    operator fun get(i: Int) = J((el as? JsonArray)?.getOrNull(i))
    val isNull get() = el == null || el is JsonNull
    val str get() = (el as? JsonPrimitive)?.takeIf { it.isString }?.content
    val num get() = (el as? JsonPrimitive)?.doubleOrNull
    val int get() = num!!.toInt()
    val bool get() = (el as? JsonPrimitive)?.booleanOrNull
    val size get() = (el as? JsonArray)?.size ?: -1
    val items get() = this["items"]
    override fun toString() = el.toString()
}

class Res(val status: Int, val body: J, val text: String, val headers: java.net.http.HttpHeaders)

/** The API running against TEST_DATABASE_URL with a pristine schema. */
class Env : AutoCloseable {
    val db = Db.open(System.getenv("TEST_DATABASE_URL"))
    val dataDir: File = Files.createTempDirectory("ntoj-test").toFile()
    val cfg = Config(
        dataDir = dataDir.path, siteTitle = "test", cookieSecure = false, judgeToken = JUDGE_TOKEN,
        adminEmail = "root@example.com", adminName = "root", adminPassword = "root-password",
    )
    val srv: Server
    private val http = HttpClient.newHttpClient()
    private val engine: io.ktor.server.engine.EmbeddedServer<*, *>
    val url: String

    init {
        db.use { it.query("SELECT 1") { } }
        db.tx { t ->
            t.exec("DROP SCHEMA public CASCADE")
            t.exec("CREATE SCHEMA public")
        }
        db.migrate()
        srv = Server(db, cfg)
        bootstrapAdmin(srv)
        engine = httpServer(srv, "127.0.0.1", 0).also { it.start(wait = false) }
        url = "http://127.0.0.1:" + runBlocking { engine.engine.resolvedConnectors().first().port }
    }

    override fun close() {
        engine.stop(0, 1000)
        db.close()
        dataDir.deleteRecursively()
    }

    inner class Client {
        val headers = HashMap<String, String>()
        var cookie: String? = null

        fun send(method: String, path: String, body: Any? = null, contentType: String? = null): Res {
            val req = HttpRequest.newBuilder(URI(url + path)).timeout(Duration.ofSeconds(30))
            val publisher = when (body) {
                null -> HttpRequest.BodyPublishers.noBody()
                is ByteArray -> HttpRequest.BodyPublishers.ofByteArray(body)
                else -> {
                    req.header("Content-Type", "application/json")
                    HttpRequest.BodyPublishers.ofString(toJson(body).toString())
                }
            }
            contentType?.let { req.header("Content-Type", it) }
            headers.forEach { (k, v) -> req.header(k, v) }
            cookie?.let { req.header("Cookie", "$SESSION_COOKIE=$it") }
            val res = http.send(req.method(method, publisher).build(), HttpResponse.BodyHandlers.ofByteArray())
            res.headers().allValues("Set-Cookie").forEach { sc ->
                val value = sc.substringBefore(';').substringAfter('=')
                cookie = value.ifEmpty { null }
            }
            val text = String(res.body(), Charsets.UTF_8)
            val parsed = runCatching { Json.parseToJsonElement(text) }.getOrNull()
            return Res(res.statusCode(), J(parsed), text, res.headers())
        }

        fun must(status: Int, method: String, path: String, body: Any? = null): J {
            val res = send(method, path, body)
            if (res.status != status) fail("$method $path: status ${res.status}, want $status: ${res.text}")
            return res.body
        }

        fun upload(path: String, files: Map<String, String>): Res {
            val boundary = "----ntojtest${System.nanoTime()}"
            val out = java.io.ByteArrayOutputStream()
            for ((name, content) in files) {
                out.write("--$boundary\r\nContent-Disposition: form-data; name=\"files\"; filename=\"$name\"\r\nContent-Type: application/octet-stream\r\n\r\n".toByteArray())
                out.write(content.toByteArray())
                out.write("\r\n".toByteArray())
            }
            out.write("--$boundary--\r\n".toByteArray())
            return send("POST", path, out.toByteArray(), "multipart/form-data; boundary=$boundary")
        }
    }

    fun signIn(email: String, password: String) = Client().also {
        it.must(200, "POST", "/api/auth/signin", mapOf("email" to email, "password" to password))
    }

    fun signUp(name: String): Pair<Client, Int> {
        val c = Client()
        val out = c.must(201, "POST", "/api/auth/signup", mapOf("email" to "$name@example.com", "name" to name, "password" to "password-$name"))
        return c to out["id"].int
    }

    fun judge() = Client().also { it.headers["Authorization"] = "Bearer $JUDGE_TOKEN" }

    fun root() = signIn("root@example.com", "root-password")
}

class ApiTest {
    /** Runs [block] against a fresh server, or skips when no test database is configured. */
    private fun withEnv(block: Env.() -> Unit) {
        if (System.getenv("TEST_DATABASE_URL").isNullOrEmpty()) {
            println("TEST_DATABASE_URL not set; skipping API tests")
            return
        }
        Env().use(block)
    }

    @Test
    fun authAndSessions() = withEnv {
        val (alice, aliceId) = signUp("alice")
        assertEquals("alice", alice.must(200, "GET", "/api/session")["account"]["name"].str)
        val token = alice.cookie!!
        assertTrue(token.length >= 40, "session token too short")
        assertEquals(0, db.count("SELECT count(*) FROM sessions WHERE token_hash = convert_to($1, 'UTF8')", token), "raw token stored")
        assertTrue(db.one("SELECT password_hash FROM accounts WHERE id = $1", aliceId) { it.getString(1) }!!.startsWith("\$argon2id\$"))

        // Validation and duplicate handling.
        val c = Client()
        c.must(400, "POST", "/api/auth/signup", mapOf("email" to "bob@example.com", "name" to "bob", "password" to "short"))
        c.must(400, "POST", "/api/auth/signup", mapOf("email" to "not-an-email", "name" to "bob", "password" to "long-enough-pw"))
        c.must(400, "POST", "/api/auth/signup", mapOf("email" to "b@example.com", "name" to "b​b", "password" to "long-enough-pw"))
        c.must(409, "POST", "/api/auth/signup", mapOf("email" to "ALICE@example.com", "name" to "x", "password" to "long-enough-pw"))
        c.must(401, "POST", "/api/auth/signin", mapOf("email" to "alice@example.com", "password" to "wrong-password"))
        c.must(401, "POST", "/api/auth/signin", mapOf("email" to "nobody@example.com", "password" to "wrong-password"))
        c.must(400, "POST", "/api/auth/signin", mapOf("email" to "a@b.c", "password" to "x", "role" to "admin")) // unknown field
        assertEquals(415, c.send("POST", "/api/auth/signin", "email=a".toByteArray(), "application/x-www-form-urlencoded").status)
        assertEquals(413, c.send("POST", "/api/auth/signin", ByteArray(MAX_JSON_BODY + 10) { ' '.code.toByte() }, "application/json").status)
        c.must(401, "GET", "/api/account/sessions")
        c.must(401, "PATCH", "/api/account", mapOf("name" to "x"))

        // Cross-site form posts are rejected before reaching any handler.
        val creds = mapOf("email" to "alice@example.com", "password" to "password-alice")
        c.headers["Sec-Fetch-Site"] = "cross-site"
        c.must(403, "POST", "/api/auth/signin", creds)
        c.headers.clear()
        c.headers["Origin"] = "https://evil.example"
        c.must(403, "POST", "/api/auth/signin", creds)
        c.headers["Origin"] = url
        c.must(200, "POST", "/api/auth/signin", creds)
        c.headers.clear()
        c.must(204, "POST", "/api/auth/signout")

        // Profile validation: only https image URLs.
        alice.must(400, "PATCH", "/api/account", mapOf("name" to "alice", "photo_url" to "javascript:alert(1)", "cover_url" to "", "motto" to ""))
        alice.must(204, "PATCH", "/api/account", mapOf("name" to "Alice", "photo_url" to "https://example.com/a.png", "cover_url" to "", "motto" to "hi"))

        // Changing the password signs every other device out.
        val second = signIn("alice@example.com", "password-alice")
        alice.must(403, "POST", "/api/account/password", mapOf("old" to "nope", "new" to "new-password-1"))
        alice.must(204, "POST", "/api/account/password", mapOf("old" to "password-alice", "new" to "new-password-1"))
        second.must(401, "GET", "/api/account/sessions")
        alice.must(200, "GET", "/api/account/sessions")
        Client().must(401, "POST", "/api/auth/signin", creds)

        // Ordinary accounts cannot reach administration.
        alice.must(403, "GET", "/api/admin/accounts")
        alice.must(403, "POST", "/api/admin/problems", mapOf("name" to "x"))
        Client().must(401, "GET", "/api/admin/dashboard")

        // Admin: restrict alice to one IP, then disable her.
        val root = root()
        assertEquals(1, root.must(200, "GET", "/api/admin/accounts?q=alice").items.size)
        val path = "/api/admin/accounts/$aliceId"
        root.must(400, "PATCH", path, mapOf("allowed_ip" to "example.com"))
        root.must(204, "PATCH", path, mapOf("allowed_ip" to "10.9.8.7"))
        alice.must(401, "GET", "/api/account/sessions") // existing session no longer honoured
        Client().must(403, "POST", "/api/auth/signin", mapOf("email" to "alice@example.com", "password" to "new-password-1"))
        root.must(204, "PATCH", path, mapOf("allowed_ip" to "", "disabled" to true))
        Client().must(401, "POST", "/api/auth/signin", mapOf("email" to "alice@example.com", "password" to "new-password-1"))
        root.must(400, "PATCH", "/api/admin/accounts/1", mapOf("role" to "user")) // no self-demotion

        root.must(204, "POST", "/api/auth/signout")
        root.must(401, "GET", "/api/account/sessions")

        // Password hashes and emails never appear in public payloads.
        val profile = Client().send("GET", "/api/accounts/1")
        assertEquals(200, profile.status)
        assertFalse("argon2" in profile.text || "root@example.com" in profile.text, "public profile leaks private data")
        assertEquals("nosniff", profile.headers.firstValue("X-Content-Type-Options").orElse(""))
    }

    @Test
    fun signInRateLimit() = withEnv {
        val c = Client()
        var last = 0
        repeat(12) { last = c.send("POST", "/api/auth/signin", mapOf("email" to "root@example.com", "password" to "bad-password")).status }
        assertEquals(429, last)
    }

    @Test
    fun forwardedForIsOnlyTrustedFromProxies() = withEnv {
        val c = Client()
        c.headers["X-Forwarded-For"] = "203.0.113.9"
        c.must(401, "POST", "/api/auth/signin", mapOf("email" to "x@example.com", "password" to "wrong-password"))
        assertEquals("127.0.0.1", db.one("SELECT operator_ip FROM audit_logs ORDER BY id DESC LIMIT 1") { it.getString(1) })
    }

    @Test
    fun problemSubmissionAndJudgeFlow() = withEnv {
        val root = root()
        val (alice, _) = signUp("alice")
        val (bob, _) = signUp("bob")
        val guest = Client()

        val pid = root.must(201, "POST", "/api/admin/problems", mapOf("name" to "A + B"))["id"].int
        val p = "/api/problems/$pid"
        val ap = "/api/admin/problems/$pid"
        fun sub(language: String, code: String, problem: Int = pid) = mapOf("problem_id" to problem, "language" to language, "code" to code)

        // New problems are hidden: invisible to everyone but administrators.
        alice.must(404, "GET", p)
        guest.must(404, "GET", p)
        alice.must(404, "POST", "/api/submissions", sub("g++", "int main(){}"))
        assertEquals(0, alice.must(200, "GET", "/api/problems").items.size)

        // Testdata upload: names are validated, files land under server-chosen names.
        assertEquals(200, root.upload("$ap/testcases", mapOf("../../evil.in" to "x")).status) // path is stripped to evil.in
        assertEquals(400, root.upload("$ap/testcases", mapOf("evil.sh" to "x")).status)
        assertEquals(403, alice.upload("$ap/testcases", mapOf("1.in" to "x")).status)
        assertEquals(200, root.upload("$ap/testcases", mapOf("1.in" to "1 2\n", "1.out" to "3\n", "2.in" to "5 5\n", "2.out" to "10\n")).status)
        val full = root.must(200, "GET", ap)
        val ids = (0 until full["testcases"].size).associate { full["testcases"][it]["name"].str!! to full["testcases"][it]["id"].int }
        assertEquals(setOf("1", "2", "evil"), ids.keys)
        root.must(204, "DELETE", "$ap/testcases/${ids["evil"]}")
        assertEquals(listOf("problems"), dataDir.list()!!.toList(), "unexpected files in the data directory")

        root.must(400, "PUT", "$ap/subtasks", listOf(mapOf("score" to 50, "testcase_ids" to listOf(999999), "depends" to emptyList<Int>())))
        root.must(400, "PUT", "$ap/subtasks", listOf(mapOf("score" to 50, "testcase_ids" to listOf(ids["1"]), "depends" to listOf(2))))
        root.must(
            204, "PUT", "$ap/subtasks",
            listOf(
                mapOf("score" to 40, "testcase_ids" to listOf(ids["1"]), "depends" to emptyList<Int>()),
                mapOf("score" to 60, "testcase_ids" to listOf(ids["2"]), "depends" to listOf(1)),
            ),
        )
        root.must(400, "PATCH", ap, mapOf("tags" to listOf("<script>")))
        root.must(
            204, "PATCH", ap,
            mapOf("status" to "online", "statement" to "Add two numbers.", "tags" to listOf("math"), "languages" to listOf("g++", "python3")),
        )

        // Attachments: HTML is never served inline.
        assertEquals(200, root.upload("$ap/files", mapOf("note.html" to "<script>alert(1)</script>", "pic.png" to "png")).status)
        val html = guest.send("GET", "$p/files/note.html")
        assertTrue(html.headers.firstValue("Content-Disposition").orElse("").startsWith("attachment"), "html attachment served inline")
        assertEquals("application/octet-stream", html.headers.firstValue("Content-Type").orElse(""))
        assertTrue("sandbox" in html.headers.firstValue("Content-Security-Policy").orElse(""))
        assertEquals("image/png", guest.send("GET", "$p/files/pic.png").headers.firstValue("Content-Type").orElse(""))
        guest.must(404, "GET", "$p/files/..%2f..%2ftestdata%2f1.in")

        val got = guest.must(200, "GET", p)
        assertEquals("Add two numbers.", got["statement"].str)
        assertEquals(2, got["files"].size)

        // Submitting.
        guest.must(401, "POST", "/api/submissions", sub("g++", "x"))
        alice.must(400, "POST", "/api/submissions", sub("java", "x")) // not allowed for the problem
        alice.must(400, "POST", "/api/submissions", sub("cobol", "x")) // unknown
        alice.must(400, "POST", "/api/submissions", sub("g++", "  \n")) // empty
        alice.must(400, "POST", "/api/submissions", sub("g++", "a".repeat(MAX_CODE_SIZE + 1)))
        cfg.submitCooldown = 60
        val sid = alice.must(201, "POST", "/api/submissions", sub("g++", "secret source"))["id"].int
        alice.must(429, "POST", "/api/submissions", sub("g++", "again"))
        cfg.submitCooldown = 0
        val sp = "/api/submissions/$sid"

        // Source code: author and administrators only.
        assertEquals("secret source", alice.must(200, "GET", sp)["code"].str)
        bob.must(200, "GET", sp).let {
            assertTrue(it["code"].isNull, "code leaked to another user")
            assertEquals(false, it["can_rejudge"].bool)
        }
        assertTrue(guest.must(200, "GET", sp)["code"].isNull, "code leaked to guest")
        assertEquals("secret source", root.must(200, "GET", sp)["code"].str)
        assertEquals(1, db.count("SELECT count(*) FROM audit_logs WHERE type = 'manage.submission.view'"), "admin code view not audited")

        // Judge API requires its token.
        guest.must(401, "POST", "/api/judge/claim")
        alice.must(401, "POST", "/api/judge/claim")
        val j = judge()
        val task = j.must(200, "POST", "/api/judge/claim")
        assertEquals(sid to "secret source", task["id"].int to task["code"].str)
        assertEquals(2, task["problem"]["subtasks"].size)
        j.send("GET", "/api/judge/testcases/${ids["1"]}/in").let { assertEquals(200 to "1 2\n", it.status to it.text) }
        assertEquals(204, j.send("POST", "/api/judge/claim").status)
        assertEquals(100, alice.must(200, "GET", sp)["state"].int)
        val rp = "/api/judge/submissions/$sid/result"
        j.must(400, "POST", rp, mapOf("testcases" to listOf(mapOf("id" to 999999, "state" to 1))))
        j.must(400, "POST", rp, mapOf("testcases" to listOf(mapOf("id" to ids["1"], "state" to 100))))
        j.must(
            204, "POST", rp,
            mapOf(
                "testcases" to listOf(
                    mapOf("id" to ids["1"], "state" to 1, "time_ms" to 12, "memory_kb" to 800, "message" to "checker says ok"),
                    mapOf("id" to ids["2"], "state" to 3, "time_ms" to 30, "memory_kb" to 900),
                ),
            ),
        )
        j.must(409, "POST", rp, mapOf("state" to 9, "message" to "late"))

        val v = alice.must(200, "GET", sp)
        assertEquals(Triple(2, 40.0, 30.0), Triple(v["state"].int, v["score"].num, v["time_ms"].num))
        assertEquals(2, v["result"]["testcases"].size)
        assertEquals("1", v["result"]["testcases"][0]["name"].str)
        assertEquals("", v["result"]["testcases"][0]["message"].str, "checker output must be hidden from the author")
        assertEquals(60.0, v["result"]["subtasks"][1]["max_score"].num)
        assertEquals("checker says ok", root.must(200, "GET", sp)["result"]["testcases"][0]["message"].str)

        // Rejudge: admin only; then a full solve.
        alice.must(404, "POST", "$sp/rejudge")
        root.must(204, "POST", "$sp/rejudge")
        j.must(200, "POST", "/api/judge/claim")
        j.must(204, "POST", rp, mapOf("testcases" to listOf(mapOf("id" to ids["1"], "state" to 1), mapOf("id" to ids["2"], "state" to 1))))
        alice.must(200, "GET", sp).let { assertEquals(1 to 100.0, it["state"].int to it["score"].num) }

        // Compile error path.
        val sid2 = bob.must(201, "POST", "/api/submissions", sub("python3", "print("))["id"].int
        j.must(200, "POST", "/api/judge/claim")
        j.must(204, "POST", "/api/judge/submissions/$sid2/result", mapOf("state" to 9, "message" to "SyntaxError"))
        bob.must(200, "GET", "/api/submissions/$sid2").let { assertEquals(9 to "SyntaxError", it["state"].int to it["message"].str) }
        assertEquals("", alice.must(200, "GET", "/api/submissions/$sid2")["message"].str, "compiler output leaked to another user")

        // Lists, stats, rank.
        assertEquals(1, guest.must(200, "GET", "/api/submissions?problem_id=$pid&state=1").items.size)
        guest.must(400, "GET", "/api/submissions?state=abc")
        alice.must(200, "GET", p)["stats"].let {
            assertEquals(Triple(1, 2, 1), Triple(it["solved_users"].int, it["tried_users"].int, it["my_state"].int))
        }
        guest.must(200, "GET", "/api/rank").items[0].let { assertEquals("alice" to 100.0, it["name"].str to it["rate"].num) }
        assertEquals(1, guest.must(200, "GET", "$p/rank").items.size)
        assertEquals(1, guest.must(200, "GET", "/api/problems?q=A%20%2B&tag=math").items.size)
        assertEquals(0, guest.must(200, "GET", "/api/problems?tag=nope").items.size)
        assertEquals("math", guest.must(200, "GET", "/api/tags")[0]["tag"].str)

        // Hiding the problem again hides its submissions from other users.
        root.must(204, "PATCH", ap, mapOf("status" to "hidden"))
        bob.must(404, "GET", sp)
        alice.must(200, "GET", sp)
        assertEquals(0, bob.must(200, "GET", "/api/submissions").items.size)
        guest.must(404, "GET", "$p/files/pic.png")

        root.must(204, "DELETE", ap)
        assertFalse(srv.problemDir(pid).exists(), "problem directory not removed")
    }

    @Test
    fun contestFlow() = withEnv {
        val root = root()
        val (alice, aliceId) = signUp("alice")
        val (bob, bobId) = signUp("bob")
        val (carol, carolId) = signUp("carol")
        val guest = Client()
        val j = judge()

        alice.must(403, "POST", "/api/contests", mapOf("name" to "x"))
        val cid = root.must(201, "POST", "/api/contests", mapOf("name" to "Round 1"))["id"].int
        val cp = "/api/contests/$cid"

        fun mk(name: String, status: String): Int {
            val id = root.must(201, "POST", "/api/admin/problems", mapOf("name" to name))["id"].int
            root.must(204, "PATCH", "/api/admin/problems/$id", mapOf("status" to status))
            return id
        }
        val p1 = mk("Contest task", "contest")
        val hidden = mk("Secret", "hidden")
        val online = mk("Public", "online")
        assertEquals(200, root.upload("/api/admin/problems/$p1/testcases", mapOf("1.in" to "", "1.out" to "")).status)
        assertEquals(200, root.upload("/api/admin/problems/$p1/files", mapOf("fig.png" to "png")).status)
        fun sub(problem: Int, code: String = "x", contest: Int? = cid) =
            mapOf("problem_id" to problem, "contest_id" to contest, "language" to "g++", "code" to code)

        val now = Instant.now()
        root.must(400, "PATCH", cp, mapOf("start_at" to now, "end_at" to now.minusSeconds(3600)))
        root.must(400, "PATCH", cp, mapOf("start_at" to "yesterday"))
        root.must(
            204, "PATCH", cp,
            mapOf(
                "start_at" to now.plusSeconds(3600), "end_at" to now.plusSeconds(3 * 3600), "reg_end_at" to now.plusSeconds(1800),
                "reg_mode" to "approval", "cooldown_sec" to 0, "public_scoreboard" to true, "allow_other_pages" to false,
                "desc_before" to "BEFORE", "desc_during" to "DURING", "desc_after" to "AFTER",
            ),
        )
        root.must(
            204, "PUT", "$cp/manage/problems",
            listOf(
                mapOf("problem_id" to p1, "score_type" to "best", "result_style" to "total"),
                mapOf("problem_id" to hidden, "score_type" to "best", "result_style" to "full"),
            ),
        )

        // Management is closed to non-administrators of the contest.
        alice.must(403, "GET", "$cp/manage")
        alice.must(403, "PATCH", cp, mapOf("name" to "hacked"))
        alice.must(403, "PUT", "$cp/manage/problems", emptyList<Any>())
        alice.must(403, "PUT", "$cp/manage/users/$aliceId", mapOf("status" to "admin"))
        guest.must(401, "GET", "$cp/manage")

        // Registration with approval.
        guest.must(401, "POST", "$cp/register")
        assertEquals("requested", alice.must(200, "POST", "$cp/register")["status"].str)
        alice.must(403, "POST", "$cp/register")
        bob.must(200, "POST", "$cp/register")
        bob.must(204, "DELETE", "$cp/register")
        bob.must(200, "POST", "$cp/register")
        root.must(204, "PUT", "$cp/manage/users/$aliceId", mapOf("status" to "approved"))
        root.must(204, "PUT", "$cp/manage/users/$bobId", mapOf("status" to "approved"))
        root.must(400, "PUT", "$cp/manage/users/999999", mapOf("status" to "approved"))

        // A contest manager who is not a site admin cannot pull in hidden problems or mint managers.
        root.must(204, "PUT", "$cp/manage/users/$carolId", mapOf("status" to "admin"))
        carol.must(200, "GET", "$cp/manage").let {
            assertEquals("BEFORE", it["contest"]["desc_before"].str)
            assertEquals(false, it["can_manage_admins"].bool)
        }
        val other = mk("Other secret", "hidden")
        carol.must(400, "PUT", "$cp/manage/problems", listOf(mapOf("problem_id" to other, "score_type" to "best", "result_style" to "full")))
        carol.must(403, "PUT", "$cp/manage/users/$bobId", mapOf("status" to "admin"))
        carol.must(403, "DELETE", "$cp/manage/users/1")
        carol.must(403, "DELETE", cp)

        // Before the start: nothing is visible to contestants.
        alice.must(200, "GET", cp).let {
            assertEquals("BEFORE" to "before", it["description"].str to it["access"]["phase"].str)
            assertTrue(it["contest"]["desc_during"].isNull, "other phases' descriptions must not be sent")
        }
        alice.must(403, "GET", "$cp/problems")
        alice.must(403, "GET", "$cp/problems/$p1")
        alice.must(403, "GET", "$cp/scoreboard")
        alice.must(403, "POST", "/api/submissions", sub(p1))
        alice.must(404, "GET", "/api/problems/$p1/files/fig.png")
        alice.must(404, "GET", "/api/problems/$p1") // contest problems are not on the public site

        // Start the contest.
        db.exec("UPDATE contests SET start_at = now() - interval '1 hour', reg_end_at = now() - interval '2 hours' WHERE id = $1", cid)
        alice.must(200, "GET", cp).let { assertEquals("DURING" to true, it["description"].str to it["access"]["can_submit"].bool) }
        alice.must(200, "GET", "$cp/problems").let {
            assertEquals(1, it.size, "contestants must not see hidden problems")
            assertEquals(p1, it[0]["id"].int)
        }
        alice.must(200, "GET", "$cp/problems/$p1")
        alice.must(404, "GET", "$cp/problems/$hidden")
        alice.must(404, "GET", "$cp/problems/$online")
        alice.must(200, "GET", "/api/problems/$p1/files/fig.png")
        guest.must(403, "GET", "$cp/problems")
        guest.must(404, "GET", "/api/problems/$p1/files/fig.png")
        val (dave, _) = signUp("dave") // not registered, registration closed
        dave.must(403, "POST", "$cp/register")
        dave.must(403, "GET", "$cp/problems")
        dave.must(403, "POST", "/api/submissions", sub(p1))

        // Lockdown: contestants are confined to the contest while it runs.
        assertEquals(cid, alice.must(200, "GET", "/api/session")["lockdown"].int)
        alice.must(403, "GET", "/api/problems")
        alice.must(403, "GET", "/api/problems/$online")
        alice.must(403, "GET", "/api/submissions")
        alice.must(403, "POST", "/api/submissions", sub(online, contest = null))
        dave.must(200, "GET", "/api/problems")

        // Submissions inside the contest.
        alice.must(404, "POST", "/api/submissions", sub(hidden))
        alice.must(404, "POST", "/api/submissions", sub(online))
        val sa = alice.must(201, "POST", "/api/submissions", sub(p1, "alice code"))["id"].int
        val sb = bob.must(201, "POST", "/api/submissions", sub(p1, "bob code"))["id"].int
        val tid = db.one("SELECT id FROM testcases WHERE problem_id = $1", p1) { it.getInt(1) }!!
        for ((id, state) in listOf(sa to 1, sb to 3)) {
            assertEquals(id, j.must(200, "POST", "/api/judge/claim")["id"].int, "queue order")
            j.must(204, "POST", "/api/judge/submissions/$id/result", mapOf("testcases" to listOf(mapOf("id" to tid, "state" to state))))
        }

        // While running, contestants see only their own submissions, with the configured detail level.
        bob.must(404, "GET", "/api/submissions/$sa")
        guest.must(404, "GET", "/api/submissions/$sa")
        dave.must(404, "GET", "/api/submissions/$sa")
        alice.must(200, "GET", "/api/submissions/$sa").let {
            assertEquals("total" to "alice code", it["result_style"].str to it["code"].str)
            assertTrue(it["result"]["testcases"].isNull, "result style not applied")
        }
        assertEquals(1, bob.must(200, "GET", "/api/submissions?contest_id=$cid").items.size)
        assertEquals(0, bob.must(200, "GET", "/api/submissions?contest_id=$cid&account_id=$aliceId").items.size, "scope bypassed")
        assertEquals(0, guest.must(200, "GET", "/api/submissions?contest_id=$cid").items.size)
        assertEquals(0, dave.must(200, "GET", "/api/submissions").items.size, "contest submissions leak into the public list")
        assertEquals(2, carol.must(200, "GET", "/api/submissions?contest_id=$cid").items.size)
        carol.must(204, "POST", "/api/submissions/$sb/rejudge")
        bob.must(404, "POST", "/api/submissions/$sb/rejudge")
        j.must(200, "POST", "/api/judge/claim")
        j.must(204, "POST", "/api/judge/submissions/$sb/result", mapOf("testcases" to listOf(mapOf("id" to tid, "state" to 3))))

        guest.must(200, "GET", "$cp/scoreboard").let {
            assertEquals(2, it["rows"].size)
            assertEquals("alice" to 100.0, it["rows"][0]["name"].str to it["rows"][0]["total"].num)
            assertEquals(true, it["rows"][0]["cells"]["$p1"]["solved"].bool)
            assertEquals(1, it["problems"].size)
        }

        // Questions and announcements.
        dave.must(403, "POST", "$cp/questions", mapOf("subject" to "s", "content" to "c"))
        alice.must(201, "POST", "$cp/questions", mapOf("subject" to "Clarify", "content" to "Is n > 0?"))
        bob.must(403, "POST", "$cp/manage/announcements", mapOf("subject" to "s", "content" to "c"))
        root.must(201, "POST", "$cp/manage/announcements", mapOf("subject" to "Note", "content" to "n > 0"))
        bob.must(200, "GET", "$cp/qa").let { assertEquals(0 to 1, it["questions"].size to it["announcements"].size) }
        val qid = carol.must(200, "GET", "$cp/qa")["questions"][0]["id"].int
        bob.must(403, "POST", "$cp/manage/questions/$qid/reply", mapOf("reply" to "x"))
        carol.must(204, "POST", "$cp/manage/questions/$qid/reply", mapOf("reply" to "Yes"))
        assertEquals(2, alice.must(200, "GET", cp)["unread"].int)
        alice.must(200, "GET", "$cp/qa")
        assertEquals(0, alice.must(200, "GET", cp)["unread"].int)

        // After the end: results open up, submitting closes, lockdown lifts.
        db.exec("UPDATE contests SET start_at = now() - interval '3 hours', end_at = now() - interval '1 minute' WHERE id = $1", cid)
        alice.must(403, "POST", "/api/submissions", sub(p1, "late"))
        alice.must(200, "GET", "/api/problems")
        bob.must(200, "GET", "/api/submissions/$sa").let {
            assertTrue(it["code"].isNull)
            assertEquals("full", it["result_style"].str)
        }
        assertEquals("AFTER", alice.must(200, "GET", cp)["description"].str)
        guest.must(200, "GET", "$cp/problems")
        guest.must(404, "GET", "$cp/problems/$hidden")

        assertTrue(root.must(200, "GET", "$cp/manage/logs").items.size > 0, "contest audit log is empty")
        alice.must(403, "GET", "$cp/manage/logs")
        root.must(204, "DELETE", cp)
        guest.must(404, "GET", cp)
    }

    @Test
    fun flexibleContestSession() = withEnv {
        val root = root()
        val (alice, aliceId) = signUp("alice")
        val cid = root.must(201, "POST", "/api/contests", mapOf("name" to "Flex"))["id"].int
        val cp = "/api/contests/$cid"
        val now = Instant.now()
        root.must(400, "PATCH", cp, mapOf("time_mode" to "flexible")) // needs a duration
        root.must(
            204, "PATCH", cp,
            mapOf("time_mode" to "flexible", "duration_sec" to 3600, "start_at" to now.minusSeconds(3600), "end_at" to now.plusSeconds(1800)),
        )
        root.must(204, "PUT", "$cp/manage/users/$aliceId", mapOf("status" to "approved"))

        alice.must(403, "GET", "$cp/problems")
        val out = alice.must(200, "POST", "$cp/start")
        val length = Duration.between(Instant.parse(out["start"].str), Instant.parse(out["end"].str))
        assertTrue(length in Duration.ofMinutes(29)..Duration.ofMinutes(31), "session must be clipped to the contest end, got $length")
        alice.must(403, "POST", "$cp/start") // cannot restart
        alice.must(200, "GET", "$cp/problems")
        root.must(204, "POST", "$cp/manage/users/$aliceId/reset-session")
        alice.must(403, "GET", "$cp/problems")
    }

    @Test
    fun siteFeatures() = withEnv {
        val root = root()
        val (alice, aliceId) = signUp("alice")
        val (bob, _) = signUp("bob")
        val guest = Client()

        // Bulletins.
        alice.must(403, "POST", "/api/admin/bulletins", mapOf("title" to "t", "content" to "c"))
        root.must(400, "POST", "/api/admin/bulletins", mapOf("title" to "t", "content" to "c", "color" to "url(x)"))
        val bid = root.must(201, "POST", "/api/admin/bulletins", mapOf("title" to "Hello", "content" to "**hi**", "color" to "red", "pinned" to true))["id"].int
        assertEquals("root", guest.must(200, "GET", "/api/bulletins/$bid")["author_name"].str)
        root.must(204, "PUT", "/api/admin/bulletins/$bid", mapOf("title" to "Hello 2", "content" to "x"))
        assertEquals("Hello 2", guest.must(200, "GET", "/api/bulletins")[0]["title"].str)
        root.must(204, "DELETE", "/api/admin/bulletins/$bid")
        guest.must(404, "GET", "/api/bulletins/$bid")

        // Problem classes: private classes stay private, only owners may edit.
        alice.must(403, "POST", "/api/classes", mapOf("name" to "Official", "problem_ids" to listOf(1), "official" to true))
        val cls = alice.must(201, "POST", "/api/classes", mapOf("name" to "Mine", "problem_ids" to listOf(3, 1, 3), "public" to false))["id"].int
        bob.must(404, "GET", "/api/classes/$cls")
        bob.must(404, "PUT", "/api/classes/$cls", mapOf("name" to "Stolen", "problem_ids" to listOf(1), "public" to true))
        bob.must(404, "DELETE", "/api/classes/$cls")
        bob.must(204, "PUT", "/api/classes/$cls/collect") // silently ignored
        assertEquals(0, db.count("SELECT count(*) FROM class_collections"), "collected an invisible class")
        assertEquals(2, alice.must(200, "GET", "/api/classes/$cls")["problem_count"].int)
        alice.must(204, "PUT", "/api/classes/$cls", mapOf("name" to "Mine", "problem_ids" to listOf(1), "public" to true))
        bob.must(204, "PUT", "/api/classes/$cls/collect")
        assertEquals(true, bob.must(200, "GET", "/api/classes/$cls")["collected"].bool)
        assertEquals(1, guest.must(200, "GET", "/api/classes").size)
        alice.must(204, "DELETE", "/api/classes/$cls")

        // Questions: private to the asker.
        val qid = alice.must(201, "POST", "/api/questions", mapOf("content" to "Help?"))["id"].int
        bob.must(404, "DELETE", "/api/questions/$qid")
        bob.must(403, "POST", "/api/admin/questions/$qid/reply", mapOf("reply" to "x"))
        root.must(204, "POST", "/api/admin/questions/$qid/reply", mapOf("reply" to "Sure"))
        assertEquals(1, alice.must(200, "GET", "/api/session")["account"]["unread_replies"].int)
        assertEquals("Sure", alice.must(200, "GET", "/api/questions")[0]["reply"].str)
        assertEquals(0, alice.must(200, "GET", "/api/session")["account"]["unread_replies"].int)
        assertEquals(0, bob.must(200, "GET", "/api/questions").size, "question leaked to another account")

        // Boards.
        val now = Instant.now()
        val board = hashMapOf<String, Any?>(
            "name" to "Class A", "visible" to false, "start_at" to now.minusSeconds(3600), "end_at" to now.plusSeconds(3600),
            "problem_ids" to listOf(1), "account_ids" to listOf(aliceId),
        )
        alice.must(403, "POST", "/api/admin/boards", board)
        val bd = root.must(201, "POST", "/api/admin/boards", board)["id"].int
        guest.must(404, "GET", "/api/boards/$bd")
        board["visible"] = true
        root.must(204, "PUT", "/api/admin/boards/$bd", board)
        assertEquals(1, guest.must(200, "GET", "/api/boards/$bd")["rows"].size)
        assertEquals(1, guest.must(200, "GET", "/api/boards").size)

        // Audit log and dashboard are admin-only and record sign-ins.
        alice.must(403, "GET", "/api/admin/logs")
        assertTrue(root.must(200, "GET", "/api/admin/logs?type=signin").items.size > 0, "sign-ins are not audited")
        assertEquals(true, root.must(200, "GET", "/api/admin/dashboard")["judge_configured"].bool)
        guest.must(404, "GET", "/api/nope")
        assertEquals("ok", guest.send("GET", "/api/healthz").text)
    }

    @Test
    fun passwordHashingRoundTrip() {
        val hash = Auth.hashPassword("correct horse")
        assertTrue(Auth.verifyPassword(hash, "correct horse"))
        assertFalse(Auth.verifyPassword(hash, "wrong horse"))
        assertFalse(Auth.verifyPassword("\$argon2id\$v=19\$m=999999999,t=2,p=1\$AAAA\$AAAA", "x"), "absurd cost parameters must be refused")
        assertNotNull(Auth.passwordProblem("short"))
        assertNotNull(parseIp("::ffff:10.0.0.1").also { assertEquals("10.0.0.1", it!!.hostAddress) })
        assertEquals(null, parseIp("example.com"))
        assertTrue(Cidr.parse("172.16.0.0/12")!!.contains(parseIp("172.20.1.1")!!))
        assertFalse(Cidr.parse("172.16.0.0/12")!!.contains(parseIp("172.32.0.1")!!))
    }
}
