package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxTestdataUpload = 512 << 20
	maxFileUpload     = 64 << 20
	maxStatement      = 256 << 10
)

var (
	fileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	testNameRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]{0,63})\.(in|out|ans)$`)
	tagRe      = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _+#.-]{0,31}$`)
)

// inlineTypes are attachment types safe to render in the browser. Anything
// else (HTML and SVG in particular) is forced to download.
var inlineTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".pdf": "application/pdf", ".txt": "text/plain; charset=utf-8",
}

func (s *Server) problemDir(id int, sub string) string {
	return filepath.Join(s.cfg.DataDir, "problems", strconv.Itoa(id), sub)
}

func (s *Server) testcasePath(problemID, testcaseID int, kind string) string {
	return filepath.Join(s.problemDir(problemID, "testdata"), fmt.Sprintf("%d.%s", testcaseID, kind))
}

func (s *Server) listFiles(problemID int) []map[string]any {
	out := []map[string]any{}
	entries, _ := os.ReadDir(s.problemDir(problemID, "files"))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && fileNameRe.MatchString(e.Name()) {
			out = append(out, map[string]any{"name": e.Name(), "size": info.Size()})
		}
	}
	return out
}

func (s *Server) listTags(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT tag, count(*) AS count FROM problems, unnest(tags) AS tag
		WHERE status = 'online' GROUP BY tag ORDER BY count(*) DESC, tag LIMIT 200`)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) listProblems(c *Ctx) error {
	limit, offset, page := c.page(50)
	q := c.r.URL.Query()
	classID, hasClass, err := c.queryInt("class_id")
	if err != nil {
		return err
	}
	args := []any{c.acct.Admin(), c.acctID()}
	// $2 is cast here so the count query, which may not use it otherwise, still types it.
	where := `(($1 AND p.status <> 'contest') OR p.status = 'online') AND $2::int >= 0`
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		args = append(args, "%"+escapeLike(v)+"%", strings.TrimPrefix(v, "#"))
		where += fmt.Sprintf(` AND (p.name ILIKE $%d OR p.id::text = $%d)`, len(args)-1, len(args))
	}
	if v := q.Get("tag"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(` AND $%d = ANY (p.tags)`, len(args))
	}
	if hasClass {
		args = append(args, classID)
		where += fmt.Sprintf(` AND p.id IN (SELECT unnest(problem_ids) FROM problem_classes
			WHERE id = $%d AND (public OR owner_id = $2 OR ($1 AND owner_id IS NULL)))`, len(args))
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM problems p WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	args = append(args, limit, offset)
	items, err := s.jsonList(c, fmt.Sprintf(`
		SELECT p.id, p.name, p.status, p.tags, p.allow_submit, st.tried_users, st.solved_users, b.state AS my_state, b.score AS my_score
		FROM (SELECT * FROM problems p WHERE %s ORDER BY p.id LIMIT $%d OFFSET $%d) p
		LEFT JOIN LATERAL (
			SELECT count(DISTINCT account_id) AS tried_users, count(DISTINCT account_id) FILTER (WHERE state = 1) AS solved_users
			FROM submissions WHERE problem_id = p.id AND contest_id IS NULL AND state < 100) st ON true
		LEFT JOIN LATERAL (
			SELECT min(state) AS state, max(score) AS score
			FROM submissions WHERE problem_id = p.id AND account_id = $2 AND contest_id IS NULL AND state < 100) b ON true
		ORDER BY p.id`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

// problemJSON loads the contestant-facing view of a problem.
func (s *Server) problemJSON(c *Ctx, id int) (map[string]any, error) {
	raw, err := s.jsonRow(c, `
		SELECT p.id, p.name, p.status, p.allow_submit, p.tags, p.statement, p.time_limit_ms, p.memory_limit_kb,
			p.checker, p.languages, p.score_precision,
			(SELECT count(*) FROM testcases t WHERE t.problem_id = p.id) AS testcase_count,
			(SELECT coalesce(jsonb_agg(jsonb_build_object('idx', st.idx, 'score', st.score,
				'testcases', cardinality(st.testcase_ids), 'depends', st.depends) ORDER BY st.idx), '[]')
				FROM subtasks st WHERE st.problem_id = p.id) AS subtasks
		FROM problems p WHERE p.id = $1`, id)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out["files"] = s.listFiles(id)
	return out, nil
}

func (s *Server) getProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	out, err := s.problemJSON(c, id)
	if err != nil {
		return err
	}
	// Hidden and contest-only problems do not exist for ordinary visitors.
	if out["status"] != "online" && !c.acct.Admin() {
		return errNotFound
	}
	stats, err := s.jsonRow(c, `
		SELECT count(*) AS submissions, count(*) FILTER (WHERE state = 1) AS ac_submissions,
			count(DISTINCT account_id) AS tried_users, count(DISTINCT account_id) FILTER (WHERE state = 1) AS solved_users,
			min(state) FILTER (WHERE account_id = $2) AS my_state, max(score) FILTER (WHERE account_id = $2) AS my_score
		FROM submissions WHERE problem_id = $1 AND contest_id IS NULL AND state < 100`, id, c.acctID())
	if err != nil {
		return err
	}
	out["stats"] = stats
	return c.ok(out)
}

func (s *Server) problemRank(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var status string
	if err := s.db.QueryRow(c, `SELECT status FROM problems WHERE id = $1`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		return err
	}
	if status != "online" && !c.acct.Admin() {
		return errNotFound
	}
	limit, offset, page := c.page(50)
	const best = `SELECT DISTINCT ON (s.account_id) s.id, s.account_id, a.name AS account_name, s.language,
			s.time_ms, s.memory_kb, s.score, s.created_at
		FROM submissions s JOIN accounts a ON a.id = s.account_id
		WHERE s.problem_id = $1 AND s.contest_id IS NULL AND s.state = 1
		ORDER BY s.account_id, s.time_ms, s.memory_kb, s.id`
	items, err := s.jsonList(c, `SELECT * FROM (`+best+`) b ORDER BY time_ms, memory_kb, id LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(DISTINCT account_id) FROM submissions
		WHERE problem_id = $1 AND contest_id IS NULL AND state = 1`, id).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

// canReadProblem reports whether the caller may read a problem's statement
// and attachments, directly or through a contest they can currently view.
func (s *Server) canReadProblem(c *Ctx, id int) (bool, error) {
	var status string
	err := s.db.QueryRow(c, `SELECT status FROM problems WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if c.acct.Admin() {
		return true, nil
	}
	if status == "online" {
		locked, err := c.lockdownContest()
		if err != nil {
			return false, err
		}
		if locked == 0 {
			return true, nil
		}
	}
	if status == "hidden" {
		return false, nil
	}
	rows, err := s.db.Query(c, `SELECT contest_id FROM contest_problems WHERE problem_id = $1`, id)
	if err != nil {
		return false, err
	}
	contestIDs, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return false, err
	}
	for _, cid := range contestIDs {
		_, access, err := s.loadAccess(c, cid)
		if err != nil {
			return false, err
		}
		if access.CanViewProblems {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) getProblemFile(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	name := c.r.PathValue("name")
	if !fileNameRe.MatchString(name) {
		return errNotFound
	}
	ok, err := s.canReadProblem(c, id)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	return c.serveFile(filepath.Join(s.problemDir(id, "files"), name), name)
}

func (c *Ctx) serveFile(path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return errNotFound
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errNotFound
	}
	h := c.w.Header()
	ctype, inline := inlineTypes[strings.ToLower(filepath.Ext(name))]
	disposition := "inline"
	if !inline {
		ctype, disposition = "application/octet-stream", "attachment"
	}
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-cache")
	http.ServeContent(c.w, c.r, "", info.ModTime(), f)
	return nil
}

