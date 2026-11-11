package ntoj

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.encodeToJsonElement

/**
 * Each account's best result per publicly visible problem, counting only
 * submissions made outside contests.
 */
const val BEST_CTE = """best AS (
    SELECT s.account_id, s.problem_id, max(s.score) AS score, min(s.state) AS state
    FROM submissions s JOIN problems p ON p.id = s.problem_id AND p.status = 'online'
    WHERE s.contest_id IS NULL AND s.state < 100
    GROUP BY s.account_id, s.problem_id)"""

/** The ID of a running contest that confines the caller to contest pages, or 0. */
fun lockdownContest(c: Ctx): Int {
    val acct = c.acct ?: return 0
    if (acct.admin) return 0
    return c.db.one(
        """SELECT c.id FROM contest_users cu JOIN contests c ON c.id = cu.contest_id
           WHERE cu.account_id = $1 AND cu.status = 'approved' AND NOT c.allow_other_pages
             AND now() >= CASE WHEN c.time_mode = 'flexible' THEN cu.session_start ELSE c.start_at END
             AND now() <  CASE WHEN c.time_mode = 'flexible' THEN cu.session_end ELSE c.end_at END
           ORDER BY c.id LIMIT 1""",
        acct.id,
    ) { it.getInt(1) } ?: 0
}

fun checkLockdown(c: Ctx) {
    val id = lockdownContest(c)
    if (id != 0) {
        throw ApiError(403, "contest_lockdown", "This page is unavailable while you are taking part in a contest", mapOf("contest_id" to id))
    }
}

suspend fun getAccount(c: Ctx) {
    val id = c.pathInt("id")
    c.okRaw(
        c.db.jsonRow(
            """WITH $BEST_CTE
               SELECT a.id, a.name, a.role, a.photo_url, a.cover_url, a.motto, a.created_at,
                   (SELECT coalesce(sum(score), 0) FROM best WHERE account_id = a.id) AS rate,
                   (SELECT count(*) FROM best WHERE account_id = a.id AND state = 1) AS ac_problems,
                   (SELECT count(*) FROM problems WHERE status = 'online') AS total_problems,
                   (SELECT count(*) FROM submissions s JOIN problems p ON p.id = s.problem_id AND p.status = 'online'
                       WHERE s.account_id = a.id AND s.contest_id IS NULL) AS submissions,
                   (SELECT count(*) FROM submissions s JOIN problems p ON p.id = s.problem_id AND p.status = 'online'
                       WHERE s.account_id = a.id AND s.contest_id IS NULL AND s.state = 1) AS ac_submissions,
                   (SELECT coalesce(jsonb_agg(jsonb_build_object('id', p.id, 'state', b.state, 'score', b.score) ORDER BY p.id), '[]')
                       FROM problems p LEFT JOIN best b ON b.problem_id = p.id AND b.account_id = a.id
                       WHERE p.status = 'online') AS problems
               FROM accounts a WHERE a.id = $1 AND NOT a.disabled""",
            id,
        ),
    )
}

suspend fun userRank(c: Ctx) {
    val page = c.page(50)
    val items = c.db.jsonList(
        """WITH $BEST_CTE
           SELECT a.id, a.name, a.motto, a.photo_url, coalesce(sum(b.score), 0) AS rate,
               count(b.problem_id) FILTER (WHERE b.state = 1) AS ac_problems,
               rank() OVER (ORDER BY coalesce(sum(b.score), 0) DESC) AS rank
           FROM accounts a LEFT JOIN best b ON b.account_id = a.id
           WHERE NOT a.disabled GROUP BY a.id ORDER BY rate DESC, a.id LIMIT $1 OFFSET $2""",
        page.limit, page.offset,
    )
    c.paged(items, c.db.count("SELECT count(*) FROM accounts WHERE NOT disabled"), page)
}

fun escapeLike(s: String) = s.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")

suspend fun adminListAccounts(c: Ctx) {
    val page = c.page(50)
    val term = c.query("q").trim()
    val like = "%" + escapeLike(term) + "%"
    val where = " WHERE (name ILIKE $1 OR email ILIKE $1 OR id::text = $2)"
    val items = c.db.jsonList(
        "SELECT id, email, name, role, disabled, last_ip, allowed_ip, created_at FROM accounts$where ORDER BY id LIMIT $3 OFFSET $4",
        like, term, page.limit, page.offset,
    )
    c.paged(items, c.db.count("SELECT count(*) FROM accounts$where", like, term), page)
}

@Serializable
private class AccountPatch(val name: String? = null, val role: String? = null, val disabled: Boolean? = null, val allowedIp: String? = null)

suspend fun adminUpdateAccount(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<AccountPatch>()
    val name = input.name?.let(::cleanText)?.also(::checkName)
    if (input.role != null && input.role != "admin" && input.role != "user") throw badRequest("Invalid role")
    val allowedIp = input.allowedIp?.let {
        if (it.isBlank()) "" else parseIp(it)?.hostAddress ?: throw badRequest("Allowed IP must be a valid IP address")
    }
    if (id == c.acctId && ((input.role != null && input.role != "admin") || input.disabled == true)) {
        throw badRequest("You cannot demote or disable your own account")
    }
    c.db.tx { t ->
        val updated = t.exec(
            """UPDATE accounts SET name = coalesce($2, name), role = coalesce($3, role),
               disabled = coalesce($4, disabled), allowed_ip = coalesce($5, allowed_ip) WHERE id = $1""",
            id, name, input.role, input.disabled, allowedIp,
        )
        if (updated == 0) throw notFound()
        if (input.disabled == true) t.exec("DELETE FROM sessions WHERE account_id = $1", id)
    }
    c.audit("manage.account.update", "Account updated", mapOf("account_id" to id, "changes" to json.encodeToJsonElement(input)))
    c.noContent()
}

@Serializable
private class ResetPasswordInput(val password: String = "")

suspend fun adminResetPassword(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<ResetPasswordInput>()
    Auth.passwordProblem(input.password)?.let { throw badRequest(it) }
    val hash = Auth.hashPassword(input.password)
    c.db.tx { t ->
        if (t.exec("UPDATE accounts SET password_hash = $2 WHERE id = $1", id, hash) == 0) throw notFound()
        t.exec("DELETE FROM sessions WHERE account_id = $1 AND id <> $2", id, c.acct!!.sessionId)
    }
    c.audit("manage.account.password", "Password reset by administrator", mapOf("account_id" to id))
    c.noContent()
}
