package contest

import (
	"testing"
	"time"

	"github.com/kuohuanhuan/ntoj/backend/internal/score"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func fixed() *Contest {
	return &Contest{ID: 1, Mode: "ioi", TimeMode: "fixed", StartAt: t0, EndAt: t0.Add(3 * time.Hour),
		RegMode: "free", RegEndAt: t0, PublicScoreboard: true, HideAdmin: true}
}

func TestAccessFixedContest(t *testing.T) {
	c := fixed()
	approved := &Member{Status: "approved"}
	during, after, before := t0.Add(time.Hour), t0.Add(4*time.Hour), t0.Add(-time.Hour)

	a := Resolve(c, 5, approved, false, before)
	if a.CanViewProblems || a.CanSubmit || a.CanViewScoreboard || a.Submissions != ScopeNone || !a.CanWithdraw {
		t.Fatalf("participant before start: %+v", a)
	}
	a = Resolve(c, 5, approved, false, during)
	if !a.CanViewProblems || !a.CanSubmit || !a.CanViewScoreboard || a.Submissions != ScopeOwn || !a.RestrictResults {
		t.Fatalf("participant during: %+v", a)
	}
	if !a.CanViewSubmission(5, false) || a.CanViewSubmission(6, false) {
		t.Fatal("participants must only see their own submissions while running")
	}
	a = Resolve(c, 5, approved, false, after)
	if a.CanSubmit || !a.CanViewProblems || a.Submissions != ScopeParticipants || a.CanViewSubmission(9, true) {
		t.Fatalf("participant after: %+v", a)
	}

	guest := Resolve(c, 0, nil, false, during)
	if guest.CanViewProblems || guest.CanSubmit || guest.CanRegister || guest.Submissions != ScopeNone {
		t.Fatalf("guest during: %+v", guest)
	}
	if !guest.CanViewScoreboard {
		t.Fatal("public scoreboard should be visible to guests once started")
	}
	for _, status := range []string{"requested", "rejected"} {
		a = Resolve(c, 7, &Member{Status: status}, false, during)
		if a.IsMember || a.CanViewProblems || a.CanSubmit {
			t.Fatalf("%s account must not get in: %+v", status, a)
		}
	}
	admin := Resolve(c, 2, &Member{Status: "admin"}, false, before)
	if !admin.IsAdmin || !admin.CanViewProblems || !admin.CanSubmit || admin.Submissions != ScopeAll || admin.RestrictResults {
		t.Fatalf("contest admin: %+v", admin)
	}
	if site := Resolve(c, 3, nil, true, before); !site.IsAdmin {
		t.Fatal("site admins administer every contest")
	}
}

func TestAccessPrivateScoreboardAndRegistration(t *testing.T) {
	c := fixed()
	c.PublicScoreboard = false
	if Resolve(c, 0, nil, false, t0.Add(time.Hour)).CanViewScoreboard {
		t.Fatal("private scoreboard leaked to guest")
	}
	a := Resolve(c, 5, &Member{Status: "approved"}, false, t0.Add(4*time.Hour))
	if a.Submissions != ScopeOwn {
		t.Fatalf("private contest must keep submissions private after the end: %v", a.Submissions)
	}
	if !Resolve(c, 8, nil, false, t0.Add(-time.Minute)).CanRegister {
		t.Fatal("registration should be open before reg_end")
	}
	if Resolve(c, 8, nil, false, t0.Add(time.Minute)).CanRegister {
		t.Fatal("registration should close at reg_end")
	}
	c.RegMode = "invited"
	if Resolve(c, 8, nil, false, t0.Add(-time.Minute)).CanRegister {
		t.Fatal("invite-only contests do not accept registrations")
	}
}

func TestAccessFlexibleContest(t *testing.T) {
	c := fixed()
	c.TimeMode, c.DurationSec, c.EndAt = "flexible", 3600, t0.Add(24*time.Hour)
	now := t0.Add(2 * time.Hour)

	a := Resolve(c, 5, &Member{Status: "approved"}, false, now)
	if !a.CanStart || a.CanViewProblems || a.CanSubmit || a.CanViewScoreboard {
		t.Fatalf("not started: %+v", a)
	}
	start, end := t0.Add(90*time.Minute), t0.Add(150*time.Minute)
	started := &Member{Status: "approved", SessionStart: &start, SessionEnd: &end}
	a = Resolve(c, 5, started, false, now)
	if a.CanStart || !a.CanViewProblems || !a.CanSubmit || a.Phase != Running {
		t.Fatalf("in session: %+v", a)
	}
	a = Resolve(c, 5, started, false, t0.Add(3*time.Hour))
	if a.CanSubmit || a.CanViewProblems {
		t.Fatalf("own session over while the window is open must hide the set: %+v", a)
	}
	a = Resolve(c, 5, started, false, t0.Add(25*time.Hour))
	if !a.CanViewProblems || a.CanSubmit {
		t.Fatalf("after the global end: %+v", a)
	}
}

func TestScoreboardIOI(t *testing.T) {
	users := []BoardUser{{1, "a", t0, t0.Add(time.Hour)}, {2, "b", t0, t0.Add(time.Hour)}, {3, "c", t0, t0.Add(time.Hour)}}
	problems := []BoardProblem{{10, "p", "best"}, {11, "q", "subtask_union"}}
	st := func(a, b float64) []score.SubtaskResult {
		return []score.SubtaskResult{{Idx: 1, Score: a}, {Idx: 2, Score: b}}
	}
	subs := []BoardSubmission{
		{1, 1, 10, t0.Add(time.Minute), score.PC, 40, nil},
		{2, 1, 10, t0.Add(2 * time.Minute), score.PC, 30, nil},
		{3, 1, 11, t0.Add(3 * time.Minute), score.PC, 30, st(30, 0)},
		{4, 1, 11, t0.Add(4 * time.Minute), score.PC, 70, st(0, 70)},
		{5, 2, 10, t0.Add(5 * time.Minute), score.AC, 100, nil},
		{6, 2, 10, t0.Add(2 * time.Hour), score.AC, 100, nil},      // outside the window
		{7, 9, 10, t0.Add(5 * time.Minute), score.AC, 100, nil},    // not a contestant
		{8, 3, 10, t0.Add(5 * time.Minute), score.Pending, 0, nil}, // still queued
		{9, 3, 99, t0.Add(5 * time.Minute), score.AC, 100, nil},    // not a contest problem
	}
	rows := Scoreboard(BoardOptions{Mode: "ioi"}, problems, users, subs)
	if rows[0].AccountID != 1 || rows[0].Total != 140 || rows[0].Cells[11].Score != 100 {
		t.Fatalf("first: %+v", rows[0])
	}
	if rows[1].AccountID != 2 || rows[1].Total != 100 || rows[1].Rank != 2 || rows[1].Cells[10].Attempts != 1 {
		t.Fatalf("second: %+v", rows[1])
	}
	if rows[2].Total != 0 || rows[2].Cells[10].Pending != 1 || rows[2].Cells[99] != nil {
		t.Fatalf("third: %+v", rows[2])
	}
}

func TestScoreboardACMAndFreeze(t *testing.T) {
	users := []BoardUser{{1, "a", t0, t0.Add(time.Hour)}, {2, "b", t0, t0.Add(time.Hour)}}
	problems := []BoardProblem{{10, "p", "best"}}
	subs := []BoardSubmission{
		{1, 1, 10, t0.Add(5 * time.Minute), score.WA, 0, nil},
		{2, 1, 10, t0.Add(6 * time.Minute), score.CE, 0, nil}, // free
		{3, 1, 10, t0.Add(10 * time.Minute), score.AC, 100, nil},
		{4, 1, 10, t0.Add(11 * time.Minute), score.WA, 0, nil}, // after AC: ignored
		{5, 2, 10, t0.Add(50 * time.Minute), score.AC, 100, nil},
	}
	rows := Scoreboard(BoardOptions{Mode: "acm", PenaltyMinutes: 20}, problems, users, subs)
	if rows[0].AccountID != 1 || rows[0].Solved != 1 || rows[0].Penalty != 30 {
		t.Fatalf("acm first: %+v", rows[0])
	}
	if rows[1].Penalty != 50 || rows[1].Rank != 2 {
		t.Fatalf("acm second: %+v", rows[1])
	}
	// With the last 15 minutes frozen, b's accepted run is hidden.
	rows = Scoreboard(BoardOptions{Mode: "acm", PenaltyMinutes: 20, FreezeMinutes: 15}, problems, users, subs)
	if rows[1].Solved != 0 || rows[1].Cells[10].Frozen != 1 {
		t.Fatalf("freeze: %+v", rows[1])
	}
}
