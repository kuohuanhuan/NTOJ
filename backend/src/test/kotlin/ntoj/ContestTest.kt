package ntoj

import java.time.Duration
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ContestTest {
    private val t0 = Instant.parse("2026-01-01T12:00:00Z")
    private fun at(minutes: Long) = t0.plus(Duration.ofMinutes(minutes))

    private fun fixed() = Contest(
        id = 1, mode = "ioi", timeMode = "fixed", startAt = t0, endAt = at(180), regMode = "free", regEndAt = t0,
        publicScoreboard = true, hideAdmin = true,
    )

    @Test
    fun fixedContest() {
        val c = fixed()
        val approved = Member("approved")
        val before = at(-60)
        val during = at(60)
        val after = at(240)

        var a = Access.resolve(c, 5, approved, false, before)
        assertFalse(a.canViewProblems || a.canSubmit || a.canViewScoreboard)
        assertEquals(Scope.NONE, a.submissions)
        assertTrue(a.canWithdraw)

        a = Access.resolve(c, 5, approved, false, during)
        assertTrue(a.canViewProblems && a.canSubmit && a.canViewScoreboard && a.restrictResults)
        assertEquals(Scope.OWN, a.submissions)
        assertTrue(a.canViewSubmission(5, false))
        assertFalse(a.canViewSubmission(6, false), "participants only see their own submissions while running")

        a = Access.resolve(c, 5, approved, false, after)
        assertFalse(a.canSubmit)
        assertTrue(a.canViewProblems)
        assertEquals(Scope.PARTICIPANTS, a.submissions)
        assertFalse(a.canViewSubmission(9, true))

        val guest = Access.resolve(c, 0, null, false, during)
        assertFalse(guest.canViewProblems || guest.canSubmit || guest.canRegister)
        assertEquals(Scope.NONE, guest.submissions)
        assertTrue(guest.canViewScoreboard, "a public scoreboard is visible to guests once started")

        for (status in listOf("requested", "rejected")) {
            a = Access.resolve(c, 7, Member(status), false, during)
            assertFalse(a.isMember || a.canViewProblems || a.canSubmit, "$status account must not get in")
        }
        val admin = Access.resolve(c, 2, Member("admin"), false, before)
        assertTrue(admin.isAdmin && admin.canViewProblems && admin.canSubmit)
        assertEquals(Scope.ALL, admin.submissions)
        assertFalse(admin.restrictResults)
        assertTrue(Access.resolve(c, 3, null, true, before).isAdmin, "site admins administer every contest")
    }

    @Test
    fun privateScoreboardAndRegistration() {
        val c = fixed().copy(publicScoreboard = false)
        assertFalse(Access.resolve(c, 0, null, false, at(60)).canViewScoreboard)
        assertEquals(Scope.OWN, Access.resolve(c, 5, Member("approved"), false, at(240)).submissions)
        assertTrue(Access.resolve(c, 8, null, false, at(-1)).canRegister)
        assertFalse(Access.resolve(c, 8, null, false, at(1)).canRegister, "registration closes at reg_end")
        assertFalse(Access.resolve(c.copy(regMode = "invited"), 8, null, false, at(-1)).canRegister)
    }

    @Test
    fun flexibleContest() {
        val c = fixed().copy(timeMode = "flexible", durationSec = 3600, endAt = at(24 * 60))
        var a = Access.resolve(c, 5, Member("approved"), false, at(120))
        assertTrue(a.canStart)
        assertFalse(a.canViewProblems || a.canSubmit || a.canViewScoreboard)

        val started = Member("approved", at(90), at(150))
        a = Access.resolve(c, 5, started, false, at(120))
        assertFalse(a.canStart)
        assertTrue(a.canViewProblems && a.canSubmit)
        assertEquals(Phase.RUNNING, a.phase)

        a = Access.resolve(c, 5, started, false, at(180))
        assertFalse(a.canSubmit || a.canViewProblems, "own session over while the window is open must hide the set")
        a = Access.resolve(c, 5, started, false, at(25 * 60))
        assertTrue(a.canViewProblems)
        assertFalse(a.canSubmit)
    }

    @Test
    fun scoreboardIoi() {
        val users = listOf(BoardUser(1, "a", t0, at(60)), BoardUser(2, "b", t0, at(60)), BoardUser(3, "c", t0, at(60)))
        val problems = listOf(BoardProblem(10, "p", "best"), BoardProblem(11, "q", "subtask_union"))
        fun st(a: Double, b: Double) = listOf(SubtaskResult(1, score = a), SubtaskResult(2, score = b))
        val subs = listOf(
            BoardSubmission(1, 1, 10, at(1), State.PC, 40.0),
            BoardSubmission(2, 1, 10, at(2), State.PC, 30.0),
            BoardSubmission(3, 1, 11, at(3), State.PC, 30.0, st(30.0, 0.0)),
            BoardSubmission(4, 1, 11, at(4), State.PC, 70.0, st(0.0, 70.0)),
            BoardSubmission(5, 2, 10, at(5), State.AC, 100.0),
            BoardSubmission(6, 2, 10, at(120), State.AC, 100.0), // outside the window
            BoardSubmission(7, 9, 10, at(5), State.AC, 100.0), // not a contestant
            BoardSubmission(8, 3, 10, at(5), State.PENDING, 0.0), // still queued
            BoardSubmission(9, 3, 99, at(5), State.AC, 100.0), // not a contest problem
        )
        val rows = Scoreboard.compute("ioi", 20, 0, problems, users, subs)
        assertEquals(1 to 140.0, rows[0].accountId to rows[0].total)
        assertEquals(100.0, rows[0].cells.getValue(11).score)
        assertEquals(Triple(2, 100.0, 2), Triple(rows[1].accountId, rows[1].total, rows[1].rank))
        assertEquals(1, rows[1].cells.getValue(10).attempts)
        assertEquals(0.0, rows[2].total)
        assertEquals(1, rows[2].cells.getValue(10).pending)
        assertNull(rows[2].cells[99])
    }

    @Test
    fun scoreboardAcmAndFreeze() {
        val users = listOf(BoardUser(1, "a", t0, at(60)), BoardUser(2, "b", t0, at(60)))
        val problems = listOf(BoardProblem(10, "p", "best"))
        val subs = listOf(
            BoardSubmission(1, 1, 10, at(5), State.WA, 0.0),
            BoardSubmission(2, 1, 10, at(6), State.CE, 0.0), // free
            BoardSubmission(3, 1, 10, at(10), State.AC, 100.0),
            BoardSubmission(4, 1, 10, at(11), State.WA, 0.0), // after AC: ignored
            BoardSubmission(5, 2, 10, at(50), State.AC, 100.0),
        )
        var rows = Scoreboard.compute("acm", 20, 0, problems, users, subs)
        assertEquals(Triple(1, 1, 30), Triple(rows[0].accountId, rows[0].solved, rows[0].penalty))
        assertEquals(50 to 2, rows[1].penalty to rows[1].rank)
        // With the last 15 minutes frozen, b's accepted run is hidden.
        rows = Scoreboard.compute("acm", 20, 15, problems, users, subs)
        assertEquals(0, rows[1].solved)
        assertEquals(1, rows[1].cells.getValue(10).frozen)
    }
}
