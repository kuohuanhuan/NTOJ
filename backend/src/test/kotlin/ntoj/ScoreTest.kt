package ntoj

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals

class ScoreTest {
    private fun cases(vararg states: Int) = states.mapIndexed { i, s -> CaseResult(i + 1, state = s, timeMs = 10L * (i + 1), memoryKb = 100) }

    @Test
    fun withoutSubtasks() {
        var r = Scoring.evaluate(emptyList(), cases(State.AC, State.AC), 0)
        assertEquals(Triple(State.AC, 100.0, 20L), Triple(r.state, r.score, r.timeMs))
        r = Scoring.evaluate(emptyList(), cases(State.AC, State.TLE), 0)
        assertEquals(State.TLE to 0.0, r.state to r.score)
    }

    @Test
    fun groupMinAndDependencies() {
        val subtasks = listOf(Subtask(1, 30.0, listOf(1)), Subtask(2, 70.0, listOf(2, 3), listOf(1)))
        var r = Scoring.evaluate(subtasks, cases(State.AC, State.AC, State.WA), 0)
        assertEquals(State.PC to 30.0, r.state to r.score)
        assertEquals(State.WA to 0.0, r.subtasks[1].state to r.subtasks[1].score)
        // Failing the dependency voids the dependent subtask even if its own cases pass.
        r = Scoring.evaluate(subtasks, cases(State.RE, State.AC, State.AC), 0)
        assertEquals(State.RE to 0.0, r.state to r.score)
        assertEquals(State.SKIPPED, r.subtasks[1].state)
        r = Scoring.evaluate(subtasks, cases(State.AC, State.AC, State.AC), 0)
        assertEquals(State.AC to 100.0, r.state to r.score)
    }

    @Test
    fun partialRatioAndMissingCase() {
        val subtasks = listOf(Subtask(1, 10.0, listOf(1, 2)))
        val base = cases(State.AC, State.PC)
        var r = Scoring.evaluate(subtasks, listOf(base[0], base[1].copy(ratio = 0.333)), 2)
        assertEquals(State.PC to 3.33, r.state to r.score)
        // Out-of-range ratios are clamped.
        r = Scoring.evaluate(subtasks, listOf(base[0], base[1].copy(ratio = 7.0)), 2)
        assertEquals(10.0, r.score)
        assertNotEquals(State.AC, r.state)
        // A testcase the judge never reported counts as skipped, not as passed.
        r = Scoring.evaluate(subtasks, cases(State.AC), 0)
        assertEquals(0.0, r.score)
        assertNotEquals(State.AC, r.state)
    }
}
