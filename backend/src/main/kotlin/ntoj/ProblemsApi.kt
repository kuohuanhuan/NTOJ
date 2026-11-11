package ntoj

import io.ktor.http.ContentDisposition
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.content.PartData
import io.ktor.server.http.content.LocalFileContent
import io.ktor.server.request.contentLength
import io.ktor.server.request.contentType
import io.ktor.server.request.receiveMultipart
import io.ktor.server.response.header
import io.ktor.utils.io.jvm.javaio.toInputStream
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

const val MAX_TESTDATA_UPLOAD = 512L shl 20
const val MAX_FILE_UPLOAD = 64L shl 20
const val MAX_STATEMENT = 256 shl 10

private val FILE_NAME = Regex("""^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$""")
private val TEST_NAME = Regex("""^([A-Za-z0-9][A-Za-z0-9_-]{0,63})\.(in|out|ans)$""")
private val TAG = Regex("""^[\p{L}\p{N}][\p{L}\p{N} _+#.-]{0,31}$""")

/**
 * Attachment types safe to render in the browser. Anything else (HTML and
 * SVG in particular) is forced to download.
 */
private val INLINE_TYPES = mapOf(
    "png" to ContentType.Image.PNG, "jpg" to ContentType.Image.JPEG, "jpeg" to ContentType.Image.JPEG,
    "gif" to ContentType.Image.GIF, "webp" to ContentType("image", "webp"), "pdf" to ContentType.Application.Pdf,
    "txt" to ContentType.Text.Plain.withParameter("charset", "utf-8"),
)

fun Server.problemDir(id: Int, sub: String = ""): File = File(File(File(cfg.dataDir, "problems"), id.toString()), sub)

fun Server.testcaseFile(problemId: Int, testcaseId: Int, kind: String) = File(problemDir(problemId, "testdata"), "$testcaseId.$kind")

private fun Server.listFiles(problemId: Int): List<Map<String, Any?>> =
    (problemDir(problemId, "files").listFiles() ?: emptyArray())
        .filter { it.isFile && FILE_NAME.matches(it.name) }.sortedBy { it.name }
        .map { mapOf("name" to it.name, "size" to it.length()) }

/** Adds computed fields to a JSON object that PostgreSQL produced. */
private fun withFields(rawJson: String, vararg extra: Pair<String, Any?>): JsonObject =
    JsonObject(dbJson.parseToJsonElement(rawJson).jsonObject + extra.associate { (k, v) -> k to toJson(v) })

suspend fun listTags(c: Ctx) = c.okRaw(
    c.db.jsonList(
        """SELECT tag, count(*) AS count FROM problems, unnest(tags) AS tag
           WHERE status = 'online' GROUP BY tag ORDER BY count(*) DESC, tag LIMIT 200""",
    ),
)

suspend fun listProblems(c: Ctx) {
    val page = c.page(50)
    val args = arrayListOf<Any?>(c.admin, c.acctId)
    // $2 is cast here so the count query, which may not use it otherwise, still types it.
    var where = "((\$1 AND p.status <> 'contest') OR p.status = 'online') AND \$2::int >= 0"
    val term = c.query("q").trim()
    if (term.isNotEmpty()) {
        args += "%" + escapeLike(term) + "%"
        args += term.removePrefix("#")
        where += " AND (p.name ILIKE \$${args.size - 1} OR p.id::text = \$${args.size})"
    }
    c.query("tag").takeIf { it.isNotEmpty() }?.let {
        args += it
        where += " AND \$${args.size} = ANY (p.tags)"
    }
    c.queryInt("class_id")?.let {
        args += it
        where += """ AND p.id IN (SELECT unnest(problem_ids) FROM problem_classes
            WHERE id = $${args.size} AND (public OR owner_id = $2 OR ($1 AND owner_id IS NULL)))"""
    }
    val total = c.db.count("SELECT count(*) FROM problems p WHERE $where", *args.toTypedArray())
    args += page.limit
    args += page.offset
    val items = c.db.jsonList(
        """SELECT p.id, p.name, p.status, p.tags, p.allow_submit, st.tried_users, st.solved_users, b.state AS my_state, b.score AS my_score
           FROM (SELECT * FROM problems p WHERE $where ORDER BY p.id LIMIT $${args.size - 1} OFFSET $${args.size}) p
           LEFT JOIN LATERAL (
               SELECT count(DISTINCT account_id) AS tried_users, count(DISTINCT account_id) FILTER (WHERE state = 1) AS solved_users
               FROM submissions WHERE problem_id = p.id AND contest_id IS NULL AND state < 100) st ON true
           LEFT JOIN LATERAL (
               SELECT min(state) AS state, max(score) AS score
               FROM submissions WHERE problem_id = p.id AND account_id = $2 AND contest_id IS NULL AND state < 100) b ON true
           ORDER BY p.id""",
        *args.toTypedArray(),
    )
    c.paged(items, total, page)
}

