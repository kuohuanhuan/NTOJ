package ntoj

import kotlinx.serialization.Serializable

// ---- Bulletins ----

@Serializable
private class BulletinInput(val title: String = "", val content: String = "", val color: String = "", val pinned: Boolean = false) {
    fun clean(): BulletinInput {
        val out = BulletinInput(cleanText(title), cleanText(content), color, pinned)
        if (runeCount(out.title) !in 1..100) throw badRequest("Title must be 1 to 100 characters")
        if (out.content.isEmpty() || out.content.toByteArray(Charsets.UTF_8).size > MAX_STATEMENT) {
            throw badRequest("Content must not be empty or too long")
        }
        if (color !in setOf("", "red", "orange", "yellow", "green", "blue", "purple")) throw badRequest("Invalid color")
        return out
    }
}

private const val BULLETIN_SELECT = """SELECT b.id, b.title, b.content, b.color, b.pinned, b.created_at, b.author_id, a.name AS author_name
    FROM bulletins b LEFT JOIN accounts a ON a.id = b.author_id"""

suspend fun listBulletins(c: Ctx) = c.okRaw(c.db.jsonList("$BULLETIN_SELECT ORDER BY b.pinned DESC, b.id DESC LIMIT 200"))

suspend fun getBulletin(c: Ctx) = c.okRaw(c.db.jsonRow("$BULLETIN_SELECT WHERE b.id = $1", c.pathInt("id")))

suspend fun adminCreateBulletin(c: Ctx) {
    val b = c.bind<BulletinInput>().clean()
    val id = c.db.one(
        "INSERT INTO bulletins (title, content, color, pinned, author_id) VALUES ($1, $2, $3, $4, $5) RETURNING id",
        b.title, b.content, b.color, b.pinned, c.acctId,
    ) { it.getInt(1) }!!
    c.audit("manage.bulletin.create", "Bulletin published", mapOf("bulletin_id" to id, "title" to b.title))
    c.created("id" to id)
}

suspend fun adminUpdateBulletin(c: Ctx) {
    val id = c.pathInt("id")
    val b = c.bind<BulletinInput>().clean()
    val updated = c.db.exec("UPDATE bulletins SET title = $2, content = $3, color = $4, pinned = $5 WHERE id = $1", id, b.title, b.content, b.color, b.pinned)
    if (updated == 0) throw notFound()
    c.audit("manage.bulletin.update", "Bulletin updated", mapOf("bulletin_id" to id, "title" to b.title))
    c.noContent()
}

suspend fun adminDeleteBulletin(c: Ctx) {
    val id = c.pathInt("id")
    if (c.db.exec("DELETE FROM bulletins WHERE id = $1", id) == 0) throw notFound()
    c.audit("manage.bulletin.delete", "Bulletin deleted", mapOf("bulletin_id" to id))
    c.noContent()
}

// ---- Problem classes ----

/**
 * WHERE fragment deciding which classes $1 (account id, 0 for guests) may
 * see; $2 is whether the caller is a site administrator.
 */
private const val CLASS_VISIBLE = "(pc.public OR pc.owner_id = \$1 OR (\$2 AND pc.owner_id IS NULL))"

private const val CLASS_SELECT = """SELECT pc.id, pc.name, pc.description, pc.owner_id, a.name AS owner_name, pc.public,
        cardinality(pc.problem_ids) AS problem_count, pc.problem_ids,
        EXISTS (SELECT 1 FROM class_collections cc WHERE cc.class_id = pc.id AND cc.account_id = $1) AS collected
    FROM problem_classes pc LEFT JOIN accounts a ON a.id = pc.owner_id WHERE """ + CLASS_VISIBLE

/** WHERE fragment restricting writes to the owner, or to site administrators for official classes. */
private const val CLASS_OWNER = "(owner_id = \$2 OR (\$3 AND owner_id IS NULL))"

