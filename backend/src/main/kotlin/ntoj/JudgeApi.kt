package ntoj

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString

private const val STALE_CLAIM = "10 minutes"
private const val MAX_JUDGE_OUTPUT = 16 shl 10

/** Hands the next queued submission to a worker, or 204. */
suspend fun judgeClaim(c: Ctx) {
    class Task(val id: Long, val problemId: Int, val language: String, val code: String)

    val task = c.db.one(
        """UPDATE submissions SET state = 100, claimed_at = now()
           WHERE id = (SELECT id FROM submissions WHERE state = 101 ORDER BY priority DESC, id FOR UPDATE SKIP LOCKED LIMIT 1)
           RETURNING id, problem_id, language, code""",
    ) { Task(it.getLong(1), it.getInt(2), it.getString(3), it.getString(4)) } ?: return c.noContent()
    val problem = c.db.jsonRow(
        """SELECT p.id, p.time_limit_ms, p.memory_limit_kb, p.checker,
               (SELECT coalesce(jsonb_agg(to_jsonb(t) - 'problem_id' ORDER BY t.name), '[]') FROM testcases t WHERE t.problem_id = p.id) AS testcases,
               (SELECT coalesce(jsonb_agg(to_jsonb(st) - 'problem_id' ORDER BY st.idx), '[]') FROM subtasks st WHERE st.problem_id = p.id) AS subtasks
           FROM problems p WHERE p.id = $1""",
        task.problemId,
    )
    c.ok("id" to task.id, "language" to task.language, "code" to task.code, "problem" to raw(problem))
}

/** Cuts [s] to at most [max] bytes of UTF-8 without splitting a character. */
private fun truncate(s: String, max: Int): String {
    val clean = s.replace("\u0000", "")
    val bytes = clean.toByteArray(Charsets.UTF_8)
    if (bytes.size <= max) return clean
    var end = max
    while (end > 0 && (bytes[end].toInt() and 0xC0) == 0x80) end--
    return String(bytes, 0, end, Charsets.UTF_8) + "\n[truncated]"
}

@Serializable
private class JudgeResultInput(
    /** CE, CLE, IE or JE; 0 when testcases were run. */
    val state: Int = 0,
    val message: String = "",
    val testcases: List<CaseResult> = emptyList(),
)

/**
 * Stores a verdict. Scores are always computed here, from the problem's
 * subtask configuration, never taken from the worker.
 */
suspend fun judgeResult(c: Ctx) {
    val id = c.pathInt("id")
    val input = c.bind<JudgeResultInput>()
    if (input.state != 0 && input.state !in State.CE..State.JE) throw badRequest("state must be 0 or a compile/judge error")
    c.db.tx { t ->
        val (problemId, precision) = t.one(
            """SELECT s.problem_id, p.score_precision FROM submissions s JOIN problems p ON p.id = s.problem_id
               WHERE s.id = $1 AND s.state = 100 FOR UPDATE OF s""",
            id,
        ) { it.getInt(1) to it.getInt(2) } ?: throw conflict("Submission is not being judged")

        var result = ScoreResult(input.state, 0.0, 0, 0, emptyList())
        val cases = ArrayList<CaseResult>()
        if (input.state == 0) {
            val names = t.query("SELECT id, name FROM testcases WHERE problem_id = $1", problemId) { it.getInt(1) to it.getString(2) }.toMap()
            val seen = HashSet<Int>()
            for (tc in input.testcases) {
                val name = names[tc.id]
                if (name == null || !seen.add(tc.id) || !State.validVerdict(tc.state) || tc.timeMs < 0 || tc.memoryKb < 0) {
                    throw badRequest("Invalid result for testcase ${tc.id}")
                }
                cases += tc.copy(name = name, message = truncate(tc.message, 4 shl 10))
            }
            val subtasks = t.query(
                "SELECT idx, score::float8, testcase_ids, depends FROM subtasks WHERE problem_id = $1 ORDER BY idx", problemId,
            ) { Subtask(it.getInt(1), it.getDouble(2), it.ints(3), it.ints(4)) }
            if (subtasks.isEmpty()) {
                // Without subtasks every testcase of the problem counts.
                names.filterKeys { it !in seen }.toSortedMap().forEach { (tid, name) -> cases += CaseResult(tid, name, State.SKIPPED) }
            }
            result = Scoring.evaluate(subtasks, cases, precision)
        }
        t.exec(
            """UPDATE submissions SET state = $2, score = $3, time_ms = $4, memory_kb = $5, message = $6,
               result = $7, judged_at = now() WHERE id = $1""",
            id, result.state, result.score, result.timeMs, result.memoryKb, truncate(input.message, MAX_JUDGE_OUTPUT),
            Jsonb(json.encodeToString(SubmissionResult(result.subtasks, cases))),
        )
    }
    c.noContent()
}

suspend fun judgeTestcase(c: Ctx) {
    val tid = c.pathInt("tid")
    val kind = c.call.parameters["kind"]
    if (kind != "in" && kind != "out") throw notFound()
    val (problemId, name) = c.db.one("SELECT problem_id, name FROM testcases WHERE id = $1", tid) { it.getInt(1) to it.getString(2) }
        ?: throw notFound()
    serveFile(c, c.s.testcaseFile(problemId, tid, kind), "$name.$kind")
}

/** Requeues submissions abandoned by a worker and removes expired sessions. */
fun runMaintenance(s: Server) {
    try {
        val requeued = s.db.exec(
            "UPDATE submissions SET state = 101, claimed_at = NULL WHERE state = 100 AND claimed_at < now() - $1::interval", STALE_CLAIM,
        )
        if (requeued > 0) s.log.warn("requeued {} abandoned submission(s)", requeued)
        s.db.exec("DELETE FROM sessions WHERE expires_at < now()")
    } catch (e: Exception) {
        s.log.error("maintenance failed", e)
    }
}