/** The contestant-facing view of a problem. */
fun problemJson(c: Ctx, id: Int): JsonObject = withFields(
    c.db.jsonRow(
        """SELECT p.id, p.name, p.status, p.allow_submit, p.tags, p.statement, p.time_limit_ms, p.memory_limit_kb,
               p.checker, p.languages, p.score_precision,
               (SELECT count(*) FROM testcases t WHERE t.problem_id = p.id) AS testcase_count,
               (SELECT coalesce(jsonb_agg(jsonb_build_object('idx', st.idx, 'score', st.score,
                   'testcases', cardinality(st.testcase_ids), 'depends', st.depends) ORDER BY st.idx), '[]')
                   FROM subtasks st WHERE st.problem_id = p.id) AS subtasks
           FROM problems p WHERE p.id = $1""",
        id,
    ),
    "files" to c.s.listFiles(id),
)

private fun problemStatus(c: Ctx, id: Int): String? = c.db.one("SELECT status FROM problems WHERE id = $1", id) { it.getString(1) }

suspend fun getProblem(c: Ctx) {
    val id = c.pathInt("id")
    val problem = problemJson(c, id)
    // Hidden and contest-only problems do not exist for ordinary visitors.
    if (problem.getValue("status").jsonPrimitive.content != "online" && !c.admin) throw notFound()
    val stats = c.db.jsonRow(
        """SELECT count(*) AS submissions, count(*) FILTER (WHERE state = 1) AS ac_submissions,
               count(DISTINCT account_id) AS tried_users, count(DISTINCT account_id) FILTER (WHERE state = 1) AS solved_users,
               min(state) FILTER (WHERE account_id = $2) AS my_state, max(score) FILTER (WHERE account_id = $2) AS my_score
           FROM submissions WHERE problem_id = $1 AND contest_id IS NULL AND state < 100""",
        id, c.acctId,
    )
    c.ok(JsonObject(problem + ("stats" to raw(stats))))
}

suspend fun problemRank(c: Ctx) {
    val id = c.pathInt("id")
    val status = problemStatus(c, id) ?: throw notFound()
    if (status != "online" && !c.admin) throw notFound()
    val page = c.page(50)
    val items = c.db.jsonList(
        """SELECT * FROM (
               SELECT DISTINCT ON (s.account_id) s.id, s.account_id, a.name AS account_name, s.language,
                   s.time_ms, s.memory_kb, s.score, s.created_at
               FROM submissions s JOIN accounts a ON a.id = s.account_id
               WHERE s.problem_id = $1 AND s.contest_id IS NULL AND s.state = 1
               ORDER BY s.account_id, s.time_ms, s.memory_kb, s.id) b
           ORDER BY time_ms, memory_kb, id LIMIT $2 OFFSET $3""",
        id, page.limit, page.offset,
    )
    val total = c.db.count("SELECT count(DISTINCT account_id) FROM submissions WHERE problem_id = $1 AND contest_id IS NULL AND state = 1", id)
    c.paged(items, total, page)
}

/**
 * Whether the caller may read a problem's statement and attachments, directly
 * or through a contest they can currently view.
 */
private fun canReadProblem(c: Ctx, id: Int): Boolean {
    val status = problemStatus(c, id) ?: return false
    if (c.admin) return true
    if (status == "online" && lockdownContest(c) == 0) return true
    if (status == "hidden") return false
    return c.db.query("SELECT contest_id FROM contest_problems WHERE problem_id = $1", id) { it.getInt(1) }
        .any { loadAccess(c, it).second.canViewProblems }
}

suspend fun getProblemFile(c: Ctx) {
    val id = c.pathInt("id")
    val name = c.call.parameters["name"].orEmpty()
    if (!FILE_NAME.matches(name) || !canReadProblem(c, id)) throw notFound()
    serveFile(c, File(c.s.problemDir(id, "files"), name), name)
}

suspend fun serveFile(c: Ctx, file: File, name: String) {
    if (!file.isFile) throw notFound()
    val inline = INLINE_TYPES[name.substringAfterLast('.', "").lowercase()]
    val disposition = (if (inline != null) ContentDisposition.Inline else ContentDisposition.Attachment)
        .withParameter(ContentDisposition.Parameters.FileName, name)
    c.call.response.header(HttpHeaders.ContentDisposition, disposition.toString())
    c.call.response.header(HttpHeaders.CacheControl, "private, no-cache")
    c.respondContent(LocalFileContent(file, inline ?: ContentType.Application.OctetStream), "default-src 'none'; sandbox")
}