// ---- Administration ----

func (s *Server) adminListProblems(c *Ctx) error {
	limit, offset, page := c.page(50)
	items, err := s.jsonList(c, `SELECT p.id, p.name, p.status, p.allow_submit, p.tags, p.updated_at,
			(SELECT count(*) FROM testcases t WHERE t.problem_id = p.id) AS testcase_count,
			(SELECT count(*) FROM submissions sb WHERE sb.problem_id = p.id) AS submissions
		FROM problems p ORDER BY p.id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return err
	}
	var total int
	if err := s.db.QueryRow(c, `SELECT count(*) FROM problems`).Scan(&total); err != nil {
		return err
	}
	return c.ok(paged{items, total, page, limit})
}

func (s *Server) adminCreateProblem(c *Ctx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Name = cleanText(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 64 {
		return badRequest("Problem name must be 1 to 64 characters")
	}
	var id int
	if err := s.db.QueryRow(c, `INSERT INTO problems (name) VALUES ($1) RETURNING id`, in.Name).Scan(&id); err != nil {
		return err
	}
	c.audit("manage.problem.create", "Problem created", map[string]any{"problem_id": id, "name": in.Name}, nil)
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) adminGetProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	raw, err := s.jsonRow(c, `SELECT p.*,
			(SELECT coalesce(jsonb_agg(to_jsonb(t) - 'problem_id' ORDER BY t.name), '[]') FROM testcases t WHERE t.problem_id = p.id) AS testcases,
			(SELECT coalesce(jsonb_agg(to_jsonb(st) - 'problem_id' ORDER BY st.idx), '[]') FROM subtasks st WHERE st.problem_id = p.id) AS subtasks,
			(SELECT coalesce(jsonb_agg(jsonb_build_object('id', ct.id, 'name', ct.name) ORDER BY ct.id), '[]')
				FROM contest_problems cp JOIN contests ct ON ct.id = cp.contest_id WHERE cp.problem_id = p.id) AS contests
		FROM problems p WHERE p.id = $1`, id)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	out["files"] = s.listFiles(id)
	return c.ok(out)
}

func (s *Server) adminUpdateProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in struct {
		Name           *string   `json:"name"`
		Status         *string   `json:"status"`
		AllowSubmit    *bool     `json:"allow_submit"`
		Tags           *[]string `json:"tags"`
		Statement      *string   `json:"statement"`
		TimeLimitMS    *int      `json:"time_limit_ms"`
		MemoryLimitKB  *int      `json:"memory_limit_kb"`
		Checker        *string   `json:"checker"`
		ScorePrecision *int      `json:"score_precision"`
		Languages      *[]string `json:"languages"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if in.Name != nil {
		*in.Name = cleanText(*in.Name)
		if n := utf8.RuneCountInString(*in.Name); n < 1 || n > 64 {
			return badRequest("Problem name must be 1 to 64 characters")
		}
	}
	if in.Status != nil && !oneOf(*in.Status, "online", "contest", "hidden") {
		return badRequest("Invalid status")
	}
	if in.Checker != nil && !oneOf(*in.Checker, "diff", "diff-strict", "float-4", "float-6", "float-9", "testlib") {
		return badRequest("Invalid checker")
	}
	if in.Statement != nil && (len(*in.Statement) > maxStatement || !utf8.ValidString(*in.Statement) || strings.ContainsRune(*in.Statement, 0)) {
		return badRequest("Statement is too long or not valid text")
	}
	if in.TimeLimitMS != nil && (*in.TimeLimitMS < 1 || *in.TimeLimitMS > 600000) {
		return badRequest("Time limit must be between 1 and 600000 ms")
	}
	if in.MemoryLimitKB != nil && (*in.MemoryLimitKB < 1024 || *in.MemoryLimitKB > 16777216) {
		return badRequest("Memory limit must be between 1024 and 16777216 KiB")
	}
	if in.ScorePrecision != nil && (*in.ScorePrecision < 0 || *in.ScorePrecision > 3) {
		return badRequest("Score precision must be between 0 and 3")
	}
	var tags, langs any // nil keeps the stored value
	if in.Tags != nil {
		clean, err := cleanTags(*in.Tags)
		if err != nil {
			return err
		}
		tags = clean
	}
	if in.Languages != nil {
		clean, err := cleanLanguages(*in.Languages)
		if err != nil {
			return err
		}
		langs = clean
	}
	tag, err := s.db.Exec(c, `UPDATE problems SET name = coalesce($2, name), status = coalesce($3, status),
			allow_submit = coalesce($4, allow_submit), tags = coalesce($5, tags), statement = coalesce($6, statement),
			time_limit_ms = coalesce($7, time_limit_ms), memory_limit_kb = coalesce($8, memory_limit_kb),
			checker = coalesce($9, checker), score_precision = coalesce($10, score_precision),
			languages = coalesce($11, languages), updated_at = now() WHERE id = $1`,
		id, in.Name, in.Status, in.AllowSubmit, tags, in.Statement, in.TimeLimitMS, in.MemoryLimitKB,
		in.Checker, in.ScorePrecision, langs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	in.Statement = nil // keep the log small
	c.audit("manage.problem.update", "Problem updated", map[string]any{"problem_id": id, "changes": in}, nil)
	return c.noContent()
}

func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

func cleanTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = cleanText(t)
		if t == "" || seen[t] {
			continue
		}
		if !tagRe.MatchString(t) {
			return nil, badRequest("Invalid tag %q", t)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 16 {
		return nil, badRequest("At most 16 tags are allowed")
	}
	return out, nil
}

func cleanLanguages(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, l := range in {
		if !validLanguage(l) {
			return nil, badRequest("Unknown language %q", l)
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *Server) adminDeleteProblem(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM problems WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	if err := os.RemoveAll(s.problemDir(id, "")); err != nil {
		s.log.Error("remove problem directory", "problem", id, "err", err)
	}
	c.audit("manage.problem.delete", "Problem deleted", map[string]any{"problem_id": id}, nil)
	return c.noContent()
}

func (s *Server) problemExists(c *Ctx, id int) error {
	var ok bool
	if err := s.db.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM problems WHERE id = $1)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	return nil
}

// saveUpload streams one multipart part to dst atomically and returns its size.
func saveUpload(part io.Reader, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, part)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), dst)
}

