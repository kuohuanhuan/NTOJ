package ntoj

import java.time.Duration
import java.time.temporal.ChronoUnit
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

fun loadContest(c: Ctx, id: Int): Contest = dbJson.decodeFromString(c.db.jsonRow("SELECT * FROM contests WHERE id = $1", id))

/** Resolves the caller's relation to a contest. */
fun loadAccess(c: Ctx, id: Int): Pair<Contest, Access> {
    val contest = loadContest(c, id)
    val member = c.acct?.let { acct ->
        c.db.one("SELECT status, session_start, session_end FROM contest_users WHERE contest_id = $1 AND account_id = $2", id, acct.id) {
            Member(it.getString(1), it.instant(2), it.instant(3))
        }
    }
    return contest to Access.resolve(contest, c.acctId, member, c.admin, c.s.now())
}

/** Loads the contest in the path and requires the caller to administer it. */
private fun contestAdmin(c: Ctx): Contest {
    val (contest, access) = loadAccess(c, c.pathInt("id"))
    if (!access.isAdmin) throw forbidden()
    return contest
}

/** The contest without its phase descriptions, which are served one at a time. */
private fun publicContest(contest: Contest): JsonObject =
    JsonObject(json.encodeToJsonElement(contest).jsonObject.filterKeys { !it.startsWith("desc_") })

suspend fun listContests(c: Ctx) = c.okRaw(
    c.db.jsonList(
        """SELECT c.id, c.name, c.mode, c.start_at, c.end_at, c.time_mode, c.duration_sec,
               c.reg_mode, c.reg_end_at, c.public_scoreboard, cu.status AS my_status,
               (SELECT count(*) FROM contest_users u WHERE u.contest_id = c.id AND u.status = 'approved') AS participants
           FROM contests c LEFT JOIN contest_users cu ON cu.contest_id = c.id AND cu.account_id = $1
           ORDER BY c.start_at DESC, c.id DESC""",
        c.acctId,
    ),
)

suspend fun createContest(c: Ctx) {
    val name = cleanText(c.bind<NameInput>().name)
    if (runeCount(name) !in 1..64) throw badRequest("Contest name must be 1 to 64 characters")
    val start = c.s.now().plus(Duration.ofHours(24)).truncatedTo(ChronoUnit.HOURS)
    val id = c.db.tx { t ->
        val id = t.one(
            "INSERT INTO contests (name, creator_id, start_at, end_at, reg_end_at) VALUES ($1, $2, $3, $4, $3) RETURNING id",
            name, c.acctId, start, start.plus(Duration.ofHours(3)),
        ) { it.getInt(1) }!!
        t.exec("INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, 'admin')", id, c.acctId)
        id
    }
    c.audit("contest.create", "Contest created", mapOf("name" to name), id)
    c.created("id" to id)
}

suspend fun getContest(c: Ctx) {
    val id = c.pathInt("id")
    val (contest, access) = loadAccess(c, id)
    val description = when {
        !access.activated && access.global != Phase.ENDED -> contest.descBefore
        access.phase == Phase.RUNNING -> contest.descDuring
        access.phase == Phase.ENDED || access.global == Phase.ENDED -> contest.descAfter
        else -> contest.descBefore
    }
    val participants = c.db.count("SELECT count(*) FROM contest_users WHERE contest_id = $1 AND status = 'approved'", id)
    val unread = when {
        access.isAdmin -> c.db.count("SELECT count(*) FROM contest_questions WHERE contest_id = $1 AND reply IS NULL", id)
        access.isMember -> c.db.count(
            """SELECT (SELECT count(*) FROM contest_announcements n WHERE n.contest_id = cu.contest_id AND n.updated_at > cu.read_at) +
                   (SELECT count(*) FROM contest_questions q WHERE q.contest_id = cu.contest_id AND q.asker_id = cu.account_id AND q.replied_at > cu.read_at)
               FROM contest_users cu WHERE cu.contest_id = $1 AND cu.account_id = $2""",
            id, c.acctId,
        )
        else -> 0
    }
    c.ok(
        "contest" to publicContest(contest), "description" to description, "access" to json.encodeToJsonElement(access),
        "participants" to participants, "unread" to unread, "now" to c.s.now(),
    )
}