// ---- Administration ----

suspend fun adminListProblems(c: Ctx) {
    val page = c.page(50)
    val items = c.db.jsonList(
        """SELECT p.id, p.name, p.status, p.allow_submit, p.tags, p.updated_at,
               (SELECT count(*) FROM testcases t WHERE t.problem_id = p.id) AS testcase_count,
               (SELECT count(*) FROM submissions sb WHERE sb.problem_id = p.id) AS submissions
           FROM problems p ORDER BY p.id LIMIT $1 OFFSET $2""",
        page.limit, page.offset,
    )
    c.paged(items, c.db.count("SELECT count(*) FROM problems"), page)
}

@Serializable
class NameInput(val name: String = "")

private fun checkProblemName(name: String) {
    if (runeCount(name) !in 1..64) throw badRequest("Problem name must be 1 to 64 characters")
}

suspend fun adminCreateProblem(c: Ctx) {
    val name = cleanText(c.bind<NameInput>().name)
    checkProblemName(name)
    val id = c.db.one("INSERT INTO problems (name) VALUES ($1) RETURNING id", name) { it.getInt(1) }!!
    c.audit("manage.problem.create", "Problem created", mapOf("problem_id" to id, "name" to name))
    c.created("id" to id)
}

suspend fun adminGetProblem(c: Ctx) {
    val id = c.pathInt("id")
    val row = c.db.jsonRow(
        """SELECT p.*,
               (SELECT coalesce(jsonb_agg(to_jsonb(t) - 'problem_id' ORDER BY t.name), '[]') FROM testcases t WHERE t.problem_id = p.id) AS testcases,
               (SELECT coalesce(jsonb_agg(to_jsonb(st) - 'problem_id' ORDER BY st.idx), '[]') FROM subtasks st WHERE st.problem_id = p.id) AS subtasks,
               (SELECT coalesce(jsonb_agg(jsonb_build_object('id', ct.id, 'name', ct.name) ORDER BY ct.id), '[]')
                   FROM contest_problems cp JOIN contests ct ON ct.id = cp.contest_id WHERE cp.problem_id = p.id) AS contests
           FROM problems p WHERE p.id = $1""",
        id,
    )
    c.ok(withFields(row, "files" to c.s.listFiles(id)))
}

@Serializable
private data class ProblemPatch(
    val name: String? = null,
    val status: String? = null,
    val allowSubmit: Boolean? = null,
    val tags: List<String>? = null,
    val statement: String? = null,
    val timeLimitMs: Int? = null,
    val memoryLimitKb: Int? = null,
    val checker: String? = null,
    val scorePrecision: Int? = null,
    val languages: List<String>? = null,
)

fun cleanTags(input: List<String>): List<String> {
    val out = input.map(::cleanText).filter { it.isNotEmpty() }.distinct()
    out.firstOrNull { !TAG.matches(it) }?.let { throw badRequest("Invalid tag \"$it\"") }
    if (out.size > 16) throw badRequest("At most 16 tags are allowed")
    return out
}

fun cleanLanguages(input: List<String>): List<String> {
    input.firstOrNull { !validLanguage(it) }?.let { throw badRequest("Unknown language \"$it\"") }
    return input.distinct()
}