// adminUploadTestcases accepts multipart files named <case>.in and
// <case>.out (or .ans). Files are stored under server-chosen names, so the
// client-supplied name never reaches the filesystem.
func (s *Server) adminUploadTestcases(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	if err := s.problemExists(c, id); err != nil {
		return err
	}
	c.r.Body = http.MaxBytesReader(c.w, c.r.Body, maxTestdataUpload)
	mr, err := c.r.MultipartReader()
	if err != nil {
		return badRequest("Expected a multipart upload")
	}
	saved := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if part.FileName() == "" {
			continue
		}
		m := testNameRe.FindStringSubmatch(filepath.Base(strings.ReplaceAll(part.FileName(), `\`, "/")))
		if m == nil {
			return badRequest("Invalid testdata file name %q: use <name>.in and <name>.out", part.FileName())
		}
		kind, column := "in", "input_size"
		if m[2] != "in" {
			kind, column = "out", "output_size"
		}
		var tid int
		if err := s.db.QueryRow(c, `INSERT INTO testcases (problem_id, name) VALUES ($1, $2)
			ON CONFLICT (problem_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, id, m[1]).Scan(&tid); err != nil {
			return err
		}
		size, err := saveUpload(part, s.testcasePath(id, tid, kind))
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(c, `UPDATE testcases SET `+column+` = $2 WHERE id = $1`, tid, size); err != nil {
			return err
		}
		saved++
	}
	if saved == 0 {
		return badRequest("No files uploaded")
	}
	c.audit("manage.problem.testdata", "Testdata uploaded", map[string]any{"problem_id": id, "files": saved}, nil)
	return c.ok(map[string]any{"files": saved})
}

func (s *Server) adminDeleteTestcase(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tid, err := c.pathInt("tid")
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(c, `DELETE FROM testcases WHERE id = $1 AND problem_id = $2`, tid, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		_, err = tx.Exec(c, `UPDATE subtasks SET testcase_ids = array_remove(testcase_ids, $2) WHERE problem_id = $1`, id, tid)
		return err
	})
	if err != nil {
		return err
	}
	os.Remove(s.testcasePath(id, tid, "in"))
	os.Remove(s.testcasePath(id, tid, "out"))
	c.audit("manage.problem.testdata", "Testcase deleted", map[string]any{"problem_id": id, "testcase_id": tid}, nil)
	return c.noContent()
}

func (s *Server) adminDownloadTestcase(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tid, err := c.pathInt("tid")
	if err != nil {
		return err
	}
	kind := c.r.PathValue("kind")
	if kind != "in" && kind != "out" {
		return errNotFound
	}
	var name string
	err = s.db.QueryRow(c, `SELECT name FROM testcases WHERE id = $1 AND problem_id = $2`, tid, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return c.serveFile(s.testcasePath(id, tid, kind), name+"."+kind)
}

type subtaskInput struct {
	Score       float64 `json:"score"`
	TestcaseIDs []int   `json:"testcase_ids"`
	Depends     []int   `json:"depends"`
}

// adminSetSubtasks replaces the whole subtask list; subtasks are numbered
// from 1 in the order given and may only depend on earlier ones.
func (s *Server) adminSetSubtasks(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	var in []subtaskInput
	if err := c.bind(&in); err != nil {
		return err
	}
	if len(in) > 100 {
		return badRequest("At most 100 subtasks are allowed")
	}
	rows, err := s.db.Query(c, `SELECT id FROM testcases WHERE problem_id = $1`, id)
	if err != nil {
		return err
	}
	owned, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return err
	}
	valid := map[int]bool{}
	for _, t := range owned {
		valid[t] = true
	}
	for i := range in {
		st := &in[i]
		if st.Score < 0 || st.Score > 1e6 || st.Score != st.Score {
			return badRequest("Subtask %d: invalid score", i+1)
		}
		st.TestcaseIDs, st.Depends = uniqueInts(st.TestcaseIDs), uniqueInts(st.Depends)
		for _, t := range st.TestcaseIDs {
			if !valid[t] {
				return badRequest("Subtask %d: testcase %d does not belong to this problem", i+1, t)
			}
		}
		for _, d := range st.Depends {
			if d < 1 || d > i {
				return badRequest("Subtask %d may only depend on earlier subtasks", i+1)
			}
		}
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(c, `SELECT true FROM problems WHERE id = $1 FOR UPDATE`, id).Scan(&ok); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if _, err := tx.Exec(c, `DELETE FROM subtasks WHERE problem_id = $1`, id); err != nil {
			return err
		}
		for i, st := range in {
			if _, err := tx.Exec(c, `INSERT INTO subtasks (problem_id, idx, score, testcase_ids, depends) VALUES ($1, $2, $3, $4, $5)`,
				id, i+1, st.Score, st.TestcaseIDs, st.Depends); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.audit("manage.problem.subtasks", "Subtasks updated", map[string]any{"problem_id": id, "subtasks": in}, nil)
	return c.noContent()
}

func uniqueInts(in []int) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

func (s *Server) adminUploadFiles(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	if err := s.problemExists(c, id); err != nil {
		return err
	}
	c.r.Body = http.MaxBytesReader(c.w, c.r.Body, maxFileUpload)
	mr, err := c.r.MultipartReader()
	if err != nil {
		return badRequest("Expected a multipart upload")
	}
	var names []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if part.FileName() == "" {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
		if !fileNameRe.MatchString(name) {
			return badRequest("Invalid file name %q: use letters, digits, '.', '_' and '-'", part.FileName())
		}
		if _, err := saveUpload(part, filepath.Join(s.problemDir(id, "files"), name)); err != nil {
			return err
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return badRequest("No files uploaded")
	}
	c.audit("manage.problem.files", "Attachments uploaded", map[string]any{"problem_id": id, "files": names}, nil)
	return c.ok(map[string]any{"files": names})
}

func (s *Server) adminDeleteFile(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	name := c.r.PathValue("name")
	if !fileNameRe.MatchString(name) {
		return errNotFound
	}
	if err := os.Remove(filepath.Join(s.problemDir(id, "files"), name)); err != nil {
		return errNotFound
	}
	c.audit("manage.problem.files", "Attachment deleted", map[string]any{"problem_id": id, "file": name}, nil)
	return c.noContent()
}