@Serializable
private data class ContestPatch(
    val name: String? = null,
    val descBefore: String? = null,
    val descDuring: String? = null,
    val descAfter: String? = null,
    val mode: String? = null,
    val startAt: Time? = null,
    val endAt: Time? = null,
    val timeMode: String? = null,
    val durationSec: Int? = null,
    val regMode: String? = null,
    val regEndAt: Time? = null,
    val languages: List<String>? = null,
    val publicScoreboard: Boolean? = null,
    val allowOtherPages: Boolean? = null,
    val hideAdmin: Boolean? = null,
    val cooldownSec: Int? = null,
    val freezeMinutes: Int? = null,
    val keepFrozen: Boolean? = null,
    val penaltyMinutes: Int? = null,
)

suspend fun updateContest(c: Ctx) {
    val current = contestAdmin(c)
    val p = c.bind<ContestPatch>()
    val row = current.copy(
        name = cleanText(p.name ?: current.name),
        descBefore = p.descBefore ?: current.descBefore,
        descDuring = p.descDuring ?: current.descDuring,
        descAfter = p.descAfter ?: current.descAfter,
        mode = p.mode ?: current.mode,
        startAt = p.startAt ?: current.startAt,
        endAt = p.endAt ?: current.endAt,
        timeMode = p.timeMode ?: current.timeMode,
        durationSec = p.durationSec ?: current.durationSec,
        regMode = p.regMode ?: current.regMode,
        regEndAt = p.regEndAt ?: current.regEndAt,
        languages = p.languages?.let(::cleanLanguages) ?: current.languages,
        publicScoreboard = p.publicScoreboard ?: current.publicScoreboard,
        allowOtherPages = p.allowOtherPages ?: current.allowOtherPages,
        hideAdmin = p.hideAdmin ?: current.hideAdmin,
        cooldownSec = p.cooldownSec ?: current.cooldownSec,
        freezeMinutes = p.freezeMinutes ?: current.freezeMinutes,
        keepFrozen = p.keepFrozen ?: current.keepFrozen,
        penaltyMinutes = p.penaltyMinutes ?: current.penaltyMinutes,
    )
    val window = Duration.between(row.startAt, row.endAt).seconds
    fun tooLong(s: String) = s.toByteArray(Charsets.UTF_8).size > MAX_STATEMENT
    when {
        runeCount(row.name) !in 1..64 -> throw badRequest("Contest name must be 1 to 64 characters")
        tooLong(row.descBefore) || tooLong(row.descDuring) || tooLong(row.descAfter) -> throw badRequest("Description is too long")
        row.mode !in setOf("ioi", "acm") -> throw badRequest("Invalid contest mode")
        row.timeMode !in setOf("fixed", "flexible") -> throw badRequest("Invalid time mode")
        row.regMode !in setOf("invited", "free", "approval") -> throw badRequest("Invalid registration mode")
        !row.endAt.isAfter(row.startAt) -> throw badRequest("The contest must end after it starts")
        row.timeMode == "flexible" && (row.durationSec < 60 || row.durationSec > window) ->
            throw badRequest("A flexible contest needs a duration between one minute and the length of its window")
        row.cooldownSec !in 0..86400 -> throw badRequest("Invalid submission cooldown")
        row.freezeMinutes < 0 || row.freezeMinutes > window / 60 -> throw badRequest("The freeze period cannot be longer than the contest")
        row.penaltyMinutes !in 0..10000 -> throw badRequest("Invalid penalty")
    }
    c.db.exec(
        """UPDATE contests SET name = $2, desc_before = $3, desc_during = $4, desc_after = $5, mode = $6,
               start_at = $7, end_at = $8, time_mode = $9, duration_sec = $10, reg_mode = $11, reg_end_at = $12, languages = $13,
               public_scoreboard = $14, allow_other_pages = $15, hide_admin = $16, cooldown_sec = $17, freeze_minutes = $18,
               keep_frozen = $19, penalty_minutes = $20 WHERE id = $1""",
        row.id, row.name, row.descBefore, row.descDuring, row.descAfter, row.mode, row.startAt, row.endAt, row.timeMode,
        if (row.timeMode == "fixed") 0 else row.durationSec, row.regMode, row.regEndAt, TextArr(row.languages),
        row.publicScoreboard, row.allowOtherPages, row.hideAdmin, row.cooldownSec, row.freezeMinutes, row.keepFrozen, row.penaltyMinutes,
    )
    val logged = p.copy(descBefore = null, descDuring = null, descAfter = null)
    c.audit("contest.update", "Contest settings updated", mapOf("changes" to json.encodeToJsonElement(logged)), row.id)
    c.noContent()
}

