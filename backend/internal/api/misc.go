package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// ---- Bulletins ----

type bulletinInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Color   string `json:"color"`
	Pinned  bool   `json:"pinned"`
}

func (in *bulletinInput) validate() error {
	in.Title, in.Content = cleanText(in.Title), cleanText(in.Content)
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 100 {
		return badRequest("Title must be 1 to 100 characters")
	}
	if in.Content == "" || len(in.Content) > maxStatement {
		return badRequest("Content must not be empty or too long")
	}
	if !oneOf(in.Color, "", "red", "orange", "yellow", "green", "blue", "purple") {
		return badRequest("Invalid color")
	}
	return nil
}

const bulletinSelect = `SELECT b.id, b.title, b.content, b.color, b.pinned, b.created_at, b.author_id, a.name AS author_name
	FROM bulletins b LEFT JOIN accounts a ON a.id = b.author_id`

func (s *Server) listBulletins(c *Ctx) error {
	out, err := s.jsonList(c, bulletinSelect+` ORDER BY b.pinned DESC, b.id DESC LIMIT 200`)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) getBulletin(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	out, err := s.jsonRow(c, bulletinSelect+` WHERE b.id = $1`, id)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) adminCreateBulletin(c *Ctx) error {
	var in bulletinInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	var id int
	if err := s.db.QueryRow(c, `INSERT INTO bulletins (title, content, color, pinned, author_id) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.Title, in.Content, in.Color, in.Pinned, c.acct.ID).Scan(&id); err != nil {
		return err
	}
	c.audit("manage.bulletin.create", "Bulletin published", map[string]any{"bulletin_id": id, "title": in.Title}, nil)
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) adminUpdateBulletin(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in bulletinInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE bulletins SET title = $2, content = $3, color = $4, pinned = $5 WHERE id = $1`,
		id, in.Title, in.Content, in.Color, in.Pinned)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("manage.bulletin.update", "Bulletin updated", map[string]any{"bulletin_id": id, "title": in.Title}, nil)
	return c.noContent()
}

func (s *Server) adminDeleteBulletin(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM bulletins WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("manage.bulletin.delete", "Bulletin deleted", map[string]any{"bulletin_id": id}, nil)
	return c.noContent()
}

// ---- Problem classes ----

// classVisible is the WHERE fragment deciding which classes $1 (account id,
// 0 for guests) may see; $2 is whether the caller is a site administrator.
const classVisible = `(pc.public OR pc.owner_id = $1 OR ($2 AND pc.owner_id IS NULL))`

const classSelect = `SELECT pc.id, pc.name, pc.description, pc.owner_id, a.name AS owner_name, pc.public,
		cardinality(pc.problem_ids) AS problem_count, pc.problem_ids,
		EXISTS (SELECT 1 FROM class_collections cc WHERE cc.class_id = pc.id AND cc.account_id = $1) AS collected
	FROM problem_classes pc LEFT JOIN accounts a ON a.id = pc.owner_id WHERE ` + classVisible

func (s *Server) listClasses(c *Ctx) error {
	out, err := s.jsonList(c, classSelect+` ORDER BY pc.owner_id IS NOT NULL, pc.id LIMIT 1000`, c.acctID(), c.acct.Admin())
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) getClass(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	out, err := s.jsonRow(c, classSelect+` AND pc.id = $3`, c.acctID(), c.acct.Admin(), id)
	if err != nil {
		return err
	}
	return c.ok(out)
}

type classInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ProblemIDs  []int  `json:"problem_ids"`
	Public      bool   `json:"public"`
	Official    bool   `json:"official"`
}

func (in *classInput) validate() error {
	in.Name, in.Description = cleanText(in.Name), cleanText(in.Description)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 50 {
		return badRequest("Name must be 1 to 50 characters")
	}
	if utf8.RuneCountInString(in.Description) > 2048 {
		return badRequest("Description must be at most 2048 characters")
	}
	in.ProblemIDs = uniqueInts(in.ProblemIDs)
	if len(in.ProblemIDs) == 0 || len(in.ProblemIDs) > 1000 {
		return badRequest("A class needs between 1 and 1000 problems")
	}
	for _, p := range in.ProblemIDs {
		if p <= 0 {
			return badRequest("Invalid problem id")
		}
	}
	return nil
}

