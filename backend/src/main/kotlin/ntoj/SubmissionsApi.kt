package ntoj

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.encodeToJsonElement

const val MAX_CODE_SIZE = 64 shl 10

/** The SET clause that sends a submission back to the queue. */
const val RESET_SUBMISSION = """state = 101, score = 0, time_ms = 0, memory_kb = 0, message = '',
    result = '{"subtasks": [], "testcases": []}', claimed_at = NULL, judged_at = NULL"""

@Serializable
private class SubmitInput(val problemId: Int = 0, val contestId: Int? = null, val language: String = "", val code: String = "")

suspend fun submit(c: Ctx) {
    val acct = c.acct!!
    val input = c.bind<SubmitInput>()
    if (!validLanguage(input.language)) throw badRequest("Unknown language")
    if (input.code.isBlank()) throw badRequest("Code must not be empty")
    if (input.code.toByteArray(Charsets.UTF_8).size > MAX_CODE_SIZE || '\u0000' in input.code) {
        throw badRequest("Code must be valid UTF-8 text of at most ${MAX_CODE_SIZE shr 10} KiB")
    }

    class Problem(val status: String, val allowSubmit: Boolean, val languages: List<String>)

    val problem = c.db.one("SELECT status, allow_submit, languages FROM problems WHERE id = $1", input.problemId) {
        Problem(it.getString(1), it.getBoolean(2), it.strings(3))
    } ?: throw notFound()

    var cooldown = c.s.cfg.submitCooldown
    var priority = 0
    if (input.contestId != null) {
        val (contest, access) = loadAccess(c, input.contestId)
        val inContest = c.db.one(
            "SELECT 1 FROM contest_problems WHERE contest_id = $1 AND problem_id = $2", contest.id, input.problemId,
        ) { true } != null
        if (!inContest || (problem.status == "hidden" && !access.isAdmin)) throw notFound()
        if (!access.canSubmit) throw ApiError(403, "contest_closed", "You cannot submit to this contest right now")
        if (contest.languages.isNotEmpty() && input.language !in contest.languages) {
            throw badRequest("This language is not allowed in the contest")
        }
        cooldown = if (access.isAdmin) 0 else contest.cooldownSec
        priority = 1
    } else {
        if (problem.status != "online" && !c.admin) throw notFound()
        checkLockdown(c)
    }
    if (!problem.allowSubmit && !c.admin) throw ApiError(403, "submit_closed", "This problem does not accept submissions")
    if (problem.languages.isNotEmpty() && input.language !in problem.languages) {
        throw badRequest("This language is not allowed for the problem")
    }

    val id = c.db.tx { t ->
        // Serialise one account's submissions so the cooldown cannot be raced.
        t.query("SELECT pg_advisory_xact_lock(1, $1)", acct.id) { }
        if (cooldown > 0) {
            val wait = t.one(
                """SELECT coalesce($3 - extract(epoch FROM now() - max(created_at)), 0)::float8
                   FROM submissions WHERE account_id = $1 AND contest_id IS NOT DISTINCT FROM $2""",
                acct.id, input.contestId, cooldown,
            ) { it.getDouble(1) } ?: 0.0
            if (wait > 0) {
                val seconds = wait.toInt() + 1
                throw ApiError(429, "cooldown", "Please wait $seconds more second(s) before submitting again", mapOf("retry_after" to seconds))
            }
        }
        val newId = t.one(
            "INSERT INTO submissions (problem_id, account_id, contest_id, language, code, priority) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
            input.problemId, acct.id, input.contestId, input.language, input.code, priority,
        ) { it.getLong(1) }!!
        t.exec("UPDATE accounts SET last_language = $2 WHERE id = $1", acct.id, input.language)
        newId
    }
    c.created("id" to id)
}

suspend fun listSubmissions(c: Ctx) {
    val page = c.page(30)
    val args = ArrayList<Any?>()
    fun arg(v: Any?): String {
        args += v
        return "$" + args.size
    }
    var where = "true"

    val contestId = c.queryInt("contest_id")
    if (contestId != null) {
        val (_, access) = loadAccess(c, contestId)
        where += " AND s.contest_id = " + arg(contestId)
        when (access.submissions) {
            Scope.NONE -> where += " AND false"
            Scope.OWN -> where += " AND s.account_id = " + arg(c.acctId)
            Scope.PARTICIPANTS -> where += " AND (s.account_id = ${arg(c.acctId)} OR s.account_id IN (SELECT account_id " +
                "FROM contest_users WHERE contest_id = ${arg(contestId)} AND status = 'approved'))"
        }
    } else {
        checkLockdown(c)
        where += " AND s.contest_id IS NULL"
        if (!c.admin) where += " AND p.status = 'online'"
    }
    for ((param, column) in listOf("problem_id" to "s.problem_id", "account_id" to "s.account_id", "state" to "s.state")) {
        val v = c.queryInt(param)
        if (v != null && v != 0) where += " AND $column = " + arg(v)
    }
    c.query("language").takeIf { it.isNotEmpty() }?.let { where += " AND s.language = " + arg(it) }

    val from = " FROM submissions s JOIN problems p ON p.id = s.problem_id JOIN accounts a ON a.id = s.account_id WHERE "
    val total = c.db.count("SELECT count(*)$from$where", *args.toTypedArray())
    val items = c.db.jsonList(
        """SELECT s.id, s.problem_id, p.name AS problem_name, s.account_id, a.name AS account_name,
               s.contest_id, s.language, s.created_at, s.state, s.score, s.time_ms, s.memory_kb, length(s.code) AS code_length""" +
            from + where + " ORDER BY s.id DESC LIMIT ${arg(page.limit)} OFFSET ${arg(page.offset)}",
        *args.toTypedArray(),
    )
    c.paged(items, total, page)
}