suspend fun deleteContest(c: Ctx) {
    val id = c.pathInt("id")
    val name = c.db.one("DELETE FROM contests WHERE id = $1 RETURNING name", id) { it.getString(1) } ?: throw notFound()
    c.audit("contest.delete", "Contest deleted", mapOf("contest_id" to id, "name" to name))
    c.noContent()
}

suspend fun registerContest(c: Ctx) {
    val id = c.pathInt("id")
    val (contest, access) = loadAccess(c, id)
    if (!access.canRegister) throw ApiError(403, "registration_closed", "Registration is not open to you")
    val status = if (contest.regMode == "free") "approved" else "requested"
    c.db.exec("INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", id, c.acctId, status)
    c.audit("contest.register", "Registered for contest", mapOf("status" to status), id)
    c.ok("status" to status)
}

suspend fun withdrawContest(c: Ctx) {
    val id = c.pathInt("id")
    if (!loadAccess(c, id).second.canWithdraw) throw ApiError(403, "registration_closed", "You can no longer withdraw from this contest")
    c.db.exec("DELETE FROM contest_users WHERE contest_id = $1 AND account_id = $2 AND status IN ('requested', 'approved')", id, c.acctId)
    c.audit("contest.withdraw", "Withdrew from contest", contestId = id)
    c.noContent()
}

/**
 * Opens the caller's personal window in a flexible contest. The single UPDATE
 * makes it atomic and idempotent.
 */
suspend fun startContestSession(c: Ctx) {
    val id = c.pathInt("id")
    if (!loadAccess(c, id).second.canStart) throw ApiError(403, "cannot_start", "You cannot start this contest now")
    val (start, end) = c.db.one(
        """UPDATE contest_users cu SET session_start = now(),
               session_end = least(now() + make_interval(secs => ct.duration_sec), ct.end_at)
           FROM contests ct
           WHERE ct.id = cu.contest_id AND cu.contest_id = $1 AND cu.account_id = $2 AND cu.status = 'approved'
             AND cu.session_start IS NULL AND ct.time_mode = 'flexible' AND now() >= ct.start_at AND now() < ct.end_at
           RETURNING cu.session_start, cu.session_end""",
        id, c.acctId,
    ) { it.instant(1)!! to it.instant(2)!! } ?: throw conflict("Your contest session has already started")
    c.audit("contest.start", "Started flexible contest session", contestId = id)
    c.ok("start" to start, "end" to end)
}

suspend fun listContestProblems(c: Ctx) {
    val id = c.pathInt("id")
    val access = loadAccess(c, id).second
    if (!access.canViewProblems) throw forbidden()
    c.okRaw(
        c.db.jsonList(
            """SELECT p.id, p.name, p.status, p.allow_submit, cp.score_type, cp.result_style,
                   my.state AS my_state, my.score AS my_score
               FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
               LEFT JOIN LATERAL (SELECT min(state) AS state, max(score) AS score FROM submissions
                   WHERE contest_id = $1 AND problem_id = p.id AND account_id = $2 AND state < 100) my ON true
               WHERE cp.contest_id = $1 AND ($3 OR p.status <> 'hidden') ORDER BY cp.ord""",
            id, c.acctId, access.isAdmin,
        ),
    )
}

suspend fun getContestProblem(c: Ctx) {
    val id = c.pathInt("id")
    val pid = c.pathInt("pid")
    val (contest, access) = loadAccess(c, id)
    if (!access.canViewProblems) throw forbidden()
    val style = c.db.one("SELECT result_style FROM contest_problems WHERE contest_id = $1 AND problem_id = $2", id, pid) { it.getString(1) }
        ?: throw notFound()
    val problem = problemJson(c, pid)
    if (problem.getValue("status").jsonPrimitive.content == "hidden" && !access.isAdmin) throw notFound()
    val problemLanguages = problem.getValue("languages").jsonArray.map { it.jsonPrimitive.content }
    val languages = if (contest.languages.isEmpty()) {
        problemLanguages
    } else {
        contest.languages.filter { problemLanguages.isEmpty() || it in problemLanguages }
    }
    c.ok(JsonObject(problem + obj("languages" to languages, "result_style" to style, "can_submit" to access.canSubmit)))
}