suspend fun listClasses(c: Ctx) =
    c.okRaw(c.db.jsonList("$CLASS_SELECT ORDER BY pc.owner_id IS NOT NULL, pc.id LIMIT 1000", c.acctId, c.admin))

suspend fun getClass(c: Ctx) = c.okRaw(c.db.jsonRow("$CLASS_SELECT AND pc.id = $3", c.acctId, c.admin, c.pathInt("id")))

@Serializable
private class ClassInput(
    val name: String = "",
    val description: String = "",
    val problemIds: List<Int> = emptyList(),
    val public: Boolean = false,
    val official: Boolean = false,
) {
    fun clean(): ClassInput {
        val out = ClassInput(cleanText(name), cleanText(description), problemIds.distinct().sorted(), public, official)
        if (runeCount(out.name) !in 1..50) throw badRequest("Name must be 1 to 50 characters")
        if (runeCount(out.description) > 2048) throw badRequest("Description must be at most 2048 characters")
        if (out.problemIds.size !in 1..1000) throw badRequest("A class needs between 1 and 1000 problems")
        if (out.problemIds.any { it <= 0 }) throw badRequest("Invalid problem id")
        return out
    }
}

suspend fun createClass(c: Ctx) {
    val input = c.bind<ClassInput>().clean()
    val owner: Int?
    if (input.official) {
        if (!c.admin) throw forbidden()
        owner = null
    } else {
        owner = c.acctId
        if (c.db.count("SELECT count(*) FROM problem_classes WHERE owner_id = $1", c.acctId) >= 50) {
            throw conflict("You have reached the limit of 50 problem classes")
        }
    }
    val id = c.db.one(
        "INSERT INTO problem_classes (name, description, problem_ids, owner_id, public) VALUES ($1, $2, $3, $4, $5) RETURNING id",
        input.name, input.description, IntArr(input.problemIds), owner, input.public,
    ) { it.getInt(1) }!!
    c.audit("class.create", "Problem class created", mapOf("class_id" to id, "name" to input.name, "official" to input.official))
    c.created("id" to id)
}

suspend fun updateClass(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<ClassInput>().clean()
    val updated = c.db.exec(
        "UPDATE problem_classes SET name = $4, description = $5, problem_ids = $6, public = $7 WHERE id = $1 AND $CLASS_OWNER",
        id, c.acctId, c.admin, input.name, input.description, IntArr(input.problemIds), input.public,
    )
    if (updated == 0) throw notFound()
    c.audit("class.update", "Problem class updated", mapOf("class_id" to id))
    c.noContent()
}

suspend fun deleteClass(c: Ctx) {
    val id = c.pathInt("id")
    if (c.db.exec("DELETE FROM problem_classes WHERE id = $1 AND $CLASS_OWNER", id, c.acctId, c.admin) == 0) throw notFound()
    c.audit("class.delete", "Problem class deleted", mapOf("class_id" to id))
    c.noContent()
}

suspend fun collectClass(c: Ctx) {
    // Classes the caller cannot see are silently skipped.
    c.db.exec(
        """INSERT INTO class_collections (account_id, class_id)
           SELECT $1, pc.id FROM problem_classes pc WHERE pc.id = $3 AND $CLASS_VISIBLE ON CONFLICT DO NOTHING""",
        c.acctId, c.admin, c.pathInt("id"),
    )
    c.noContent()
}

suspend fun uncollectClass(c: Ctx) {
    c.db.exec("DELETE FROM class_collections WHERE account_id = $1 AND class_id = $2", c.acctId, c.pathInt("id"))
    c.noContent()
}

// ---- Boards ----