/** Null lists are details the viewer is not allowed to see. */
@Serializable
data class SubmissionResult(val subtasks: List<SubtaskResult>? = emptyList(), val testcases: List<CaseResult>? = emptyList())

@Serializable
private data class SubmissionView(
    val id: Long,
    val problemId: Int,
    val problemName: String,
    val accountId: Int,
    val accountName: String,
    val contestId: Int?,
    val language: String,
    val createdAt: Time,
    val judgedAt: Time?,
    val state: Int,
    val score: Double,
    val timeMs: Long,
    val memoryKb: Long,
    val message: String,
    /** Null when the viewer may not read it. */
    val code: String?,
    val result: SubmissionResult,
    val resultStyle: String,
    val canRejudge: Boolean,
)

suspend fun getSubmission(c: Ctx) {
    val id = c.pathInt("id")
    var status = ""
    var code = ""
    var v = c.db.one(
        """SELECT s.id, s.problem_id, p.name, p.status, s.account_id, a.name, s.contest_id, s.language,
               s.created_at, s.judged_at, s.state, s.score::float8, s.time_ms, s.memory_kb, s.message, s.code, s.result::text
           FROM submissions s JOIN problems p ON p.id = s.problem_id JOIN accounts a ON a.id = s.account_id
           WHERE s.id = $1""",
        id,
    ) { r ->
        status = r.getString(4)
        code = r.getString(16)
        SubmissionView(
            id = r.getLong(1), problemId = r.getInt(2), problemName = r.getString(3), accountId = r.getInt(5),
            accountName = r.getString(6), contestId = r.intOrNull(7), language = r.getString(8), createdAt = r.instant(9)!!,
            judgedAt = r.instant(10), state = r.getInt(11), score = r.getDouble(12), timeMs = r.getLong(13),
            memoryKb = r.getLong(14), message = r.getString(15), code = null,
            result = dbJson.decodeFromString(r.getString(17)), resultStyle = "full", canRejudge = false,
        )
    } ?: throw notFound()

    val owner = c.acctId == v.accountId
    var privileged = c.admin
    var style = "full"
    if (v.contestId != null) {
        val (_, access) = loadAccess(c, v.contestId!!)
        val ownerIsAdmin = c.db.one(
            "SELECT 1 FROM contest_users WHERE contest_id = $1 AND account_id = $2 AND status = 'admin'", v.contestId, v.accountId,
        ) { true } != null
        if (!access.canViewSubmission(v.accountId, ownerIsAdmin)) throw notFound()
        privileged = access.isAdmin
        if (access.restrictResults) {
            style = c.db.one(
                "SELECT result_style FROM contest_problems WHERE contest_id = $1 AND problem_id = $2", v.contestId, v.problemId,
            ) { it.getString(1) } ?: "full"
        }
    } else {
        if (status != "online" && !privileged && !owner) throw notFound()
        if (!owner) checkLockdown(c)
    }

    var result = when (style) {
        "total" -> SubmissionResult(null, null)
        "subtask" -> v.result.copy(testcases = null)
        else -> v.result
    }
    var message = v.message
    if (!privileged) {
        // Checker output can reveal test data; compiler output only goes to the author.
        result = result.copy(testcases = result.testcases?.map { it.copy(message = "") })
        if (!owner) message = ""
    }
    val showCode = owner || privileged
    if (showCode && !owner) {
        c.audit("manage.submission.view", "Viewed another account's source code", mapOf("submission_id" to v.id, "owner_id" to v.accountId), v.contestId)
    }
    v = v.copy(result = result, message = message, resultStyle = style, canRejudge = privileged, code = if (showCode) code else null)
    c.ok(json.encodeToJsonElement(v))
}

suspend fun rejudgeSubmission(c: Ctx) {
    val id = c.pathInt("id")
    val contestId = c.db.one("SELECT coalesce(contest_id, 0) FROM submissions WHERE id = $1", id) { it.getInt(1) } ?: throw notFound()
    val allowed = c.admin || (contestId != 0 && loadAccess(c, contestId).second.isAdmin)
    if (!allowed) throw notFound()
    c.db.exec("UPDATE submissions SET $RESET_SUBMISSION, priority = 2 WHERE id = $1", id)
    c.audit("manage.submission.rejudge", "Submission rejudged", mapOf("submission_id" to id), contestId.takeIf { it != 0 })
    c.noContent()
}

suspend fun adminRejudgeProblem(c: Ctx) {
    val id = c.pathInt("id")
    requireProblem(c, id)
    val count = c.db.exec("UPDATE submissions SET $RESET_SUBMISSION, priority = -1 WHERE problem_id = $1", id)
    c.audit("manage.problem.rejudge", "Problem rejudged", mapOf("problem_id" to id, "submissions" to count))
    c.ok("submissions" to count)
}
