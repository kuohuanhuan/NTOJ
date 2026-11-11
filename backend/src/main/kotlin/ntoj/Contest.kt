package ntoj

import java.time.Duration
import java.time.Instant
import java.time.OffsetDateTime
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.Transient
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

// Contest rules that do not touch storage: who may see or do what at a given
// time, and how scoreboards are computed.

/** ISO-8601 timestamps, accepting any UTC offset on input. */
object InstantSerializer : KSerializer<Instant> {
    override val descriptor = PrimitiveSerialDescriptor("Instant", PrimitiveKind.STRING)
    override fun serialize(encoder: Encoder, value: Instant) = encoder.encodeString(value.toString())
    override fun deserialize(decoder: Decoder): Instant = try {
        OffsetDateTime.parse(decoder.decodeString()).toInstant()
    } catch (e: java.time.format.DateTimeParseException) {
        throw kotlinx.serialization.SerializationException("invalid timestamp")
    }
}

typealias Time = @Serializable(InstantSerializer::class) Instant

@Serializable
data class Contest(
    val id: Int = 0,
    var name: String = "",
    val creatorId: Int? = null,
    var mode: String = "ioi",
    var startAt: Time,
    var endAt: Time,
    var timeMode: String = "fixed",
    var durationSec: Int = 0,
    var regMode: String = "invited",
    var regEndAt: Time,
    var languages: List<String> = emptyList(),
    var publicScoreboard: Boolean = false,
    var allowOtherPages: Boolean = true,
    var hideAdmin: Boolean = true,
    var cooldownSec: Int = 30,
    var freezeMinutes: Int = 0,
    var keepFrozen: Boolean = false,
    var penaltyMinutes: Int = 20,
    var descBefore: String = "",
    var descDuring: String = "",
    var descAfter: String = "",
) {
    val flexible get() = timeMode == "flexible"
}

/** An account's row in a contest. */
data class Member(val status: String, val sessionStart: Instant? = null, val sessionEnd: Instant? = null)

object Phase {
    const val BEFORE = "before"
    const val RUNNING = "running"
    const val ENDED = "ended"

    fun of(start: Instant, end: Instant, now: Instant) = when {
        !now.isBefore(end) -> ENDED
        now.isBefore(start) -> BEFORE
        else -> RUNNING
    }
}

/** Which contest submissions a viewer may list. */
object Scope {
    const val NONE = "none"
    const val OWN = "own"
    /** Approved contestants only, never admins. */
    const val PARTICIPANTS = "participants"
    const val ALL = "all"
}

/**
 * The resolved relation between one viewer and one contest at one instant.
 * Handlers consult it instead of re-deriving timing rules.
 */
@Serializable
data class Access(
    @Transient val accountId: Int = 0,
    /** The viewer's own window. */
    val phase: String,
    /** The contest's configured window. */
    val global: String,
    /** "", requested, approved, rejected or admin. */
    val status: String,
    /** False: flexible session not started yet. */
    val activated: Boolean,
    val start: Time,
    val end: Time,
    val isAdmin: Boolean,
    val isMember: Boolean,
    val isParticipant: Boolean,
    val canViewProblems: Boolean,
    val canSubmit: Boolean,
    val canViewScoreboard: Boolean,
    val canAsk: Boolean,
    val canStart: Boolean,
    val canRegister: Boolean,
    val canWithdraw: Boolean,
    val submissions: String,
    /** Contest problems' result_style applies to this viewer. */
    val restrictResults: Boolean,
) {
    /**
     * Whether the viewer may open one submission's result page. Source code is
     * governed separately (owner and admins only).
     */
    fun canViewSubmission(ownerId: Int, ownerIsAdmin: Boolean) = when (submissions) {
        Scope.ALL -> true
        Scope.OWN -> accountId != 0 && ownerId == accountId
        Scope.PARTICIPANTS -> !ownerIsAdmin || ownerId == accountId
        else -> false
    }

    companion object {
        /** Computes the access of account [acctId] (0 for guests). */
        fun resolve(c: Contest, acctId: Int, m: Member?, siteAdmin: Boolean, now: Instant): Access {
            val global = Phase.of(c.startAt, c.endAt, now)
            val status = m?.status ?: ""
            val isAdmin = acctId != 0 && (siteAdmin || status == "admin" || c.creatorId == acctId)
            val isParticipant = status == "approved" && !isAdmin
            val isMember = isAdmin || isParticipant

            var start = c.startAt
            var end = c.endAt
            var activated = true
            if (c.flexible && isParticipant) {
                if (m?.sessionStart != null && m.sessionEnd != null) {
                    start = m.sessionStart
                    end = m.sessionEnd
                } else {
                    activated = false
                }
            }
            val phase = if (activated) Phase.of(start, end, now) else Phase.BEFORE

            // A flexible contestant whose own session is over must not see the
            // set again while others can still start theirs.
            val endedView = (phase == Phase.ENDED && (!(c.flexible && isParticipant) || global == Phase.ENDED)) ||
                (!activated && global == Phase.ENDED)
            val available = if (c.flexible && !isAdmin) {
                global == Phase.ENDED || (isParticipant && activated)
            } else {
                phase != Phase.BEFORE
            }
            val regOpen = now.isBefore(c.regEndAt)

            val effective = if (!activated && global == Phase.ENDED) Phase.ENDED else phase
            val scope = when {
                isAdmin -> Scope.ALL
                effective == Phase.BEFORE -> Scope.NONE
                effective == Phase.RUNNING || !c.publicScoreboard -> if (acctId == 0) Scope.NONE else Scope.OWN
                else -> Scope.PARTICIPANTS
            }
            return Access(
                accountId = acctId, phase = phase, global = global, status = status, activated = activated,
                start = start, end = end, isAdmin = isAdmin, isMember = isMember, isParticipant = isParticipant,
                canViewProblems = isAdmin || endedView || (phase == Phase.RUNNING && isMember),
                canSubmit = isMember && (isAdmin || phase == Phase.RUNNING),
                canViewScoreboard = isAdmin || (available && (c.publicScoreboard || isMember)),
                canAsk = isParticipant && global != Phase.ENDED,
                canStart = c.flexible && isParticipant && !activated && global == Phase.RUNNING,
                canRegister = acctId != 0 && !isAdmin && c.regMode != "invited" && regOpen && status == "",
                canWithdraw = !isAdmin && (status == "requested" || status == "approved") && regOpen && now.isBefore(c.startAt),
                submissions = scope,
                restrictResults = !isAdmin && phase == Phase.RUNNING,
            )
        }
    }
}