suspend fun contestScoreboard(c: Ctx) {
    val id = c.pathInt("id")
    val (contest, access) = loadAccess(c, id)
    if (!access.canViewScoreboard) throw forbidden()
    val problems = c.db.query(
        """SELECT p.id, p.name, cp.score_type FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
           WHERE cp.contest_id = $1 AND p.status <> 'hidden' ORDER BY cp.ord""",
        id,
    ) { BoardProblem(it.getInt(1), it.getString(2), it.getString(3)) }
    // Contestants compete in their own window; administrators are listed only
    // when the contest does not hide them.
    val users = c.db.query(
        """SELECT a.id, a.name,
               CASE WHEN ct.time_mode = 'flexible' AND cu.status = 'approved' THEN cu.session_start ELSE ct.start_at END,
               CASE WHEN ct.time_mode = 'flexible' AND cu.status = 'approved' THEN cu.session_end ELSE ct.end_at END
           FROM contest_users cu JOIN accounts a ON a.id = cu.account_id JOIN contests ct ON ct.id = cu.contest_id
           WHERE cu.contest_id = $1 AND (cu.status = 'approved' OR (cu.status = 'admin' AND NOT ct.hide_admin))
             AND (ct.time_mode = 'fixed' OR cu.status = 'admin' OR cu.session_start IS NOT NULL)
           ORDER BY a.id""",
        id,
    ) { BoardUser(it.getInt(1), it.getString(2), it.instant(3)!!, it.instant(4)!!) }
    val subs = c.db.query(
        "SELECT id, account_id, problem_id, created_at, state, score::float8, result->>'subtasks' FROM submissions WHERE contest_id = $1 ORDER BY id",
        id,
    ) {
        BoardSubmission(
            it.getLong(1), it.getInt(2), it.getInt(3), it.instant(4)!!, it.getInt(5), it.getDouble(6),
            it.getString(7)?.let { text -> dbJson.decodeFromString<List<SubtaskResult>?>(text) } ?: emptyList(),
        )
    }
    val frozen = contest.freezeMinutes > 0 && !access.isAdmin && (c.s.now().isBefore(contest.endAt) || contest.keepFrozen)
    val rows = Scoreboard.compute(contest.mode, contest.penaltyMinutes, if (frozen) contest.freezeMinutes else 0, problems, users, subs)
    c.ok(
        "mode" to contest.mode, "problems" to json.encodeToJsonElement(problems), "frozen" to frozen,
        "rows" to json.encodeToJsonElement(rows),
    )
}

// ---- Announcements and questions ----

suspend fun contestQA(c: Ctx) {
    val id = c.pathInt("id")
    val access = loadAccess(c, id).second
    val announcements = c.db.jsonList(
        "SELECT n.id, n.subject, n.content, n.created_at, n.updated_at FROM contest_announcements n WHERE n.contest_id = $1 ORDER BY n.id DESC", id,
    )
    val questions = c.db.jsonList(
        """SELECT q.id, q.asker_id, a.name AS asker_name, q.subject, q.content, q.reply, q.created_at, q.replied_at
           FROM contest_questions q JOIN accounts a ON a.id = q.asker_id
           WHERE q.contest_id = $1 AND ($3 OR q.asker_id = $2) ORDER BY q.id DESC""",
        id, c.acctId, access.isAdmin,
    )
    if (c.acct != null) c.db.exec("UPDATE contest_users SET read_at = now() WHERE contest_id = $1 AND account_id = $2", id, c.acctId)
    c.ok("announcements" to raw(announcements), "questions" to raw(questions), "can_ask" to access.canAsk, "is_admin" to access.isAdmin)
}

@Serializable
private class PostInput(val subject: String = "", val content: String = "") {
    fun clean(): Pair<String, String> {
        val s = cleanText(subject)
        val body = cleanText(content)
        if (runeCount(s) !in 1..100) throw badRequest("Subject must be 1 to 100 characters")
        if (runeCount(body) !in 1..4000) throw badRequest("Content must be 1 to 4000 characters")
        return s to body
    }
}

@Serializable
class ReplyInput(val reply: String = "") {
    fun clean(): String = cleanText(reply).also {
        if (runeCount(it) !in 1..4000) throw badRequest("Reply must be 1 to 4000 characters")
    }
}

