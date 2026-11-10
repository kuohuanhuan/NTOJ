package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/kuohuanhuan/ntoj/backend/internal/contest"
	"github.com/kuohuanhuan/ntoj/backend/internal/score"
)

type contestRow struct {
	contest.Contest
	DescBefore string `json:"desc_before"`
	DescDuring string `json:"desc_during"`
	DescAfter  string `json:"desc_after"`
}

func (s *Server) loadContest(c *Ctx, id int) (*contestRow, error) {
	raw, err := s.jsonRow(c, `SELECT * FROM contests WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	var row contestRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// loadAccess resolves the caller's relation to a contest.
func (s *Server) loadAccess(c *Ctx, id int) (*contestRow, contest.Access, error) {
	row, err := s.loadContest(c, id)
	if err != nil {
		return nil, contest.Access{}, err
	}
	var member *contest.Member
	if c.acct != nil {
		var m contest.Member
		err := s.db.QueryRow(c, `SELECT status, session_start, session_end FROM contest_users
			WHERE contest_id = $1 AND account_id = $2`, id, c.acct.ID).Scan(&m.Status, &m.SessionStart, &m.SessionEnd)
		if err == nil {
			member = &m
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, contest.Access{}, err
		}
	}
	return row, contest.Resolve(&row.Contest, c.acctID(), member, c.acct.Admin(), s.now()), nil
}

// contestAdmin loads a contest and requires the caller to administer it.
// Outsiders get 404 so that management URLs do not confirm anything.
func (s *Server) contestAdmin(c *Ctx) (*contestRow, error) {
	id, err := c.pathInt("id")
	if err != nil {
		return nil, err
	}
	row, access, err := s.loadAccess(c, id)
	if err != nil {
		return nil, err
	}
	if !access.IsAdmin {
		return nil, errForbidden
	}
	return row, nil
}

func (s *Server) listContests(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT c.id, c.name, c.mode, c.start_at, c.end_at, c.time_mode, c.duration_sec,
			c.reg_mode, c.reg_end_at, c.public_scoreboard, cu.status AS my_status,
			(SELECT count(*) FROM contest_users u WHERE u.contest_id = c.id AND u.status = 'approved') AS participants
		FROM contests c LEFT JOIN contest_users cu ON cu.contest_id = c.id AND cu.account_id = $1
		ORDER BY c.start_at DESC, c.id DESC`, c.acctID())
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) createContest(c *Ctx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Name = cleanText(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 64 {
		return badRequest("Contest name must be 1 to 64 characters")
	}
	start := s.now().Add(24 * time.Hour).Truncate(time.Hour)
	var id int
	err := pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(c, `INSERT INTO contests (name, creator_id, start_at, end_at, reg_end_at)
			VALUES ($1, $2, $3, $4, $3) RETURNING id`, in.Name, c.acct.ID, start, start.Add(3*time.Hour)).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(c, `INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, 'admin')`, id, c.acct.ID)
		return err
	})
	if err != nil {
		return err
	}
	c.audit("contest.create", "Contest created", map[string]any{"name": in.Name}, &id)
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) getContest(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	row, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	desc := row.DescBefore
	switch access.Phase {
	case contest.Running:
		desc = row.DescDuring
	case contest.Ended:
		desc = row.DescAfter
	}
	if !access.Activated && access.Global != contest.Ended {
		desc = row.DescBefore
	}
	var participants, unread int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM contest_users WHERE contest_id = $1 AND status = 'approved'`, id).Scan(&participants); err != nil {
		return err
	}
	if access.IsAdmin {
		err = s.db.QueryRow(c, `SELECT count(*) FROM contest_questions WHERE contest_id = $1 AND reply IS NULL`, id).Scan(&unread)
	} else if access.IsMember {
		err = s.db.QueryRow(c, `SELECT
			(SELECT count(*) FROM contest_announcements n WHERE n.contest_id = cu.contest_id AND n.updated_at > cu.read_at) +
			(SELECT count(*) FROM contest_questions q WHERE q.contest_id = cu.contest_id AND q.asker_id = cu.account_id AND q.replied_at > cu.read_at)
			FROM contest_users cu WHERE cu.contest_id = $1 AND cu.account_id = $2`, id, c.acct.ID).Scan(&unread)
	}
	if err != nil {
		return err
	}
	return c.ok(map[string]any{
		"contest": row.Contest, "description": desc, "access": access,
		"participants": participants, "unread": unread, "now": s.now(),
	})
}

func (s *Server) updateContest(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	var in struct {
		Name             *string    `json:"name"`
		DescBefore       *string    `json:"desc_before"`
		DescDuring       *string    `json:"desc_during"`
		DescAfter        *string    `json:"desc_after"`
		Mode             *string    `json:"mode"`
		StartAt          *time.Time `json:"start_at"`
		EndAt            *time.Time `json:"end_at"`
		TimeMode         *string    `json:"time_mode"`
		DurationSec      *int       `json:"duration_sec"`
		RegMode          *string    `json:"reg_mode"`
		RegEndAt         *time.Time `json:"reg_end_at"`
		Languages        *[]string  `json:"languages"`
		PublicScoreboard *bool      `json:"public_scoreboard"`
		AllowOtherPages  *bool      `json:"allow_other_pages"`
		HideAdmin        *bool      `json:"hide_admin"`
		CooldownSec      *int       `json:"cooldown_sec"`
		FreezeMinutes    *int       `json:"freeze_minutes"`
		KeepFrozen       *bool      `json:"keep_frozen"`
		PenaltyMinutes   *int       `json:"penalty_minutes"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	set(&row.Name, in.Name)
	set(&row.DescBefore, in.DescBefore)
	set(&row.DescDuring, in.DescDuring)
	set(&row.DescAfter, in.DescAfter)
	set(&row.Mode, in.Mode)
	set(&row.StartAt, in.StartAt)
	set(&row.EndAt, in.EndAt)
	set(&row.TimeMode, in.TimeMode)
	set(&row.DurationSec, in.DurationSec)
	set(&row.RegMode, in.RegMode)
	set(&row.RegEndAt, in.RegEndAt)
	set(&row.PublicScoreboard, in.PublicScoreboard)
	set(&row.AllowOtherPages, in.AllowOtherPages)
	set(&row.HideAdmin, in.HideAdmin)
	set(&row.CooldownSec, in.CooldownSec)
	set(&row.FreezeMinutes, in.FreezeMinutes)
	set(&row.KeepFrozen, in.KeepFrozen)
	set(&row.PenaltyMinutes, in.PenaltyMinutes)
	if in.Languages != nil {
		if row.Languages, err = cleanLanguages(*in.Languages); err != nil {
			return err
		}
	}

	row.Name = cleanText(row.Name)
	window := int(row.EndAt.Sub(row.StartAt) / time.Second)
	switch {
	case utf8.RuneCountInString(row.Name) < 1 || utf8.RuneCountInString(row.Name) > 64:
		return badRequest("Contest name must be 1 to 64 characters")
	case len(row.DescBefore) > maxStatement || len(row.DescDuring) > maxStatement || len(row.DescAfter) > maxStatement:
		return badRequest("Description is too long")
	case !oneOf(row.Mode, "ioi", "acm"):
		return badRequest("Invalid contest mode")
	case !oneOf(row.TimeMode, "fixed", "flexible"):
		return badRequest("Invalid time mode")
	case !oneOf(row.RegMode, "invited", "free", "approval"):
		return badRequest("Invalid registration mode")
	case !row.EndAt.After(row.StartAt):
		return badRequest("The contest must end after it starts")
	case row.TimeMode == "flexible" && (row.DurationSec < 60 || row.DurationSec > window):
		return badRequest("A flexible contest needs a duration between one minute and the length of its window")
	case row.CooldownSec < 0 || row.CooldownSec > 86400:
		return badRequest("Invalid submission cooldown")
	case row.FreezeMinutes < 0 || row.FreezeMinutes > window/60:
		return badRequest("The freeze period cannot be longer than the contest")
	case row.PenaltyMinutes < 0 || row.PenaltyMinutes > 10000:
		return badRequest("Invalid penalty")
	}
	if row.TimeMode == "fixed" {
		row.DurationSec = 0
	}
	_, err = s.db.Exec(c, `UPDATE contests SET name = $2, desc_before = $3, desc_during = $4, desc_after = $5, mode = $6,
			start_at = $7, end_at = $8, time_mode = $9, duration_sec = $10, reg_mode = $11, reg_end_at = $12, languages = $13,
			public_scoreboard = $14, allow_other_pages = $15, hide_admin = $16, cooldown_sec = $17, freeze_minutes = $18,
			keep_frozen = $19, penalty_minutes = $20 WHERE id = $1`,
		row.ID, row.Name, row.DescBefore, row.DescDuring, row.DescAfter, row.Mode, row.StartAt, row.EndAt, row.TimeMode,
		row.DurationSec, row.RegMode, row.RegEndAt, row.Languages, row.PublicScoreboard, row.AllowOtherPages, row.HideAdmin,
		row.CooldownSec, row.FreezeMinutes, row.KeepFrozen, row.PenaltyMinutes)
	if err != nil {
		return err
	}
	in.DescBefore, in.DescDuring, in.DescAfter = nil, nil, nil
	c.audit("contest.update", "Contest settings updated", map[string]any{"changes": in}, &row.ID)
	return c.noContent()
}