@Serializable
data class BoardProblem(val id: Int, val name: String, val scoreType: String)

data class BoardUser(val id: Int, val name: String, val start: Instant, val end: Instant)

data class BoardSubmission(
    val id: Long,
    val accountId: Int,
    val problemId: Int,
    val at: Instant,
    val state: Int,
    val score: Double,
    val subtasks: List<SubtaskResult> = emptyList(),
)

@Serializable
data class Cell(
    var score: Double = 0.0,
    /** Judged attempts (ACM: up to and including the first AC). */
    var attempts: Int = 0,
    var solved: Boolean = false,
    /** ACM: minutes from the account's start to the first AC. */
    var minutes: Int = 0,
    /** Attempts hidden by the scoreboard freeze. */
    var frozen: Int = 0,
    var pending: Int = 0,
)

@Serializable
data class BoardRow(
    var rank: Int = 0,
    val accountId: Int,
    val name: String,
    var total: Double = 0.0,
    var solved: Int = 0,
    var penalty: Int = 0,
    val cells: MutableMap<Int, Cell> = LinkedHashMap(),
)

object Scoreboard {
    /**
     * Ranks [users]. [subs] must be ordered by ID ascending; submissions outside
     * an account's own window are ignored. A positive [freezeMinutes] hides
     * submissions made in the last minutes of each account's window.
     */
    fun compute(
        mode: String,
        penaltyMinutes: Int,
        freezeMinutes: Int,
        problems: List<BoardProblem>,
        users: List<BoardUser>,
        subs: List<BoardSubmission>,
    ): List<BoardRow> {
        val acm = mode == "acm"
        val rows = users.associate { it.id to BoardRow(accountId = it.id, name = it.name) }
        val window = users.associateBy { it.id }
        val scoreType = problems.associate { it.id to it.scoreType }
        val union = HashMap<Pair<Int, Int>, MutableMap<Int, Double>>() // (account, problem) -> subtask -> best

        for (s in subs) {
            val row = rows[s.accountId] ?: continue
            val type = scoreType[s.problemId] ?: continue
            val u = window.getValue(s.accountId)
            if (s.at.isBefore(u.start) || !s.at.isBefore(u.end)) continue
            val cell = row.cells.getOrPut(s.problemId) { Cell() }
            if (freezeMinutes > 0 && !s.at.isBefore(u.end.minus(Duration.ofMinutes(freezeMinutes.toLong())))) {
                cell.frozen++
                continue
            }
            if (!State.finished(s.state)) {
                cell.pending++
                continue
            }
            if (acm) {
                if (cell.solved || s.state >= State.CE) continue // compile and judge errors are free
                if (s.state == State.AC) {
                    cell.solved = true
                    cell.minutes = Duration.between(u.start, s.at).toMinutes().toInt()
                    row.solved++
                    row.penalty += cell.minutes + cell.attempts * penaltyMinutes
                }
                cell.attempts++
                continue
            }
            cell.attempts++
            if (s.state == State.AC) cell.solved = true
            if (type == "subtask_union") {
                val best = union.getOrPut(s.accountId to s.problemId) { HashMap() }
                for (sr in s.subtasks) if (sr.score > (best[sr.idx] ?: 0.0)) best[sr.idx] = sr.score
                cell.score = Scoring.round(best.values.sum(), 3)
            } else if (s.score > cell.score) {
                cell.score = s.score
            }
        }

        val better: Comparator<BoardRow> =
            if (acm) compareByDescending<BoardRow> { it.solved }.thenBy { it.penalty } else compareByDescending { it.total }
        val out = users.map { u -> rows.getValue(u.id).also { r -> r.total = Scoring.round(r.cells.values.sumOf { it.score }, 3) } }
            .sortedWith(better.thenBy { it.accountId })
        out.forEachIndexed { i, row ->
            row.rank = if (i > 0 && better.compare(out[i - 1], row) == 0) out[i - 1].rank else i + 1
        }
        return out
    }
}
