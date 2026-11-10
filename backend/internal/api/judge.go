package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kuohuanhuan/ntoj/backend/internal/score"
)

const (
	staleClaim     = 10 * time.Minute
	maxJudgeOutput = 16 << 10
)

// judge authenticates a judge worker by bearer token. The endpoints do not
// exist at all when no token is configured.
func (s *Server) judge(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Ctx{Context: r.Context(), s: s, w: w, r: r, ip: s.clientIP(r)}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		want, got := sha256.Sum256([]byte(s.cfg.JudgeToken)), sha256.Sum256([]byte(token))
		switch {
		case s.cfg.JudgeToken == "":
			s.writeError(c, errNotFound)
		case !ok || subtle.ConstantTimeCompare(want[:], got[:]) != 1:
			s.writeError(c, errUnauthorized)
		default:
			if err := h(c); err != nil {
				s.writeError(c, err)
			}
		}
	}
}

// judgeClaim hands the next queued submission to a worker, or 204.
func (s *Server) judgeClaim(c *Ctx) error {
	var id int64
	var problemID int
	var language, code string
	err := s.db.QueryRow(c, `UPDATE submissions SET state = 100, claimed_at = now()
		WHERE id = (SELECT id FROM submissions WHERE state = 101 ORDER BY priority DESC, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, problem_id, language, code`).Scan(&id, &problemID, &language, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.noContent()
	}
	if err != nil {
		return err
	}
	problem, err := s.jsonRow(c, `SELECT p.id, p.time_limit_ms, p.memory_limit_kb, p.checker,
			(SELECT coalesce(jsonb_agg(to_jsonb(t) - 'problem_id' ORDER BY t.name), '[]') FROM testcases t WHERE t.problem_id = p.id) AS testcases,
			(SELECT coalesce(jsonb_agg(to_jsonb(st) - 'problem_id' ORDER BY st.idx), '[]') FROM subtasks st WHERE st.problem_id = p.id) AS subtasks
		FROM problems p WHERE p.id = $1`, problemID)
	if err != nil {
		return err
	}
	return c.ok(map[string]any{"id": id, "language": language, "code": code, "problem": problem})
}

func truncate(s string, max int) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max], "") + "\n[truncated]"
	}
	return s
}

// judgeResult stores a verdict. Scores are always computed here, from the
// problem's subtask configuration, never taken from the worker.
func (s *Server) judgeResult(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in struct {
		State     int                `json:"state"` // CE, CLE, IE or JE; 0 when testcases were run
		Message   string             `json:"message"`
		Testcases []score.CaseResult `json:"testcases"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if in.State != 0 && (in.State < score.CE || in.State > score.JE) {
		return badRequest("state must be 0 or a compile/judge error")
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		var problemID, precision int
		err := tx.QueryRow(c, `SELECT s.problem_id, p.score_precision FROM submissions s JOIN problems p ON p.id = s.problem_id
			WHERE s.id = $1 AND s.state = 100 FOR UPDATE OF s`, id).Scan(&problemID, &precision)
		if errors.Is(err, pgx.ErrNoRows) {
			return conflict("Submission is not being judged")
		}
		if err != nil {
			return err
		}
		res := score.Result{State: in.State}
		cases := []score.CaseResult{}
		if in.State == 0 {
			rows, err := tx.Query(c, `SELECT id, name FROM testcases WHERE problem_id = $1`, problemID)
			if err != nil {
				return err
			}
			names := map[int]string{}
			var tid int
			var name string
			if _, err := pgx.ForEachRow(rows, []any{&tid, &name}, func() error { names[tid] = name; return nil }); err != nil {
				return err
			}
			seen := map[int]bool{}
			for _, tc := range in.Testcases {
				name, ok := names[tc.ID]
				if !ok || seen[tc.ID] || !score.ValidVerdict(tc.State) || tc.TimeMS < 0 || tc.MemoryKB < 0 {
					return badRequest("Invalid result for testcase %d", tc.ID)
				}
				seen[tc.ID] = true
				tc.Name, tc.Message = name, truncate(tc.Message, 4<<10)
				cases = append(cases, tc)
			}
			rows, err = tx.Query(c, `SELECT idx, score::float8, testcase_ids, depends FROM subtasks WHERE problem_id = $1 ORDER BY idx`, problemID)
			if err != nil {
				return err
			}
			subtasks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[score.Subtask])
			if err != nil {
				return err
			}
			if len(subtasks) == 0 {
				// Without subtasks every testcase of the problem counts.
				for tid, name := range names {
					if !seen[tid] {
						cases = append(cases, score.CaseResult{ID: tid, Name: name, State: score.Skipped})
					}
				}
			}
			res = score.Evaluate(subtasks, cases, precision)
		}
		_, err = tx.Exec(c, `UPDATE submissions SET state = $2, score = $3, time_ms = $4, memory_kb = $5, message = $6,
			result = $7, judged_at = now() WHERE id = $1`, id, res.State, res.Score, res.TimeMS, res.MemoryKB,
			truncate(in.Message, maxJudgeOutput), submissionResult{Subtasks: res.Subtasks, Testcases: cases})
		return err
	})
	if err != nil {
		return err
	}
	return c.noContent()
}

func (s *Server) judgeTestcase(c *Ctx) error {
	tid, err := c.pathInt("tid")
	if err != nil {
		return err
	}
	kind := c.r.PathValue("kind")
	if kind != "in" && kind != "out" {
		return errNotFound
	}
	var problemID int
	var name string
	err = s.db.QueryRow(c, `SELECT problem_id, name FROM testcases WHERE id = $1`, tid).Scan(&problemID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return c.serveFile(s.testcasePath(problemID, tid, kind), name+"."+kind)
}

// RunMaintenance periodically requeues submissions abandoned by a worker and
// removes expired sessions, until ctx is cancelled.
func (s *Server) RunMaintenance(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if tag, err := s.db.Exec(ctx, `UPDATE submissions SET state = 101, claimed_at = NULL
			WHERE state = 100 AND claimed_at < now() - $1::interval`, staleClaim.String()); err != nil {
			if ctx.Err() == nil {
				s.log.Error("requeue stale submissions", "err", err)
			}
		} else if tag.RowsAffected() > 0 {
			s.log.Warn("requeued abandoned submissions", "count", tag.RowsAffected())
		}
		s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
