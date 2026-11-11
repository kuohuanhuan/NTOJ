package ntoj

import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.round
import kotlinx.serialization.Serializable

/** Submission states. The numbering is part of the stored data format. */
object State {
    const val AC = 1
    const val PC = 2
    const val WA = 3
    const val RE = 4
    const val RESIG = 5
    const val TLE = 6
    const val MLE = 7
    const val OLE = 8
    const val CE = 9
    const val CLE = 10
    const val IE = 11
    const val JE = 12
    const val JUDGING = 100
    const val PENDING = 101
    const val SKIPPED = 102
    const val REJECTED = 103

    /** Whether a judge may report [state] for a testcase. */
    fun validVerdict(state: Int) = state in AC..JE || state == SKIPPED

    /** Whether a submission state is final. */
    fun finished(state: Int) = state < JUDGING || state > PENDING
}

@Serializable
data class Subtask(val idx: Int, val score: Double, val testcaseIds: List<Int> = emptyList(), val depends: List<Int> = emptyList())

@Serializable
data class CaseResult(
    val id: Int,
    val name: String = "",
    val state: Int = 0,
    val timeMs: Long = 0,
    val memoryKb: Long = 0,
    val message: String = "",
    /** Fraction of the points earned when [state] is PC. */
    val ratio: Double = 0.0,
)

@Serializable
data class SubtaskResult(
    val idx: Int,
    val state: Int = 0,
    val score: Double = 0.0,
    val maxScore: Double = 0.0,
    val timeMs: Long = 0,
    val memoryKb: Long = 0,
)

data class ScoreResult(val state: Int, val score: Double, val timeMs: Long, val memoryKb: Long, val subtasks: List<SubtaskResult>)

/** Turns per-testcase verdicts into subtask and total results. */
object Scoring {
    fun round(v: Double, precision: Int): Double {
        val p = 10.0.pow(precision)
        return round(v * p) / p
    }

    private fun ratio(c: CaseResult) = when (c.state) {
        State.AC -> 1.0
        State.PC -> max(0.0, min(1.0, c.ratio))
        else -> 0.0
    }

    /**
     * Group-min scoring: a subtask earns its score multiplied by the lowest
     * ratio among its testcases, and nothing when a subtask it depends on was
     * not fully solved. A problem without subtasks is treated as a single
     * 100-point subtask covering every reported testcase.
     */
    fun evaluate(configured: List<Subtask>, cases: List<CaseResult>, precision: Int): ScoreResult {
        val byId = cases.associateBy { it.id }
        val subtasks = configured.ifEmpty { listOf(Subtask(1, 100.0, cases.map { it.id })) }.sortedBy { it.idx }

        val full = HashMap<Int, Boolean>()
        val results = ArrayList<SubtaskResult>()
        var firstBad = 0
        var maxTotal = 0.0
        var total = 0.0
        for (st in subtasks) {
            var state = State.AC
            var lowest = 1.0
            var time = 0L
            var memory = 0L
            for (id in st.testcaseIds) {
                val c = byId[id] ?: CaseResult(id, state = State.SKIPPED)
                lowest = min(lowest, ratio(c))
                if (c.state != State.AC && state == State.AC) state = c.state
                time = max(time, c.timeMs)
                memory = max(memory, c.memoryKb)
            }
            for (dep in st.depends) {
                if (full[dep] != true) {
                    lowest = 0.0
                    if (state == State.AC) state = State.SKIPPED
                }
            }
            full[st.idx] = lowest == 1.0 && state == State.AC
            val earned = round(st.score * lowest, precision)
            if (state != State.AC && state != State.SKIPPED && firstBad == 0) firstBad = state
            maxTotal += st.score
            total += earned
            results.add(SubtaskResult(st.idx, state, earned, st.score, time, memory))
        }
        total = round(total, precision)
        val state = when {
            subtasks.all { full[it.idx] == true } -> State.AC
            total > 0 && total < maxTotal -> State.PC
            firstBad != 0 && firstBad != State.PC -> firstBad
            else -> State.WA
        }
        return ScoreResult(state, total, results.maxOfOrNull { it.timeMs } ?: 0, results.maxOfOrNull { it.memoryKb } ?: 0, results)
    }
}