func set[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func (s *Server) deleteContest(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var name string
	err = s.db.QueryRow(c, `DELETE FROM contests WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	c.audit("contest.delete", "Contest deleted", map[string]any{"contest_id": id, "name": name}, nil)
	return c.noContent()
}

func (s *Server) registerContest(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	row, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanRegister {
		return &Error{Status: 403, Code: "registration_closed", Message: "Registration is not open to you"}
	}
	status := "requested"
	if row.RegMode == "free" {
		status = "approved"
	}
	if _, err := s.db.Exec(c, `INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, id, c.acct.ID, status); err != nil {
		return err
	}
	c.audit("contest.register", "Registered for contest", map[string]any{"status": status}, &id)
	return c.ok(map[string]any{"status": status})
}

func (s *Server) withdrawContest(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	_, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanWithdraw {
		return &Error{Status: 403, Code: "registration_closed", Message: "You can no longer withdraw from this contest"}
	}
	if _, err := s.db.Exec(c, `DELETE FROM contest_users WHERE contest_id = $1 AND account_id = $2
		AND status IN ('requested', 'approved')`, id, c.acct.ID); err != nil {
		return err
	}
	c.audit("contest.withdraw", "Withdrew from contest", nil, &id)
	return c.noContent()
}

// startContestSession opens the caller's personal window in a flexible
// contest. The single UPDATE makes it atomic and idempotent.
func (s *Server) startContestSession(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	if _, access, err := s.loadAccess(c, id); err != nil {
		return err
	} else if !access.CanStart {
		return &Error{Status: 403, Code: "cannot_start", Message: "You cannot start this contest now"}
	}
	var start, end time.Time
	err = s.db.QueryRow(c, `UPDATE contest_users cu SET session_start = now(),
			session_end = least(now() + make_interval(secs => ct.duration_sec), ct.end_at)
		FROM contests ct
		WHERE ct.id = cu.contest_id AND cu.contest_id = $1 AND cu.account_id = $2 AND cu.status = 'approved'
		  AND cu.session_start IS NULL AND ct.time_mode = 'flexible' AND now() >= ct.start_at AND now() < ct.end_at
		RETURNING cu.session_start, cu.session_end`, id, c.acct.ID).Scan(&start, &end)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("Your contest session has already started")
	}
	if err != nil {
		return err
	}
	c.audit("contest.start", "Started flexible contest session", nil, &id)
	return c.ok(map[string]any{"start": start, "end": end})
}