suspend fun adminUpdateProblem(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<ProblemPatch>()
    val name = input.name?.let(::cleanText)?.also(::checkProblemName)
    if (input.status != null && input.status !in setOf("online", "contest", "hidden")) throw badRequest("Invalid status")
    if (input.checker != null && input.checker !in setOf("diff", "diff-strict", "float-4", "float-6", "float-9", "testlib")) {
        throw badRequest("Invalid checker")
    }
    input.statement?.let {
        if (it.toByteArray(Charsets.UTF_8).size > MAX_STATEMENT || '\u0000' in it) throw badRequest("Statement is too long or not valid text")
    }
    if (input.timeLimitMs != null && input.timeLimitMs !in 1..600000) throw badRequest("Time limit must be between 1 and 600000 ms")
    if (input.memoryLimitKb != null && input.memoryLimitKb !in 1024..16777216) {
        throw badRequest("Memory limit must be between 1024 and 16777216 KiB")
    }
    if (input.scorePrecision != null && input.scorePrecision !in 0..3) throw badRequest("Score precision must be between 0 and 3")
    val tags = input.tags?.let { TextArr(cleanTags(it)) } // null keeps the stored value
    val languages = input.languages?.let { TextArr(cleanLanguages(it)) }
    val updated = c.db.exec(
        """UPDATE problems SET name = coalesce($2, name), status = coalesce($3, status),
               allow_submit = coalesce($4, allow_submit), tags = coalesce($5, tags), statement = coalesce($6, statement),
               time_limit_ms = coalesce($7, time_limit_ms), memory_limit_kb = coalesce($8, memory_limit_kb),
               checker = coalesce($9, checker), score_precision = coalesce($10, score_precision),
               languages = coalesce($11, languages), updated_at = now() WHERE id = $1""",
        id, name, input.status, input.allowSubmit, tags, input.statement, input.timeLimitMs, input.memoryLimitKb,
        input.checker, input.scorePrecision, languages,
    )
    if (updated == 0) throw notFound()
    // The statement is left out to keep the log small.
    c.audit("manage.problem.update", "Problem updated", mapOf("problem_id" to id, "changes" to json.encodeToJsonElement(input.copy(statement = null))))
    c.noContent()
}

suspend fun adminDeleteProblem(c: Ctx) {
    val id = c.pathInt("id")
    if (c.db.exec("DELETE FROM problems WHERE id = $1", id) == 0) throw notFound()
    if (!c.s.problemDir(id).deleteRecursively()) c.s.log.error("could not remove the directory of problem {}", id)
    c.audit("manage.problem.delete", "Problem deleted", mapOf("problem_id" to id))
    c.noContent()
}

fun requireProblem(c: Ctx, id: Int) {
    problemStatus(c, id) ?: throw notFound()
}

/** Tracks the bytes of one upload request so that it cannot exceed its limit. */
private class Budget(private var remaining: Long) {
    fun take(n: Int) {
        remaining -= n
        if (remaining < 0) throw tooLarge()
    }
}

/** Streams one multipart part to [dst] atomically and returns its size. */
private fun saveUpload(part: PartData.FileItem, dst: File, budget: Budget): Long {
    dst.parentFile.mkdirs()
    val tmp = File.createTempFile(".upload-", null, dst.parentFile)
    try {
        var size = 0L
        part.provider().toInputStream().use { input ->
            tmp.outputStream().use { out ->
                val buf = ByteArray(64 * 1024)
                while (true) {
                    val n = input.read(buf)
                    if (n < 0) break
                    budget.take(n)
                    out.write(buf, 0, n)
                    size += n
                }
            }
        }
        Files.move(tmp.toPath(), dst.toPath(), StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE)
        return size
    } finally {
        tmp.delete()
    }
}

/** Calls [each] with every file of a multipart upload and its client-side base name. */
private suspend fun forEachUpload(c: Ctx, limit: Long, each: (PartData.FileItem, String, Budget) -> Unit): Int {
    if (!c.call.request.contentType().match(ContentType.MultiPart.FormData)) throw badRequest("Expected a multipart upload")
    if ((c.call.request.contentLength() ?: 0) > limit) throw tooLarge()
    val budget = Budget(limit)
    val multipart = c.call.receiveMultipart(formFieldLimit = limit)
    var count = 0
    while (true) {
        val part = multipart.readPart() ?: break
        try {
            if (part is PartData.FileItem && !part.originalFileName.isNullOrEmpty()) {
                each(part, part.originalFileName!!.replace('\\', '/').substringAfterLast('/'), budget)
                count++
            }
        } finally {
            part.dispose()
        }
    }
    if (count == 0) throw badRequest("No files uploaded")
    return count
}

/**
 * Accepts multipart files named <case>.in and <case>.out (or .ans). Files are
 * stored under server-chosen names, so the client-supplied name never reaches
 * the filesystem.
 */
suspend fun adminUploadTestcases(c: Ctx) {
    val id = c.pathInt("id")
    requireProblem(c, id)
    val saved = forEachUpload(c, MAX_TESTDATA_UPLOAD) { part, name, budget ->
        val m = TEST_NAME.matchEntire(name)
            ?: throw badRequest("Invalid testdata file name \"${part.originalFileName}\": use <name>.in and <name>.out")
        val input = m.groupValues[2] == "in"
        val tid = c.db.one(
            """INSERT INTO testcases (problem_id, name) VALUES ($1, $2)
               ON CONFLICT (problem_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id""",
            id, m.groupValues[1],
        ) { it.getInt(1) }!!
        val size = saveUpload(part, c.s.testcaseFile(id, tid, if (input) "in" else "out"), budget)
        c.db.exec("UPDATE testcases SET ${if (input) "input_size" else "output_size"} = $2 WHERE id = $1", tid, size)
    }
    c.audit("manage.problem.testdata", "Testdata uploaded", mapOf("problem_id" to id, "files" to saved))
    c.ok("files" to saved)
}

