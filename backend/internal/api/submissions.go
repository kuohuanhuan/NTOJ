package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/kuohuanhuan/ntoj/backend/internal/contest"
	"github.com/kuohuanhuan/ntoj/backend/internal/score"
)

const maxCodeSize = 64 << 10

// resetSubmission is the SET clause that sends a submission back to the queue.
const resetSubmission = `state = 101, score = 0, time_ms = 0, memory_kb = 0, message = '',
	result = '{"subtasks": [], "testcases": []}', claimed_at = NULL, judged_at = NULL`

func (s *Server) submit(c *Ctx) error {
	var in struct {
		ProblemID int    `json:"problem_id"`
		ContestID *int   `json:"contest_id"`
		Language  string `json:"language"`
		Code      string `json:"code"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if !validLanguage(in.Language) {
		return badRequest("Unknown language")
	}
	if strings.TrimSpace(in.Code) == "" {
		return badRequest("Code must not be empty")
	}
	if len(in.Code) > maxCodeSize || !utf8.ValidString(in.Code) || strings.ContainsRune(in.Code, 0) {
		return badRequest("Code must be valid UTF-8 text of at most %d KiB", maxCodeSize>>10)
	}

	var status string
	var allowSubmit bool
	var languages []string
	err := s.db.QueryRow(c, `SELECT status, allow_submit, languages FROM problems WHERE id = $1`, in.ProblemID).
		Scan(&status, &allowSubmit, &languages)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}

	cooldown, priority := s.cfg.SubmitCooldown, 0
	if in.ContestID != nil {
		ct, access, err := s.loadAccess(c, *in.ContestID)
		if err != nil {
			return err
		}
		var inContest bool
		if err := s.db.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM contest_problems WHERE contest_id = $1 AND problem_id = $2)`,
			ct.ID, in.ProblemID).Scan(&inContest); err != nil {
			return err
		}
		if !inContest || (status == "hidden" && !access.IsAdmin) {
			return errNotFound
		}
		if !access.CanSubmit {
			return &Error{Status: 403, Code: "contest_closed", Message: "You cannot submit to this contest right now"}
		}
		if len(ct.Languages) > 0 && !oneOf(in.Language, ct.Languages...) {
			return badRequest("This language is not allowed in the contest")
		}
		cooldown, priority = ct.CooldownSec, 1
		if access.IsAdmin {
			cooldown = 0
		}
	} else {
		if status != "online" && !c.acct.Admin() {
			return errNotFound
		}
		if err := c.checkLockdown(); err != nil {
			return err
		}
	}
	if !allowSubmit && !c.acct.Admin() {
		return &Error{Status: 403, Code: "submit_closed", Message: "This problem does not accept submissions"}
	}
	if len(languages) > 0 && !oneOf(in.Language, languages...) {
		return badRequest("This language is not allowed for the problem")
	}

	var id int64
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		// Serialise one account's submissions so the cooldown cannot be raced.
		if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(1, $1)`, c.acct.ID); err != nil {
			return err
		}
		if cooldown > 0 {
			var wait *float64
			if err := tx.QueryRow(c, `SELECT $3 - extract(epoch FROM now() - max(created_at))
				FROM submissions WHERE account_id = $1 AND contest_id IS NOT DISTINCT FROM $2`,
				c.acct.ID, in.ContestID, cooldown).Scan(&wait); err != nil {
				return err
			}
			if wait != nil && *wait > 0 {
				c.w.Header().Set("Retry-After", strconv.Itoa(int(*wait)+1))
				return &Error{Status: 429, Code: "cooldown", Extra: map[string]any{"retry_after": int(*wait) + 1},
					Message: fmt.Sprintf("Please wait %d more second(s) before submitting again", int(*wait)+1)}
			}
		}
		if err := tx.QueryRow(c, `INSERT INTO submissions (problem_id, account_id, contest_id, language, code, priority)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.ProblemID, c.acct.ID, in.ContestID, in.Language, in.Code, priority).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(c, `UPDATE accounts SET last_language = $2 WHERE id = $1`, c.acct.ID, in.Language)
		return err
	})
	if err != nil {
		return err
	}
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listSubmissions(c *Ctx) error {
	limit, offset, page := c.page(30)
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	where := "true"

	contestID, inContest, err := c.queryInt("contest_id")
	if err != nil {
		return err
	}
	if inContest {
		_, access, err := s.loadAccess(c, contestID)
		if err != nil {
			return err
		}
		where += " AND s.contest_id = " + arg(contestID)
		switch access.Submissions {
		case contest.ScopeNone:
			where += " AND false"
		case contest.ScopeOwn:
			where += " AND s.account_id = " + arg(c.acctID())
		case contest.ScopeParticipants:
			me := arg(c.acctID())
			where += ` AND (s.account_id = ` + me + ` OR s.account_id IN (SELECT account_id FROM contest_users
				WHERE contest_id = ` + arg(contestID) + ` AND status = 'approved'))`
		}
	} else {
		if err := c.checkLockdown(); err != nil {
			return err
		}
		where += " AND s.contest_id IS NULL"
		if !c.acct.Admin() {
			where += " AND p.status = 'online'"
		}
	}
	for _, f := range []struct{ param, column string }{
		{"problem_id", "s.problem_id"}, {"account_id", "s.account_id"}, {"state", "s.state"},
	} {
		v, ok, err := c.queryInt(f.param)
		if err != nil {
			return err
		}
		if ok && v != 0 {
			where += " AND " + f.column + " = " + arg(v)
		}
	}
	if lang := c.r.URL.Query().Get("language"); lang != "" {
		where += " AND s.language = " + arg(lang)
	}

	const from = ` FROM submissions s JOIN problems p ON p.id = s.problem_id JOIN accounts a ON a.id = s.account_id WHERE `
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return err
	}
	items, err := s.jsonList(c, `SELECT s.id, s.problem_id, p.name AS problem_name, s.account_id, a.name AS account_name,
		s.contest_id, s.language, s.created_at, s.state, s.score, s.time_ms, s.memory_kb, length(s.code) AS code_length`+
		from+where+` ORDER BY s.id DESC LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

type submissionResult struct {
	Subtasks  []score.SubtaskResult `json:"subtasks"`
	Testcases []score.CaseResult    `json:"testcases"`
}

type submissionView struct {
	ID          int64            `json:"id"`
	ProblemID   int              `json:"problem_id"`
	ProblemName string           `json:"problem_name"`
	AccountID   int              `json:"account_id"`
	AccountName string           `json:"account_name"`
	ContestID   *int             `json:"contest_id"`
	Language    string           `json:"language"`
	CreatedAt   time.Time        `json:"created_at"`
	JudgedAt    *time.Time       `json:"judged_at"`
	State       int              `json:"state"`
	Score       float64          `json:"score"`
	TimeMS      int64            `json:"time_ms"`
	MemoryKB    int64            `json:"memory_kb"`
	Message     string           `json:"message"`
	Code        *string          `json:"code"` // null when the viewer may not read it
	Result      submissionResult `json:"result"`
	Style       string           `json:"result_style"`
	CanRejudge  bool             `json:"can_rejudge"`
}

func (s *Server) getSubmission(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var v submissionView
	var code, status string
	var resultRaw []byte
	err = s.db.QueryRow(c, `SELECT s.id, s.problem_id, p.name, p.status, s.account_id, a.name, s.contest_id, s.language,
			s.created_at, s.judged_at, s.state, s.score, s.time_ms, s.memory_kb, s.message, s.code, s.result
		FROM submissions s JOIN problems p ON p.id = s.problem_id JOIN accounts a ON a.id = s.account_id
		WHERE s.id = $1`, id).Scan(&v.ID, &v.ProblemID, &v.ProblemName, &status, &v.AccountID, &v.AccountName, &v.ContestID,
		&v.Language, &v.CreatedAt, &v.JudgedAt, &v.State, &v.Score, &v.TimeMS, &v.MemoryKB, &v.Message, &code, &resultRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resultRaw, &v.Result); err != nil {
		return err
	}
	owner := c.acct != nil && c.acct.ID == v.AccountID
	privileged := c.acct.Admin()
	v.Style = "full"

	if v.ContestID != nil {
		_, access, err := s.loadAccess(c, *v.ContestID)
		if err != nil {
			return err
		}
		var ownerIsAdmin bool
		if err := s.db.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM contest_users WHERE contest_id = $1 AND account_id = $2
			AND status = 'admin')`, *v.ContestID, v.AccountID).Scan(&ownerIsAdmin); err != nil {
			return err
		}
		if !access.CanViewSubmission(v.AccountID, ownerIsAdmin) {
			return errNotFound
		}
		privileged = access.IsAdmin
		if access.RestrictResults {
			if err := s.db.QueryRow(c, `SELECT result_style FROM contest_problems WHERE contest_id = $1 AND problem_id = $2`,
				*v.ContestID, v.ProblemID).Scan(&v.Style); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
	} else {
		if status != "online" && !privileged && !owner {
			return errNotFound
		}
		if !owner {
			if err := c.checkLockdown(); err != nil {
				return err
			}
		}
	}

	switch v.Style {
	case "total":
		v.Result.Subtasks = nil
		v.Result.Testcases = nil
	case "subtask":
		v.Result.Testcases = nil
	}
	if !privileged {
		// Checker output can reveal test data; compiler output only goes to the author.
		for i := range v.Result.Testcases {
			v.Result.Testcases[i].Message = ""
		}
		if !owner {
			v.Message = ""
		}
	}
	if owner || privileged {
		v.Code = &code
		if !owner {
			c.audit("manage.submission.view", "Viewed another account's source code",
				map[string]any{"submission_id": v.ID, "owner_id": v.AccountID}, v.ContestID)
		}
	}
	v.CanRejudge = privileged
	return c.ok(v)
}

func (s *Server) rejudgeSubmission(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var contestID *int
	err = s.db.QueryRow(c, `SELECT contest_id FROM submissions WHERE id = $1`, id).Scan(&contestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	allowed := c.acct.Admin()
	if !allowed && contestID != nil {
		_, access, err := s.loadAccess(c, *contestID)
		if err != nil {
			return err
		}
		allowed = access.IsAdmin
	}
	if !allowed {
		return errNotFound
	}
	if _, err := s.db.Exec(c, `UPDATE submissions SET `+resetSubmission+`, priority = 2 WHERE id = $1`, id); err != nil {
		return err
	}
	c.audit("manage.submission.rejudge", "Submission rejudged", map[string]any{"submission_id": id}, contestID)
	return c.noContent()
}

func (s *Server) adminRejudgeProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	if err := s.problemExists(c, id); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE submissions SET `+resetSubmission+`, priority = -1 WHERE problem_id = $1`, id)
	if err != nil {
		return err
	}
	c.audit("manage.problem.rejudge", "Problem rejudged", map[string]any{"problem_id": id, "submissions": tag.RowsAffected()}, nil)
	return c.ok(map[string]any{"submissions": tag.RowsAffected()})
}