func (s *Server) listContestProblems(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	_, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanViewProblems {
		return errForbidden
	}
	out, err := s.jsonList(c, `SELECT p.id, p.name, p.status, p.allow_submit, cp.score_type, cp.result_style,
			my.state AS my_state, my.score AS my_score
		FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
		LEFT JOIN LATERAL (SELECT min(state) AS state, max(score) AS score FROM submissions
			WHERE contest_id = $1 AND problem_id = p.id AND account_id = $2 AND state < 100) my ON true
		WHERE cp.contest_id = $1 AND ($3 OR p.status <> 'hidden') ORDER BY cp.ord`, id, c.acctID(), access.IsAdmin)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) getContestProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	pid, err := c.pathInt("pid")
	if err != nil {
		return err
	}
	row, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanViewProblems {
		return errForbidden
	}
	var style string
	err = s.db.QueryRow(c, `SELECT result_style FROM contest_problems WHERE contest_id = $1 AND problem_id = $2`, id, pid).Scan(&style)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	out, err := s.problemJSON(c, pid)
	if err != nil {
		return err
	}
	if out["status"] == "hidden" && !access.IsAdmin {
		return errNotFound
	}
	if len(row.Languages) > 0 {
		allowed := []string{}
		problemLangs, _ := out["languages"].([]any)
		for _, l := range row.Languages {
			ok := len(problemLangs) == 0
			for _, pl := range problemLangs {
				ok = ok || pl == l
			}
			if ok {
				allowed = append(allowed, l)
			}
		}
		out["languages"] = allowed
	}
	out["result_style"] = style
	out["can_submit"] = access.CanSubmit
	return c.ok(out)
}

