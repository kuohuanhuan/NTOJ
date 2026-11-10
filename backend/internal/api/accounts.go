package api

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/kuohuanhuan/ntoj/backend/internal/auth"
)

// bestCTE yields each account's best result per publicly visible problem,
// counting only submissions made outside contests.
const bestCTE = `best AS (
	SELECT s.account_id, s.problem_id, max(s.score) AS score, min(s.state) AS state
	FROM submissions s JOIN problems p ON p.id = s.problem_id AND p.status = 'online'
	WHERE s.contest_id IS NULL AND s.state < 100
	GROUP BY s.account_id, s.problem_id)`

// lockdownContest returns the ID of a running contest that confines the
// caller to contest pages, or 0.
func (c *Ctx) lockdownContest() (int, error) {
	if c.acct == nil || c.acct.Admin() {
		return 0, nil
	}
	var id int
	err := c.s.db.QueryRow(c, `
		SELECT c.id FROM contest_users cu JOIN contests c ON c.id = cu.contest_id
		WHERE cu.account_id = $1 AND cu.status = 'approved' AND NOT c.allow_other_pages
		  AND now() >= CASE WHEN c.time_mode = 'flexible' THEN cu.session_start ELSE c.start_at END
		  AND now() <  CASE WHEN c.time_mode = 'flexible' THEN cu.session_end ELSE c.end_at END
		ORDER BY c.id LIMIT 1`, c.acct.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (c *Ctx) checkLockdown() error {
	id, err := c.lockdownContest()
	if err != nil {
		return err
	}
	if id != 0 {
		return &Error{Status: 403, Code: "contest_lockdown", Extra: map[string]any{"contest_id": id},
			Message: "This page is unavailable while you are taking part in a contest"}
	}
	return nil
}

func (s *Server) getAccount(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	acct, err := s.jsonRow(c, `WITH `+bestCTE+`
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
		FROM accounts a WHERE a.id = $1 AND NOT a.disabled`, id)
	if err != nil {
		return err
	}
	return c.ok(acct)
}

func (s *Server) userRank(c *Ctx) error {
	limit, offset, page := c.page(50)
	items, err := s.jsonList(c, `WITH `+bestCTE+`
		SELECT a.id, a.name, a.motto, a.photo_url, coalesce(sum(b.score), 0) AS rate,
			count(b.problem_id) FILTER (WHERE b.state = 1) AS ac_problems,
			rank() OVER (ORDER BY coalesce(sum(b.score), 0) DESC) AS rank
		FROM accounts a LEFT JOIN best b ON b.account_id = a.id
		WHERE NOT a.disabled GROUP BY a.id ORDER BY rate DESC, a.id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM accounts WHERE NOT disabled`).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

func (s *Server) adminListAccounts(c *Ctx) error {
	limit, offset, page := c.page(50)
	q := "%" + escapeLike(strings.TrimSpace(c.r.URL.Query().Get("q"))) + "%"
	const where = ` WHERE (name ILIKE $1 OR email ILIKE $1 OR id::text = trim(both '%' from $1))`
	items, err := s.jsonList(c, `SELECT id, email, name, role, disabled, last_ip, allowed_ip, created_at
		FROM accounts`+where+` ORDER BY id LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM accounts`+where, q).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Server) adminUpdateAccount(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in struct {
		Name      *string `json:"name"`
		Role      *string `json:"role"`
		Disabled  *bool   `json:"disabled"`
		AllowedIP *string `json:"allowed_ip"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if in.Name != nil {
		*in.Name = cleanText(*in.Name)
		if err := validName(*in.Name); err != nil {
			return err
		}
	}
	if in.Role != nil && *in.Role != "admin" && *in.Role != "user" {
		return badRequest("Invalid role")
	}
	if in.AllowedIP != nil && *in.AllowedIP != "" {
		ip, err := netip.ParseAddr(strings.TrimSpace(*in.AllowedIP))
		if err != nil {
			return badRequest("Allowed IP must be a valid IP address")
		}
		*in.AllowedIP = ip.Unmap().String()
	}
	if id == c.acct.ID && ((in.Role != nil && *in.Role != "admin") || (in.Disabled != nil && *in.Disabled)) {
		return badRequest("You cannot demote or disable your own account")
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(c, `UPDATE accounts SET name = coalesce($2, name), role = coalesce($3, role),
			disabled = coalesce($4, disabled), allowed_ip = coalesce($5, allowed_ip) WHERE id = $1`,
			id, in.Name, in.Role, in.Disabled, in.AllowedIP)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		if in.Disabled != nil && *in.Disabled {
			_, err = tx.Exec(c, `DELETE FROM sessions WHERE account_id = $1`, id)
		}
		return err
	})
	if err != nil {
		return err
	}
	c.audit("manage.account.update", "Account updated", map[string]any{"account_id": id, "changes": in}, nil)
	return c.noContent()
}

func (s *Server) adminResetPassword(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		return badRequest("%s", capitalize(err.Error()))
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(c, `UPDATE accounts SET password_hash = $2 WHERE id = $1`, id, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		_, err = tx.Exec(c, `DELETE FROM sessions WHERE account_id = $1 AND id <> $2`, id, c.acct.SessionID)
		return err
	})
	if err != nil {
		return err
	}
	c.audit("manage.account.password", "Password reset by administrator", map[string]any{"account_id": id}, nil)
	return c.noContent()
}