func (s *Server) createClass(c *Ctx) error {
	var in classInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	var owner *int
	if in.Official {
		if !c.acct.Admin() {
			return errForbidden
		}
	} else {
		owner = &c.acct.ID
		var count int
		if err := s.db.QueryRow(c, `SELECT count(*) FROM problem_classes WHERE owner_id = $1`, c.acct.ID).Scan(&count); err != nil {
			return err
		}
		if count >= 50 {
			return conflict("You have reached the limit of 50 problem classes")
		}
	}
	var id int
	if err := s.db.QueryRow(c, `INSERT INTO problem_classes (name, description, problem_ids, owner_id, public)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, in.Name, in.Description, in.ProblemIDs, owner, in.Public).Scan(&id); err != nil {
		return err
	}
	c.audit("class.create", "Problem class created", map[string]any{"class_id": id, "name": in.Name, "official": in.Official}, nil)
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

// classOwner is the WHERE fragment restricting writes to the owner, or to
// site administrators for official classes.
const classOwner = `(owner_id = $2 OR ($3 AND owner_id IS NULL))`

func (s *Server) updateClass(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in classInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE problem_classes SET name = $4, description = $5, problem_ids = $6, public = $7
		WHERE id = $1 AND `+classOwner, id, c.acct.ID, c.acct.Admin(), in.Name, in.Description, in.ProblemIDs, in.Public)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("class.update", "Problem class updated", map[string]any{"class_id": id}, nil)
	return c.noContent()
}

func (s *Server) deleteClass(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM problem_classes WHERE id = $1 AND `+classOwner, id, c.acct.ID, c.acct.Admin())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("class.delete", "Problem class deleted", map[string]any{"class_id": id}, nil)
	return c.noContent()
}

func (s *Server) collectClass(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `INSERT INTO class_collections (account_id, class_id)
		SELECT $1, pc.id FROM problem_classes pc WHERE pc.id = $3 AND `+classVisible+` ON CONFLICT DO NOTHING`,
		c.acct.ID, c.acct.Admin(), id)
	if err != nil {
		return err
	}
	_ = tag
	return c.noContent()
}

func (s *Server) uncollectClass(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(c, `DELETE FROM class_collections WHERE account_id = $1 AND class_id = $2`, c.acct.ID, id); err != nil {
		return err
	}
	return c.noContent()
}

// ---- Boards ----

type boardInput struct {
	Name       string    `json:"name"`
	Visible    bool      `json:"visible"`
	StartAt    time.Time `json:"start_at"`
	EndAt      time.Time `json:"end_at"`
	ProblemIDs []int     `json:"problem_ids"`
	AccountIDs []int     `json:"account_ids"`
}

func (in *boardInput) validate() error {
	in.Name = cleanText(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 64 {
		return badRequest("Name must be 1 to 64 characters")
	}
	if !in.EndAt.After(in.StartAt) {
		return badRequest("The board must end after it starts")
	}
	in.ProblemIDs, in.AccountIDs = dedupe(in.ProblemIDs), dedupe(in.AccountIDs)
	if len(in.ProblemIDs) > 200 || len(in.AccountIDs) > 2000 {
		return badRequest("Too many problems or accounts")
	}
	return nil
}

// dedupe removes repeats while keeping the given order.
func dedupe(in []int) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, v := range in {
		if v > 0 && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) listBoards(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT id, name, start_at, end_at FROM boards WHERE visible ORDER BY id DESC`)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) adminListBoards(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT * FROM boards ORDER BY id DESC`)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) getBoard(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	out, err := s.jsonRow(c, `
		SELECT b.id, b.name, b.start_at, b.end_at, b.visible,
			(SELECT coalesce(jsonb_agg(jsonb_build_object('id', p.id, 'name', CASE WHEN p.status = 'online' OR $2 THEN p.name ELSE '' END)
				ORDER BY array_position(b.problem_ids, p.id)), '[]') FROM problems p WHERE p.id = ANY (b.problem_ids)) AS problems,
			(SELECT coalesce(jsonb_agg(r ORDER BY r.total DESC, r.account_id), '[]') FROM (
				SELECT a.id AS account_id, a.name, coalesce(sum(best.score), 0) AS total,
					coalesce(jsonb_object_agg(best.problem_id, jsonb_build_object('score', best.score, 'state', best.state))
						FILTER (WHERE best.problem_id IS NOT NULL), '{}') AS cells
				FROM accounts a LEFT JOIN (
					SELECT s.account_id, s.problem_id, max(s.score) AS score, min(s.state) AS state FROM submissions s
					WHERE s.contest_id IS NULL AND s.state < 100 AND s.problem_id = ANY (b.problem_ids)
					  AND s.created_at >= b.start_at AND s.created_at < b.end_at
					GROUP BY s.account_id, s.problem_id) best ON best.account_id = a.id
				WHERE a.id = ANY (b.account_ids) GROUP BY a.id) r) AS rows
		FROM boards b WHERE b.id = $1 AND (b.visible OR $2)`, id, c.acct.Admin())
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) adminCreateBoard(c *Ctx) error {
	var in boardInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	var id int
	if err := s.db.QueryRow(c, `INSERT INTO boards (name, visible, start_at, end_at, problem_ids, account_ids)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, in.Name, in.Visible, in.StartAt, in.EndAt, in.ProblemIDs, in.AccountIDs).Scan(&id); err != nil {
		return err
	}
	c.audit("manage.board.create", "Board created", map[string]any{"board_id": id, "name": in.Name}, nil)
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) adminUpdateBoard(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in boardInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `UPDATE boards SET name = $2, visible = $3, start_at = $4, end_at = $5, problem_ids = $6, account_ids = $7
		WHERE id = $1`, id, in.Name, in.Visible, in.StartAt, in.EndAt, in.ProblemIDs, in.AccountIDs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("manage.board.update", "Board updated", map[string]any{"board_id": id}, nil)
	return c.noContent()
}