@Serializable
private class BoardInput(
    val name: String = "",
    val visible: Boolean = false,
    val startAt: Time,
    val endAt: Time,
    val problemIds: List<Int> = emptyList(),
    val accountIds: List<Int> = emptyList(),
) {
    /** Removes repeats and non-positive IDs while keeping the given order. */
    private fun dedupe(ids: List<Int>) = ids.filter { it > 0 }.distinct()

    fun clean(): BoardInput {
        val out = BoardInput(cleanText(name), visible, startAt, endAt, dedupe(problemIds), dedupe(accountIds))
        if (runeCount(out.name) !in 1..64) throw badRequest("Name must be 1 to 64 characters")
        if (!endAt.isAfter(startAt)) throw badRequest("The board must end after it starts")
        if (out.problemIds.size > 200 || out.accountIds.size > 2000) throw badRequest("Too many problems or accounts")
        return out
    }
}

suspend fun listBoards(c: Ctx) = c.okRaw(c.db.jsonList("SELECT id, name, start_at, end_at FROM boards WHERE visible ORDER BY id DESC"))

suspend fun adminListBoards(c: Ctx) = c.okRaw(c.db.jsonList("SELECT * FROM boards ORDER BY id DESC"))

suspend fun getBoard(c: Ctx) = c.okRaw(
    c.db.jsonRow(
        """SELECT b.id, b.name, b.start_at, b.end_at, b.visible,
               (SELECT coalesce(jsonb_agg(jsonb_build_object('id', p.id, 'name', CASE WHEN p.status = 'online' OR $2 THEN p.name ELSE '' END)
                   ORDER BY array_position(b.problem_ids, p.id)), '[]') FROM problems p WHERE p.id = ANY (b.problem_ids)) AS problems,
               (SELECT coalesce(jsonb_agg(r ORDER BY r.total DESC, r.account_id), '[]') FROM (
                   SELECT a.id AS account_id, a.name, coalesce(sum(best.score), 0) AS total,
                       coalesce(jsonb_object_agg(best.problem_id, jsonb_build_object('score', best.score, 'state', best.state))
                           FILTER (WHERE best.problem_id IS NOT NULL), '{}') AS cells
                   FROM accounts a LEFT JOIN (
                       SELECT s.account_id, s.problem_id, max(s.score) AS score, min(s.state) AS state FROM submissions s
                       WHERE s.contest_id IS NULL AND s.state < 100 AND s.problem_id = ANY (b.problem_ids)
                         AND s.created_at >= b.start_at AND s.created_at < b.end_at
                       GROUP BY s.account_id, s.problem_id) best ON best.account_id = a.id
                   WHERE a.id = ANY (b.account_ids) GROUP BY a.id) r) AS rows
           FROM boards b WHERE b.id = $1 AND (b.visible OR $2)""",
        c.pathInt("id"), c.admin,
    ),
)

suspend fun adminCreateBoard(c: Ctx) {
    val b = c.bind<BoardInput>().clean()
    val id = c.db.one(
        "INSERT INTO boards (name, visible, start_at, end_at, problem_ids, account_ids) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
        b.name, b.visible, b.startAt, b.endAt, IntArr(b.problemIds), IntArr(b.accountIds),
    ) { it.getInt(1) }!!
    c.audit("manage.board.create", "Board created", mapOf("board_id" to id, "name" to b.name))
    c.created("id" to id)
}

suspend fun adminUpdateBoard(c: Ctx) {
    val id = c.pathInt("id")
    val b = c.bind<BoardInput>().clean()
    val updated = c.db.exec(
        "UPDATE boards SET name = $2, visible = $3, start_at = $4, end_at = $5, problem_ids = $6, account_ids = $7 WHERE id = $1",
        id, b.name, b.visible, b.startAt, b.endAt, IntArr(b.problemIds), IntArr(b.accountIds),
    )
    if (updated == 0) throw notFound()
    c.audit("manage.board.update", "Board updated", mapOf("board_id" to id))
    c.noContent()
}

suspend fun adminDeleteBoard(c: Ctx) {
    val id = c.pathInt("id")
    if (c.db.exec("DELETE FROM boards WHERE id = $1", id) == 0) throw notFound()
    c.audit("manage.board.delete", "Board deleted", mapOf("board_id" to id))
    c.noContent()
}

// ---- Questions to the administrators ----