suspend fun askContestQuestion(c: Ctx) {
    val id = c.pathInt("id")
    if (!loadAccess(c, id).second.canAsk) throw forbidden()
    val (subject, content) = c.bind<PostInput>().clean()
    if (!c.s.limiter.allow("contest-question:$id:${c.ip}", 10, 600)) throw tooMany()
    c.db.exec("INSERT INTO contest_questions (contest_id, asker_id, subject, content) VALUES ($1, $2, $3, $4)", id, c.acctId, subject, content)
    c.created()
}

suspend fun replyContestQuestion(c: Ctx) {
    val contest = contestAdmin(c)
    val qid = c.pathInt("qid")
    val reply = c.bind<ReplyInput>().clean()
    val updated = c.db.exec(
        "UPDATE contest_questions SET reply = $3, replier_id = $4, replied_at = now() WHERE id = $1 AND contest_id = $2",
        qid, contest.id, reply, c.acctId,
    )
    if (updated == 0) throw notFound()
    c.noContent()
}

suspend fun createAnnouncement(c: Ctx) {
    val contest = contestAdmin(c)
    val (subject, content) = c.bind<PostInput>().clean()
    val nid = c.db.one(
        "INSERT INTO contest_announcements (contest_id, author_id, subject, content) VALUES ($1, $2, $3, $4) RETURNING id",
        contest.id, c.acctId, subject, content,
    ) { it.getInt(1) }!!
    c.audit("contest.announce", "Announcement published", mapOf("subject" to subject), contest.id)
    c.created("id" to nid)
}

suspend fun updateAnnouncement(c: Ctx) {
    val contest = contestAdmin(c)
    val nid = c.pathInt("nid")
    val (subject, content) = c.bind<PostInput>().clean()
    val updated = c.db.exec(
        "UPDATE contest_announcements SET subject = $3, content = $4, updated_at = now() WHERE id = $1 AND contest_id = $2",
        nid, contest.id, subject, content,
    )
    if (updated == 0) throw notFound()
    c.noContent()
}

suspend fun deleteAnnouncement(c: Ctx) {
    val contest = contestAdmin(c)
    val nid = c.pathInt("nid")
    if (c.db.exec("DELETE FROM contest_announcements WHERE id = $1 AND contest_id = $2", nid, contest.id) == 0) throw notFound()
    c.noContent()
}

// ---- Management ----

private fun canManageAdmins(c: Ctx, contest: Contest) = c.admin || contest.creatorId == c.acctId

suspend fun manageContest(c: Ctx) {
    val contest = contestAdmin(c)
    val problems = c.db.jsonList(
        """SELECT p.id AS problem_id, p.name, p.status, cp.score_type, cp.result_style
           FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id WHERE cp.contest_id = $1 ORDER BY cp.ord""",
        contest.id,
    )
    val users = c.db.jsonList(
        """SELECT cu.account_id, a.name, cu.status, cu.session_start, cu.session_end, cu.created_at
           FROM contest_users cu JOIN accounts a ON a.id = cu.account_id WHERE cu.contest_id = $1 ORDER BY cu.account_id""",
        contest.id,
    )
    c.ok(
        "contest" to json.encodeToJsonElement(contest), "problems" to raw(problems), "users" to raw(users),
        "can_manage_admins" to canManageAdmins(c, contest),
    )
}

@Serializable
private class ContestProblemInput(val problemId: Int = 0, val scoreType: String = "", val resultStyle: String = "")