func (s *Server) adminDeleteBoard(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM boards WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	c.audit("manage.board.delete", "Board deleted", map[string]any{"board_id": id}, nil)
	return c.noContent()
}

// ---- Questions to the administrators ----

func (s *Server) listQuestions(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT id, content, reply, created_at, replied_at FROM questions
		WHERE account_id = $1 ORDER BY id DESC`, c.acct.ID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(c, `UPDATE questions SET reply_seen = true WHERE account_id = $1 AND reply IS NOT NULL AND NOT reply_seen`, c.acct.ID); err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) askQuestion(c *Ctx) error {
	var in struct{ Content string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Content = cleanText(in.Content)
	if n := utf8.RuneCountInString(in.Content); n < 1 || n > 1024 {
		return badRequest("Question must be 1 to 1024 characters")
	}
	if !s.limiter.allow("question:"+c.ip, 10, 10*time.Minute) {
		return errTooMany
	}
	var id int
	err := s.db.QueryRow(c, `INSERT INTO questions (account_id, content)
		SELECT $1, $2 WHERE (SELECT count(*) FROM questions WHERE account_id = $1) < 20 RETURNING id`, c.acct.ID, in.Content).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("You have too many questions; delete old ones first")
	}
	if err != nil {
		return err
	}
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) deleteQuestion(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM questions WHERE id = $1 AND account_id = $2`, id, c.acct.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return c.noContent()
}

func (s *Server) adminListQuestions(c *Ctx) error {
	limit, offset, page := c.page(50)
	items, err := s.jsonList(c, `SELECT q.id, q.account_id, a.name AS account_name, q.content, q.reply, q.created_at, q.replied_at
		FROM questions q JOIN accounts a ON a.id = q.account_id
		ORDER BY q.reply IS NOT NULL, q.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM questions`).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

func (s *Server) adminReplyQuestion(c *Ctx) error {
	id, err := c.pathInt("id")
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
	tag, err := s.db.Exec(c, `UPDATE questions SET reply = $2, replier_id = $3, replied_at = now(), reply_seen = false WHERE id = $1`,
		id, in.Reply, c.acct.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return c.noContent()
}

// ---- Audit log and dashboard ----

func (s *Server) logPage(c *Ctx, contestID *int) error {
	limit, offset, page := c.page(50)
	typ := strings.TrimSpace(c.r.URL.Query().Get("type"))
	const where = ` WHERE ($1::int IS NULL OR l.contest_id = $1) AND ($2 = '' OR l.type LIKE $2 || '%')`
	typ = escapeLike(typ)
	items, err := s.jsonList(c, `SELECT l.id, l.type, l.message, l.params, l.operator_id, a.name AS operator_name,
			l.operator_ip, l.contest_id, l.created_at
		FROM audit_logs l LEFT JOIN accounts a ON a.id = l.operator_id`+where+` ORDER BY l.id DESC LIMIT $3 OFFSET $4`,
		contestID, typ, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM audit_logs l`+where, contestID, typ).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

func (s *Server) adminLogs(c *Ctx) error { return s.logPage(c, nil) }

func (s *Server) adminDashboard(c *Ctx) error {
	out, err := s.jsonRow(c, `SELECT
		(SELECT count(*) FROM accounts) AS accounts,
		(SELECT count(*) FROM problems) AS problems,
		(SELECT count(*) FROM contests) AS contests,
		(SELECT count(*) FROM submissions) AS submissions,
		(SELECT count(*) FROM submissions WHERE state = 101) AS pending,
		(SELECT count(*) FROM submissions WHERE state = 100) AS judging,
		(SELECT count(*) FROM questions WHERE reply IS NULL) AS open_questions,
		$1::boolean AS judge_configured`, s.cfg.JudgeToken != "")
	if err != nil {
		return err
	}
	return c.ok(out)
}