func (s *Server) contestScoreboard(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	row, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanViewScoreboard {
		return errForbidden
	}
	rows, err := s.db.Query(c, `SELECT p.id, p.name, cp.score_type FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
		WHERE cp.contest_id = $1 AND p.status <> 'hidden' ORDER BY cp.ord`, id)
	if err != nil {
		return err
	}
	problems, err := pgx.CollectRows(rows, pgx.RowToStructByPos[contest.BoardProblem])
	if err != nil {
		return err
	}
	// Contestants compete in their own window; administrators are listed only
	// when the contest does not hide them.
	rows, err = s.db.Query(c, `SELECT a.id, a.name,
			CASE WHEN ct.time_mode = 'flexible' AND cu.status = 'approved' THEN cu.session_start ELSE ct.start_at END,
			CASE WHEN ct.time_mode = 'flexible' AND cu.status = 'approved' THEN cu.session_end ELSE ct.end_at END
		FROM contest_users cu JOIN accounts a ON a.id = cu.account_id JOIN contests ct ON ct.id = cu.contest_id
		WHERE cu.contest_id = $1 AND (cu.status = 'approved' OR (cu.status = 'admin' AND NOT ct.hide_admin))
		  AND (ct.time_mode = 'fixed' OR cu.status = 'admin' OR cu.session_start IS NOT NULL)
		ORDER BY a.id`, id)
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByPos[contest.BoardUser])
	if err != nil {
		return err
	}
	rows, err = s.db.Query(c, `SELECT id, account_id, problem_id, created_at, state, score::float8, result->'subtasks'
		FROM submissions WHERE contest_id = $1 ORDER BY id`, id)
	if err != nil {
		return err
	}
	subs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (contest.BoardSubmission, error) {
		var b contest.BoardSubmission
		var subtasks []byte
		if err := r.Scan(&b.ID, &b.AccountID, &b.ProblemID, &b.At, &b.State, &b.Score, &subtasks); err != nil {
			return b, err
		}
		if len(subtasks) > 0 {
			json.Unmarshal(subtasks, &b.Subtasks)
		}
		return b, nil
	})
	if err != nil {
		return err
	}
	opt := contest.BoardOptions{Mode: row.Mode, PenaltyMinutes: row.PenaltyMinutes}
	frozen := row.FreezeMinutes > 0 && !access.IsAdmin && (s.now().Before(row.EndAt) || row.KeepFrozen)
	if frozen {
		opt.FreezeMinutes = row.FreezeMinutes
	}
	return c.ok(map[string]any{
		"mode": row.Mode, "problems": problems, "frozen": frozen,
		"rows": contest.Scoreboard(opt, problems, users, subs),
	})
}