suspend fun setContestProblems(c: Ctx) {
    val contest = contestAdmin(c)
    val input = c.bind<List<ContestProblemInput>>()
    if (input.size > 100) throw badRequest("At most 100 problems are allowed")
    val seen = HashSet<Int>()
    for (p in input) {
        if (!seen.add(p.problemId)) throw badRequest("Problem ${p.problemId} is listed twice")
        if (p.scoreType !in setOf("best", "subtask_union") || p.resultStyle !in setOf("full", "subtask", "total")) {
            throw badRequest("Problem ${p.problemId}: invalid options")
        }
    }
    c.db.tx { t ->
        for (p in input) {
            // Contest managers who are not site administrators may only pull
            // in public problems, so they cannot read hidden statements.
            val allowed = t.one(
                """SELECT $3 OR p.status = 'online' OR EXISTS (
                       SELECT 1 FROM contest_problems cp WHERE cp.contest_id = $2 AND cp.problem_id = p.id)
                   FROM problems p WHERE p.id = $1""",
                p.problemId, contest.id, c.admin,
            ) { it.getBoolean(1) }
            if (allowed != true) throw badRequest("Problem ${p.problemId} does not exist or cannot be added")
        }
        t.exec("DELETE FROM contest_problems WHERE contest_id = $1 AND problem_id <> ALL ($2)", contest.id, IntArr(input.map { it.problemId }))
        input.forEachIndexed { i, p ->
            t.exec(
                """INSERT INTO contest_problems (contest_id, problem_id, ord, score_type, result_style)
                   VALUES ($1, $2, $3, $4, $5) ON CONFLICT (contest_id, problem_id)
                   DO UPDATE SET ord = EXCLUDED.ord, score_type = EXCLUDED.score_type, result_style = EXCLUDED.result_style""",
                contest.id, p.problemId, i, p.scoreType, p.resultStyle,
            )
        }
    }
    c.audit("contest.problems", "Contest problems updated", mapOf("problems" to json.encodeToJsonElement(input)), contest.id)
    c.noContent()
}

/**
 * Blocks ordinary contest managers from creating or touching other managers;
 * only the creator or a site administrator may.
 */
private fun guardAdminRow(c: Ctx, contest: Contest, accountId: Int, newStatus: String) {
    if (canManageAdmins(c, contest)) return
    val current = c.db.one("SELECT status FROM contest_users WHERE contest_id = $1 AND account_id = $2", contest.id, accountId) { it.getString(1) }
    if (current == "admin" || newStatus == "admin" || contest.creatorId == accountId) throw forbidden()
}

@Serializable
private class StatusInput(val status: String = "")

suspend fun setContestUser(c: Ctx) {
    val contest = contestAdmin(c)
    val aid = c.pathInt("aid")
    val status = c.bind<StatusInput>().status
    if (status !in setOf("approved", "rejected", "admin")) throw badRequest("Invalid status")
    guardAdminRow(c, contest, aid, status)
    try {
        c.db.exec(
            """INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, $3)
               ON CONFLICT (contest_id, account_id) DO UPDATE SET status = EXCLUDED.status""",
            contest.id, aid, status,
        )
    } catch (e: java.sql.SQLException) {
        if (e.isForeignKeyViolation()) throw badRequest("Account $aid does not exist")
        throw e
    }
    c.audit("contest.user", "Contest member updated", mapOf("account_id" to aid, "status" to status), contest.id)
    c.noContent()
}

suspend fun removeContestUser(c: Ctx) {
    val contest = contestAdmin(c)
    val aid = c.pathInt("aid")
    guardAdminRow(c, contest, aid, "")
    if (c.db.exec("DELETE FROM contest_users WHERE contest_id = $1 AND account_id = $2", contest.id, aid) == 0) throw notFound()
    c.audit("contest.user", "Contest member removed", mapOf("account_id" to aid), contest.id)
    c.noContent()
}

suspend fun resetContestSession(c: Ctx) {
    val contest = contestAdmin(c)
    val aid = c.pathInt("aid")
    val updated = c.db.exec(
        "UPDATE contest_users SET session_start = NULL, session_end = NULL WHERE contest_id = $1 AND account_id = $2", contest.id, aid,
    )
    if (updated == 0) throw notFound()
    c.audit("contest.user", "Flexible session reset", mapOf("account_id" to aid), contest.id)
    c.noContent()
}

@Serializable
private class RejudgeInput(val problemId: Int? = null)

suspend fun rejudgeContest(c: Ctx) {
    val contest = contestAdmin(c)
    val problemId = c.bind<RejudgeInput>().problemId
    val count = c.db.exec(
        "UPDATE submissions SET $RESET_SUBMISSION, priority = 2 WHERE contest_id = $1 AND ($2::int IS NULL OR problem_id = $2)",
        contest.id, problemId,
    )
    c.audit("contest.rejudge", "Contest submissions rejudged", mapOf("problem_id" to problemId, "submissions" to count), contest.id)
    c.ok("submissions" to count)
}

suspend fun contestLogs(c: Ctx) = logPage(c, contestAdmin(c).id)