suspend fun listQuestions(c: Ctx) {
    val out = c.db.jsonList("SELECT id, content, reply, created_at, replied_at FROM questions WHERE account_id = $1 ORDER BY id DESC", c.acctId)
    c.db.exec("UPDATE questions SET reply_seen = true WHERE account_id = $1 AND reply IS NOT NULL AND NOT reply_seen", c.acctId)
    c.okRaw(out)
}

@Serializable
private class QuestionInput(val content: String = "")

suspend fun askQuestion(c: Ctx) {
    val content = cleanText(c.bind<QuestionInput>().content)
    if (runeCount(content) !in 1..1024) throw badRequest("Question must be 1 to 1024 characters")
    if (!c.s.limiter.allow("question:${c.ip}", 10, 600)) throw tooMany()
    val id = c.db.one(
        """INSERT INTO questions (account_id, content)
           SELECT $1, $2 WHERE (SELECT count(*) FROM questions WHERE account_id = $1) < 20 RETURNING id""",
        c.acctId, content,
    ) { it.getInt(1) } ?: throw conflict("You have too many questions; delete old ones first")
    c.created("id" to id)
}

suspend fun deleteQuestion(c: Ctx) {
    if (c.db.exec("DELETE FROM questions WHERE id = $1 AND account_id = $2", c.pathInt("id"), c.acctId) == 0) throw notFound()
    c.noContent()
}

suspend fun adminListQuestions(c: Ctx) {
    val page = c.page(50)
    val items = c.db.jsonList(
        """SELECT q.id, q.account_id, a.name AS account_name, q.content, q.reply, q.created_at, q.replied_at
           FROM questions q JOIN accounts a ON a.id = q.account_id
           ORDER BY q.reply IS NOT NULL, q.id DESC LIMIT $1 OFFSET $2""",
        page.limit, page.offset,
    )
    c.paged(items, c.db.count("SELECT count(*) FROM questions"), page)
}

suspend fun adminReplyQuestion(c: Ctx) {
    val id = c.pathInt("id")
    val reply = c.bind<ReplyInput>().clean()
    val updated = c.db.exec(
        "UPDATE questions SET reply = $2, replier_id = $3, replied_at = now(), reply_seen = false WHERE id = $1", id, reply, c.acctId,
    )
    if (updated == 0) throw notFound()
    c.noContent()
}

// ---- Audit log and dashboard ----

suspend fun logPage(c: Ctx, contestId: Int?) {
    val page = c.page(50)
    val type = escapeLike(c.query("type").trim())
    val where = " WHERE (\$1::int IS NULL OR l.contest_id = \$1) AND (\$2 = '' OR l.type LIKE \$2 || '%')"
    val items = c.db.jsonList(
        """SELECT l.id, l.type, l.message, l.params, l.operator_id, a.name AS operator_name,
               l.operator_ip, l.contest_id, l.created_at
           FROM audit_logs l LEFT JOIN accounts a ON a.id = l.operator_id$where ORDER BY l.id DESC LIMIT $3 OFFSET $4""",
        contestId, type, page.limit, page.offset,
    )
    c.paged(items, c.db.count("SELECT count(*) FROM audit_logs l$where", contestId, type), page)
}

suspend fun adminLogs(c: Ctx) = logPage(c, null)

suspend fun adminDashboard(c: Ctx) = c.okRaw(
    c.db.jsonRow(
        """SELECT
               (SELECT count(*) FROM accounts) AS accounts,
               (SELECT count(*) FROM problems) AS problems,
               (SELECT count(*) FROM contests) AS contests,
               (SELECT count(*) FROM submissions) AS submissions,
               (SELECT count(*) FROM submissions WHERE state = 101) AS pending,
               (SELECT count(*) FROM submissions WHERE state = 100) AS judging,
               (SELECT count(*) FROM questions WHERE reply IS NULL) AS open_questions,
               $1::boolean AS judge_configured""",
        c.s.cfg.judgeToken.isNotEmpty(),
    ),
)