// ---- Announcements and questions ----

func (s *Server) contestQA(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	_, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	announcements, err := s.jsonList(c, `SELECT n.id, n.subject, n.content, n.created_at, n.updated_at
		FROM contest_announcements n WHERE n.contest_id = $1 ORDER BY n.id DESC`, id)
	if err != nil {
		return err
	}
	questions, err := s.jsonList(c, `SELECT q.id, q.asker_id, a.name AS asker_name, q.subject, q.content, q.reply, q.created_at, q.replied_at
		FROM contest_questions q JOIN accounts a ON a.id = q.asker_id
		WHERE q.contest_id = $1 AND ($3 OR q.asker_id = $2) ORDER BY q.id DESC`, id, c.acctID(), access.IsAdmin)
	if err != nil {
		return err
	}
	if c.acct != nil {
		if _, err := s.db.Exec(c, `UPDATE contest_users SET read_at = now() WHERE contest_id = $1 AND account_id = $2`, id, c.acct.ID); err != nil {
			return err
		}
	}
	return c.ok(map[string]any{"announcements": announcements, "questions": questions, "can_ask": access.CanAsk, "is_admin": access.IsAdmin})
}

func validPost(subject, content string) error {
	if n := utf8.RuneCountInString(subject); n < 1 || n > 100 {
		return badRequest("Subject must be 1 to 100 characters")
	}
	if n := utf8.RuneCountInString(content); n < 1 || n > 4000 {
		return badRequest("Content must be 1 to 4000 characters")
	}
	return nil
}