suspend fun adminDeleteTestcase(c: Ctx) {
    val id = c.pathInt("id")
    val tid = c.pathInt("tid")
    c.db.tx { t ->
        if (t.exec("DELETE FROM testcases WHERE id = $1 AND problem_id = $2", tid, id) == 0) throw notFound()
        t.exec("UPDATE subtasks SET testcase_ids = array_remove(testcase_ids, $2) WHERE problem_id = $1", id, tid)
    }
    c.s.testcaseFile(id, tid, "in").delete()
    c.s.testcaseFile(id, tid, "out").delete()
    c.audit("manage.problem.testdata", "Testcase deleted", mapOf("problem_id" to id, "testcase_id" to tid))
    c.noContent()
}

suspend fun adminDownloadTestcase(c: Ctx) {
    val id = c.pathInt("id")
    val tid = c.pathInt("tid")
    val kind = c.call.parameters["kind"]
    if (kind != "in" && kind != "out") throw notFound()
    val name = c.db.one("SELECT name FROM testcases WHERE id = $1 AND problem_id = $2", tid, id) { it.getString(1) } ?: throw notFound()
    serveFile(c, c.s.testcaseFile(id, tid, kind), "$name.$kind")
}

@Serializable
private class SubtaskInput(val score: Double = 0.0, val testcaseIds: List<Int> = emptyList(), val depends: List<Int> = emptyList())

/**
 * Replaces the whole subtask list; subtasks are numbered from 1 in the order
 * given and may only depend on earlier ones.
 */
suspend fun adminSetSubtasks(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<List<SubtaskInput>>()
    if (input.size > 100) throw badRequest("At most 100 subtasks are allowed")
    val owned = c.db.query("SELECT id FROM testcases WHERE problem_id = $1", id) { it.getInt(1) }.toSet()
    val clean = input.mapIndexed { i, st ->
        if (st.score.isNaN() || st.score < 0 || st.score > 1e6) throw badRequest("Subtask ${i + 1}: invalid score")
        val cases = st.testcaseIds.distinct().sorted()
        val depends = st.depends.distinct().sorted()
        cases.firstOrNull { it !in owned }?.let { throw badRequest("Subtask ${i + 1}: testcase $it does not belong to this problem") }
        if (depends.any { it < 1 || it > i }) throw badRequest("Subtask ${i + 1} may only depend on earlier subtasks")
        Subtask(i + 1, st.score, cases, depends)
    }
    c.db.tx { t ->
        t.one("SELECT true FROM problems WHERE id = $1 FOR UPDATE", id) { true } ?: throw notFound()
        t.exec("DELETE FROM subtasks WHERE problem_id = $1", id)
        for (st in clean) {
            t.exec(
                "INSERT INTO subtasks (problem_id, idx, score, testcase_ids, depends) VALUES ($1, $2, $3, $4, $5)",
                id, st.idx, st.score, IntArr(st.testcaseIds), IntArr(st.depends),
            )
        }
    }
    c.audit("manage.problem.subtasks", "Subtasks updated", mapOf("problem_id" to id, "subtasks" to json.encodeToJsonElement(clean)))
    c.noContent()
}

suspend fun adminUploadFiles(c: Ctx) {
    val id = c.pathInt("id")
    requireProblem(c, id)
    val names = ArrayList<String>()
    forEachUpload(c, MAX_FILE_UPLOAD) { part, name, budget ->
        if (!FILE_NAME.matches(name)) {
            throw badRequest("Invalid file name \"${part.originalFileName}\": use letters, digits, '.', '_' and '-'")
        }
        saveUpload(part, File(c.s.problemDir(id, "files"), name), budget)
        names += name
    }
    c.audit("manage.problem.files", "Attachments uploaded", mapOf("problem_id" to id, "files" to names))
    c.ok("files" to names)
}

suspend fun adminDeleteFile(c: Ctx) {
    val id = c.pathInt("id")
    val name = c.call.parameters["name"].orEmpty()
    if (!FILE_NAME.matches(name) || !File(c.s.problemDir(id, "files"), name).delete()) throw notFound()
    c.audit("manage.problem.files", "Attachment deleted", mapOf("problem_id" to id, "file" to name))
    c.noContent()
}