func (s *Server) askContestQuestion(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	_, access, err := s.loadAccess(c, id)
	if err != nil {
		return err
	}
	if !access.CanAsk {
		return errForbidden
	}
	var in struct{ Subject, Content string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Subject, in.Content = cleanText(in.Subject), cleanText(in.Content)
	if err := validPost(in.Subject, in.Content); err != nil {
		return err
	}
	if !s.limiter.allow("contest-question:"+c.r.PathValue("id")+":"+c.ip, 10, 10*time.Minute) {
		return errTooMany
	}
	if _, err := s.db.Exec(c, `INSERT INTO contest_questions (contest_id, asker_id, subject, content) VALUES ($1, $2, $3, $4)`,
		id, c.acct.ID, in.Subject, in.Content); err != nil {
		return err
	}
	return c.json(http.StatusCreated, map[string]any{})
}

func (s *Server) replyContestQuestion(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	qid, err := c.pathInt("qid")
	if err != nil {
		return err
	}
	var in struct{ Reply string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Reply = cleanText(in.Reply)
	if n := utf8.RuneCountInString(in.Reply); n < 1 || n > 4000 {
		return badRequest("Reply must be 1 to 4000 characters")
	}
	tag, err := s.db.Exec(c, `UPDATE contest_questions SET reply = $3, replier_id = $4, replied_at = now()
		WHERE id = $1 AND contest_id = $2`, qid, row.ID, in.Reply, c.acct.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return c.noContent()
}

func (s *Server) createAnnouncement(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	var in struct{ Subject, Content string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Subject, in.Content = cleanText(in.Subject), cleanText(in.Content)
	if err := validPost(in.Subject, in.Content); err != nil {
		return err
	}
	var nid int
	if err := s.db.QueryRow(c, `INSERT INTO contest_announcements (contest_id, author_id, subject, content)
		VALUES ($1, $2, $3, $4) RETURNING id`, row.ID, c.acct.ID, in.Subject, in.Content).Scan(&nid); err != nil {
		return err
	}
	c.audit("contest.announce", "Announcement published", map[string]any{"subject": in.Subject}, &row.ID)
	return c.json(http.StatusCreated, map[string]any{"id": nid})
}

func (s *Server) updateAnnouncement(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	nid, err := c.pathInt("nid")
	if err != nil {
		return err
	}
	var in struct{ Subject, Content string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Subject, in.Content = cleanText(in.Subject), cleanText(in.Content)
	if err := validPost(in.Subject, in.Content); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE contest_announcements SET subject = $3, content = $4, updated_at = now()
		WHERE id = $1 AND contest_id = $2`, nid, row.ID, in.Subject, in.Content)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return c.noContent()
}

func (s *Server) deleteAnnouncement(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	nid, err := c.pathInt("nid")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM contest_announcements WHERE id = $1 AND contest_id = $2`, nid, row.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return c.noContent()
}

// ---- Management ----

func (s *Server) manageContest(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	problems, err := s.jsonList(c, `SELECT p.id AS problem_id, p.name, p.status, cp.score_type, cp.result_style
		FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id WHERE cp.contest_id = $1 ORDER BY cp.ord`, row.ID)
	if err != nil {
		return err
	}
	users, err := s.jsonList(c, `SELECT cu.account_id, a.name, cu.status, cu.session_start, cu.session_end, cu.created_at
		FROM contest_users cu JOIN accounts a ON a.id = cu.account_id WHERE cu.contest_id = $1 ORDER BY cu.account_id`, row.ID)
	if err != nil {
		return err
	}
	return c.ok(map[string]any{"contest": row, "problems": problems, "users": users,
		"can_manage_admins": s.canManageAdmins(c, row)})
}

func (s *Server) canManageAdmins(c *Ctx, row *contestRow) bool {
	return c.acct.Admin() || (row.CreatorID != nil && *row.CreatorID == c.acct.ID)
}

func (s *Server) setContestProblems(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	var in []struct {
		ProblemID   int    `json:"problem_id"`
		ScoreType   string `json:"score_type"`
		ResultStyle string `json:"result_style"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if len(in) > 100 {
		return badRequest("At most 100 problems are allowed")
	}
	seen := map[int]bool{}
	for _, p := range in {
		if seen[p.ProblemID] {
			return badRequest("Problem %d is listed twice", p.ProblemID)
		}
		seen[p.ProblemID] = true
		if !oneOf(p.ScoreType, "best", "subtask_union") || !oneOf(p.ResultStyle, "full", "subtask", "total") {
			return badRequest("Problem %d: invalid options", p.ProblemID)
		}
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		for _, p := range in {
			// Contest managers who are not site administrators may only pull
			// in public problems, so they cannot read hidden statements.
			var allowed bool
			err := tx.QueryRow(c, `SELECT $3 OR p.status = 'online' OR EXISTS (
					SELECT 1 FROM contest_problems cp WHERE cp.contest_id = $2 AND cp.problem_id = p.id)
				FROM problems p WHERE p.id = $1`, p.ProblemID, row.ID, c.acct.Admin()).Scan(&allowed)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
				return badRequest("Problem %d does not exist or cannot be added", p.ProblemID)
			}
			if err != nil {
				return err
			}
		}
		keep := make([]int, 0, len(in))
		for _, p := range in {
			keep = append(keep, p.ProblemID)
		}
		if _, err := tx.Exec(c, `DELETE FROM contest_problems WHERE contest_id = $1 AND problem_id <> ALL ($2)`, row.ID, keep); err != nil {
			return err
		}
		for i, p := range in {
			if _, err := tx.Exec(c, `INSERT INTO contest_problems (contest_id, problem_id, ord, score_type, result_style)
				VALUES ($1, $2, $3, $4, $5) ON CONFLICT (contest_id, problem_id)
				DO UPDATE SET ord = EXCLUDED.ord, score_type = EXCLUDED.score_type, result_style = EXCLUDED.result_style`,
				row.ID, p.ProblemID, i, p.ScoreType, p.ResultStyle); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.audit("contest.problems", "Contest problems updated", map[string]any{"problems": in}, &row.ID)
	return c.noContent()
}

// guardAdminRow blocks ordinary contest managers from creating or touching
// other managers; only the creator or a site administrator may.
func (s *Server) guardAdminRow(c *Ctx, row *contestRow, accountID int, newStatus string) error {
	if s.canManageAdmins(c, row) {
		return nil
	}
	var current string
	err := s.db.QueryRow(c, `SELECT status FROM contest_users WHERE contest_id = $1 AND account_id = $2`, row.ID, accountID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if current == "admin" || newStatus == "admin" || (row.CreatorID != nil && *row.CreatorID == accountID) {
		return errForbidden
	}
	return nil
}

func (s *Server) setContestUser(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	aid, err := c.pathInt("aid")
	if err != nil {
		return err
	}
	var in struct{ Status string }
	if err := c.bind(&in); err != nil {
		return err
	}
	if !oneOf(in.Status, "approved", "rejected", "admin") {
		return badRequest("Invalid status")
	}
	if err := s.guardAdminRow(c, row, aid, in.Status); err != nil {
		return err
	}
	_, err = s.db.Exec(c, `INSERT INTO contest_users (contest_id, account_id, status) VALUES ($1, $2, $3)
		ON CONFLICT (contest_id, account_id) DO UPDATE SET status = EXCLUDED.status`, row.ID, aid, in.Status)
	if isForeignKey(err) {
		return badRequest("Account %d does not exist", aid)
	}
	if err != nil {
		return err
	}
	c.audit("contest.user", "Contest member updated", map[string]any{"account_id": aid, "status": in.Status}, &row.ID)
	return c.noContent()
}

func (s *Server) removeContestUser(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	aid, err := c.pathInt("aid")
	if err != nil {
		return err
	}
	if err := s.guardAdminRow(c, row, aid, ""); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM contest_users WHERE contest_id = $1 AND account_id = $2`, row.ID, aid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("contest.user", "Contest member removed", map[string]any{"account_id": aid}, &row.ID)
	return c.noContent()
}

func (s *Server) resetContestSession(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	aid, err := c.pathInt("aid")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE contest_users SET session_start = NULL, session_end = NULL
		WHERE contest_id = $1 AND account_id = $2`, row.ID, aid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("contest.user", "Flexible session reset", map[string]any{"account_id": aid}, &row.ID)
	return c.noContent()
}

func (s *Server) rejudgeContest(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	var in struct {
		ProblemID *int `json:"problem_id"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE submissions SET `+resetSubmission+`, priority = 2
		WHERE contest_id = $1 AND ($2::int IS NULL OR problem_id = $2)`, row.ID, in.ProblemID)
	if err != nil {
		return err
	}
	c.audit("contest.rejudge", "Contest submissions rejudged",
		map[string]any{"problem_id": in.ProblemID, "submissions": tag.RowsAffected()}, &row.ID)
	return c.ok(map[string]any{"submissions": tag.RowsAffected()})
}

func (s *Server) contestLogs(c *Ctx) error {
	row, err := s.contestAdmin(c)
	if err != nil {
		return err
	}
	return s.logPage(c, &row.ID)
}

var _ = score.AC
