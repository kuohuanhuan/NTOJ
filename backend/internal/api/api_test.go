package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kuohuanhuan/ntoj/backend/internal/config"
	"github.com/kuohuanhuan/ntoj/backend/internal/db"
)

const judgeToken = "test-judge-token-0123456789abcdef-0123456789"

type env struct {
	t   *testing.T
	srv *Server
	url string
	db  *pgxpool.Pool
}

// setup starts the API against TEST_DATABASE_URL with a pristine schema.
func setup(t *testing.T) *env {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: t.TempDir(), SiteTitle: "test", AllowSignup: true, JudgeToken: judgeToken,
		AdminEmail: "root@example.com", AdminName: "root", AdminPassword: "root-password"}
	srv := New(pool, cfg, slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})))
	if err := srv.BootstrapAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); pool.Close() })
	return &env{t: t, srv: srv, url: ts.URL, db: pool}
}

type client struct {
	e    *env
	http *http.Client
	hdr  map[string]string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}, hdr: map[string]string{}}
}

func (e *env) signIn(email, password string) *client {
	c := e.client()
	c.must(200, "POST", "/api/auth/signin", map[string]any{"email": email, "password": password})
	return c
}

func (e *env) signUp(name string) (*client, int) {
	c := e.client()
	out := c.must(201, "POST", "/api/auth/signup", map[string]any{"email": name + "@example.com", "name": name, "password": "password-" + name})
	return c, int(out["id"].(float64))
}

// do sends a request; body may be nil, a JSON-encodable value or *bytes.Buffer
// (sent as-is with the Content-Type in c.hdr).
func (c *client) do(method, path string, body any) (int, map[string]any, []byte) {
	c.e.t.Helper()
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case *bytes.Buffer:
		rd = b
	default:
		raw, _ := json.Marshal(b)
		rd, ctype = bytes.NewReader(raw), "application/json"
	}
	req, _ := http.NewRequest(method, c.e.url+path, rd)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range c.hdr {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return res.StatusCode, out, raw
}

func (c *client) must(status int, method, path string, body any) map[string]any {
	c.e.t.Helper()
	got, out, raw := c.do(method, path, body)
	if got != status {
		c.e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, status, raw)
	}
	return out
}

func (c *client) upload(path string, files map[string]string) (int, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		w, _ := mw.CreateFormFile("files", name)
		io.WriteString(w, content)
	}
	mw.Close()
	c.hdr["Content-Type"] = mw.FormDataContentType()
	defer delete(c.hdr, "Content-Type")
	status, _, raw := c.do("POST", path, &buf)
	return status, raw
}

func items(out map[string]any) []any { return out["items"].([]any) }

func TestAuthAndSessions(t *testing.T) {
	e := setup(t)
	alice, aliceID := e.signUp("alice")

	me := alice.must(200, "GET", "/api/session", nil)
	if me["account"].(map[string]any)["name"] != "alice" {
		t.Fatalf("session: %v", me)
	}
	for _, ck := range alice.http.Jar.Cookies(mustURL(e.url)) {
		if ck.Name == sessionCookie && len(ck.Value) < 40 {
			t.Fatal("session token too short")
		}
	}
	var stored int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash = convert_to($1, 'UTF8')`,
		alice.http.Jar.Cookies(mustURL(e.url))[0].Value).Scan(&stored)
	if stored != 0 {
		t.Fatal("raw session token must not be stored")
	}

	// Validation and duplicate handling.
	c := e.client()
	c.must(400, "POST", "/api/auth/signup", map[string]any{"email": "bob@example.com", "name": "bob", "password": "short"})
	c.must(400, "POST", "/api/auth/signup", map[string]any{"email": "not-an-email", "name": "bob", "password": "long-enough-pw"})
	c.must(409, "POST", "/api/auth/signup", map[string]any{"email": "ALICE@example.com", "name": "x", "password": "long-enough-pw"})
	c.must(401, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "wrong-password"})
	c.must(401, "POST", "/api/auth/signin", map[string]any{"email": "nobody@example.com", "password": "wrong-password"})
	c.must(400, "POST", "/api/auth/signin", map[string]any{"email": "a@b.c", "password": "x", "role": "admin"}) // unknown field
	c.must(401, "GET", "/api/account/sessions", nil)
	c.must(401, "PATCH", "/api/account", map[string]any{"name": "x"})

	// Cross-site form posts are rejected before reaching any handler.
	c.hdr["Sec-Fetch-Site"] = "cross-site"
	c.must(403, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "password-alice"})
	delete(c.hdr, "Sec-Fetch-Site")
	c.hdr["Origin"] = "https://evil.example"
	c.must(403, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "password-alice"})
	delete(c.hdr, "Origin")

	// Profile validation: only https image URLs.
	alice.must(400, "PATCH", "/api/account", map[string]any{"name": "alice", "photo_url": "javascript:alert(1)", "cover_url": "", "motto": ""})
	alice.must(204, "PATCH", "/api/account", map[string]any{"name": "Alice", "photo_url": "https://example.com/a.png", "cover_url": "", "motto": "hi"})

	// Changing the password signs every other device out.
	second := e.signIn("alice@example.com", "password-alice")
	alice.must(403, "POST", "/api/account/password", map[string]any{"old": "nope", "new": "new-password-1"})
	alice.must(204, "POST", "/api/account/password", map[string]any{"old": "password-alice", "new": "new-password-1"})
	second.must(401, "GET", "/api/account/sessions", nil)
	alice.must(200, "GET", "/api/account/sessions", nil)
	e.client().must(401, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "password-alice"})

	// Ordinary accounts cannot reach administration.
	alice.must(403, "GET", "/api/admin/accounts", nil)
	alice.must(403, "POST", "/api/admin/problems", map[string]any{"name": "x"})
	e.client().must(401, "GET", "/api/admin/dashboard", nil)

	// Admin: restrict alice to one IP, then disable her.
	root := e.signIn("root@example.com", "root-password")
	list := root.must(200, "GET", "/api/admin/accounts?q=alice", nil)
	if len(items(list)) != 1 {
		t.Fatalf("account search: %v", list)
	}
	path := fmt.Sprintf("/api/admin/accounts/%d", aliceID)
	root.must(204, "PATCH", path, map[string]any{"allowed_ip": "10.9.8.7"})
	alice.must(401, "GET", "/api/account/sessions", nil) // existing session no longer honoured
	e.client().must(403, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "new-password-1"})
	root.must(204, "PATCH", path, map[string]any{"allowed_ip": "", "disabled": true})
	e.client().must(401, "POST", "/api/auth/signin", map[string]any{"email": "alice@example.com", "password": "new-password-1"})
	root.must(400, "PATCH", "/api/admin/accounts/1", map[string]any{"role": "user"}) // no self-demotion

	alice.must(204, "POST", "/api/auth/signout", nil)
	root.must(204, "POST", "/api/auth/signout", nil)
	root.must(401, "GET", "/api/account/sessions", nil)

	// Password hashes and emails never appear in public payloads.
	_, _, raw := e.client().do("GET", fmt.Sprintf("/api/accounts/%d", 1), nil)
	if strings.Contains(string(raw), "argon2") || strings.Contains(string(raw), "root@example.com") {
		t.Fatalf("public profile leaks private data: %s", raw)
	}
}

func TestSignInRateLimit(t *testing.T) {
	e := setup(t)
	c := e.client()
	last := 0
	for i := 0; i < 12; i++ {
		last, _, _ = c.do("POST", "/api/auth/signin", map[string]any{"email": "root@example.com", "password": "bad-password"})
	}
	if last != 429 {
		t.Fatalf("expected rate limiting, got %d", last)
	}
}

type judgeClient struct{ *client }

func (e *env) judge() judgeClient {
	c := e.client()
	c.hdr["Authorization"] = "Bearer " + judgeToken
	return judgeClient{c}
}

func TestProblemSubmissionAndJudgeFlow(t *testing.T) {
	e := setup(t)
	root := e.signIn("root@example.com", "root-password")
	alice, _ := e.signUp("alice")
	bob, _ := e.signUp("bob")
	guest := e.client()

	pid := int(root.must(201, "POST", "/api/admin/problems", map[string]any{"name": "A + B"})["id"].(float64))
	p := fmt.Sprintf("/api/problems/%d", pid)
	ap := fmt.Sprintf("/api/admin/problems/%d", pid)

	// New problems are hidden: invisible to everyone but administrators.
	alice.must(404, "GET", p, nil)
	guest.must(404, "GET", p, nil)
	alice.must(404, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": "int main(){}"})
	if n := len(items(alice.must(200, "GET", "/api/problems", nil))); n != 0 {
		t.Fatalf("hidden problem listed: %d", n)
	}

	// Testdata upload: names are validated, files land under server-chosen names.
	if status, raw := root.upload(ap+"/testcases", map[string]string{"../../evil.in": "x"}); status != 200 {
		t.Fatalf("basename upload: %d %s", status, raw) // path is stripped to evil.in
	}
	if status, _ := root.upload(ap+"/testcases", map[string]string{"evil.sh": "x"}); status != 400 {
		t.Fatalf("bad extension accepted: %d", status)
	}
	if status, _ := alice.upload(ap+"/testcases", map[string]string{"1.in": "x"}); status != 403 {
		t.Fatalf("non-admin upload: %d", status)
	}
	if status, raw := root.upload(ap+"/testcases", map[string]string{"1.in": "1 2\n", "1.out": "3\n", "2.in": "5 5\n", "2.out": "10\n"}); status != 200 {
		t.Fatalf("upload: %d %s", status, raw)
	}
	full := root.must(200, "GET", ap, nil)
	ids := map[string]int{}
	for _, tc := range full["testcases"].([]any) {
		m := tc.(map[string]any)
		ids[m["name"].(string)] = int(m["id"].(float64))
	}
	if len(ids) != 3 {
		t.Fatalf("testcases: %v", full["testcases"])
	}
	root.must(204, "DELETE", fmt.Sprintf("%s/testcases/%d", ap, ids["evil"]), nil)
	if entries, _ := os.ReadDir(e.srv.cfg.DataDir); len(entries) != 1 || entries[0].Name() != "problems" {
		t.Fatalf("unexpected files in data dir: %v", entries)
	}

	root.must(400, "PUT", ap+"/subtasks", []map[string]any{{"score": 50, "testcase_ids": []int{999999}, "depends": []int{}}})
	root.must(400, "PUT", ap+"/subtasks", []map[string]any{{"score": 50, "testcase_ids": []int{ids["1"]}, "depends": []int{2}}})
	root.must(204, "PUT", ap+"/subtasks", []map[string]any{
		{"score": 40, "testcase_ids": []int{ids["1"]}, "depends": []int{}},
		{"score": 60, "testcase_ids": []int{ids["2"]}, "depends": []int{1}},
	})
	root.must(400, "PATCH", ap, map[string]any{"tags": []string{"<script>"}})
	root.must(204, "PATCH", ap, map[string]any{"status": "online", "statement": "Add two numbers.", "tags": []string{"math"}, "languages": []string{"g++", "python3"}})

	// Attachments: HTML is never served inline.
	if status, raw := root.upload(ap+"/files", map[string]string{"note.html": "<script>alert(1)</script>", "pic.png": "png"}); status != 200 {
		t.Fatalf("file upload: %d %s", status, raw)
	}
	res, _ := guest.http.Get(e.url + p + "/files/note.html")
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || res.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("html attachment served inline: %v", res.Header)
	}
	guest.must(404, "GET", p+"/files/..%2f..%2ftestdata%2f1.in", nil)

	got := guest.must(200, "GET", p, nil)
	if got["statement"] != "Add two numbers." || len(got["files"].([]any)) != 2 {
		t.Fatalf("problem: %v", got)
	}

	// Submitting.
	guest.must(401, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": "x"})
	alice.must(400, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "java", "code": "x"})   // not allowed for the problem
	alice.must(400, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "cobol", "code": "x"})  // unknown
	alice.must(400, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": "  \n"}) // empty
	alice.must(400, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": strings.Repeat("a", maxCodeSize+1)})
	e.srv.cfg.SubmitCooldown = 60
	sid := int(alice.must(201, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": "secret source"})["id"].(float64))
	alice.must(429, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "g++", "code": "again"})
	e.srv.cfg.SubmitCooldown = 0
	sp := fmt.Sprintf("/api/submissions/%d", sid)

	// Source code: author and administrators only.
	if alice.must(200, "GET", sp, nil)["code"] != "secret source" {
		t.Fatal("author cannot read own code")
	}
	if v := bob.must(200, "GET", sp, nil); v["code"] != nil || v["can_rejudge"] != false {
		t.Fatalf("code leaked to another user: %v", v)
	}
	if guest.must(200, "GET", sp, nil)["code"] != nil {
		t.Fatal("code leaked to guest")
	}
	if root.must(200, "GET", sp, nil)["code"] != "secret source" {
		t.Fatal("admin cannot read code")
	}
	var audits int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE type = 'manage.submission.view'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("admin code view not audited: %d", audits)
	}

	// Judge API requires its token.
	guest.must(401, "POST", "/api/judge/claim", nil)
	alice.must(401, "POST", "/api/judge/claim", nil)
	j := e.judge()
	task := j.must(200, "POST", "/api/judge/claim", nil)
	if int(task["id"].(float64)) != sid || task["code"] != "secret source" {
		t.Fatalf("claim: %v", task)
	}
	status, _, raw := j.do("GET", fmt.Sprintf("/api/judge/testcases/%d/in", ids["1"]), nil)
	if status != 200 || string(raw) != "1 2\n" {
		t.Fatalf("judge testdata: %d %q", status, raw)
	}
	if status, _, _ := j.do("POST", "/api/judge/claim", nil); status != 204 {
		t.Fatalf("empty queue should be 204, got %d", status)
	}
	if alice.must(200, "GET", sp, nil)["state"].(float64) != 100 {
		t.Fatal("claimed submission should be judging")
	}
	rp := fmt.Sprintf("/api/judge/submissions/%d/result", sid)
	j.must(400, "POST", rp, map[string]any{"testcases": []map[string]any{{"id": 999999, "state": 1}}})
	j.must(400, "POST", rp, map[string]any{"testcases": []map[string]any{{"id": ids["1"], "state": 100}}})
	j.must(204, "POST", rp, map[string]any{"testcases": []map[string]any{
		{"id": ids["1"], "state": 1, "time_ms": 12, "memory_kb": 800, "message": "checker says ok"},
		{"id": ids["2"], "state": 3, "time_ms": 30, "memory_kb": 900},
	}})
	j.must(409, "POST", rp, map[string]any{"state": 9, "message": "late"})

	v := alice.must(200, "GET", sp, nil)
	if v["state"].(float64) != 2 || v["score"].(float64) != 40 || v["time_ms"].(float64) != 30 {
		t.Fatalf("scored result: %v", v)
	}
	tcs := v["result"].(map[string]any)["testcases"].([]any)
	if len(tcs) != 2 || tcs[0].(map[string]any)["message"] != "" {
		t.Fatalf("checker output must be hidden from the author: %v", tcs)
	}
	if root.must(200, "GET", sp, nil)["result"].(map[string]any)["testcases"].([]any)[0].(map[string]any)["message"] != "checker says ok" {
		t.Fatal("admin should see checker output")
	}

	// Rejudge: admin only; then a full solve.
	alice.must(404, "POST", sp+"/rejudge", nil)
	root.must(204, "POST", sp+"/rejudge", nil)
	j.must(200, "POST", "/api/judge/claim", nil)
	j.must(204, "POST", rp, map[string]any{"testcases": []map[string]any{
		{"id": ids["1"], "state": 1}, {"id": ids["2"], "state": 1},
	}})
	if v := alice.must(200, "GET", sp, nil); v["state"].(float64) != 1 || v["score"].(float64) != 100 {
		t.Fatalf("after rejudge: %v", v)
	}

	// Compile error path.
	sid2 := int(bob.must(201, "POST", "/api/submissions", map[string]any{"problem_id": pid, "language": "python3", "code": "print("})["id"].(float64))
	j.must(200, "POST", "/api/judge/claim", nil)
	j.must(204, "POST", fmt.Sprintf("/api/judge/submissions/%d/result", sid2), map[string]any{"state": 9, "message": "SyntaxError"})
	if v := bob.must(200, "GET", fmt.Sprintf("/api/submissions/%d", sid2), nil); v["state"].(float64) != 9 || v["message"] != "SyntaxError" {
		t.Fatalf("compile error: %v", v)
	}
	if alice.must(200, "GET", fmt.Sprintf("/api/submissions/%d", sid2), nil)["message"] != "" {
		t.Fatal("compiler output leaked to another user")
	}

	// Lists, stats, rank.
	if n := len(items(guest.must(200, "GET", fmt.Sprintf("/api/submissions?problem_id=%d&state=1", pid), nil))); n != 1 {
		t.Fatalf("filtered submissions: %d", n)
	}
	guest.must(400, "GET", "/api/submissions?state=abc", nil)
	stats := alice.must(200, "GET", p, nil)["stats"].(map[string]any)
	if stats["solved_users"].(float64) != 1 || stats["tried_users"].(float64) != 2 || stats["my_state"].(float64) != 1 {
		t.Fatalf("stats: %v", stats)
	}
	rank := items(guest.must(200, "GET", "/api/rank", nil))
	if rank[0].(map[string]any)["name"] != "alice" || rank[0].(map[string]any)["rate"].(float64) != 100 {
		t.Fatalf("rank: %v", rank)
	}
	if n := len(items(guest.must(200, "GET", p+"/rank", nil))); n != 1 {
		t.Fatalf("problem rank: %d", n)
	}

	// Hiding the problem again hides its submissions from other users.
	root.must(204, "PATCH", ap, map[string]any{"status": "hidden"})
	bob.must(404, "GET", sp, nil)
	alice.must(200, "GET", sp, nil)
	if n := len(items(bob.must(200, "GET", "/api/submissions", nil))); n != 0 {
		t.Fatalf("hidden problem submissions listed: %d", n)
	}
	guest.must(404, "GET", p+"/files/pic.png", nil)

	root.must(204, "DELETE", ap, nil)
	if _, err := os.Stat(e.srv.problemDir(pid, "")); !os.IsNotExist(err) {
		t.Fatal("problem directory not removed")
	}
}

func TestContestFlow(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	root := e.signIn("root@example.com", "root-password")
	alice, aliceID := e.signUp("alice")
	bob, bobID := e.signUp("bob")
	carol, carolID := e.signUp("carol")
	guest := e.client()
	j := e.judge()

	alice.must(403, "POST", "/api/contests", map[string]any{"name": "x"})
	cid := int(root.must(201, "POST", "/api/contests", map[string]any{"name": "Round 1"})["id"].(float64))
	cp := fmt.Sprintf("/api/contests/%d", cid)

	// Two problems: one contest-only, one hidden.
	mk := func(name, status string) int {
		id := int(root.must(201, "POST", "/api/admin/problems", map[string]any{"name": name})["id"].(float64))
		root.must(204, "PATCH", fmt.Sprintf("/api/admin/problems/%d", id), map[string]any{"status": status})
		return id
	}
	p1, hidden, online := mk("Contest task", "contest"), mk("Secret", "hidden"), mk("Public", "online")
	if status, _ := root.upload(fmt.Sprintf("/api/admin/problems/%d/testcases", p1), map[string]string{"1.in": "", "1.out": ""}); status != 200 {
		t.Fatal("upload")
	}
	if status, _ := root.upload(fmt.Sprintf("/api/admin/problems/%d/files", p1), map[string]string{"fig.png": "png"}); status != 200 {
		t.Fatal("upload file")
	}

	now := time.Now()
	root.must(400, "PATCH", cp, map[string]any{"start_at": now, "end_at": now.Add(-time.Hour)})
	root.must(204, "PATCH", cp, map[string]any{
		"start_at": now.Add(time.Hour), "end_at": now.Add(3 * time.Hour), "reg_end_at": now.Add(30 * time.Minute),
		"reg_mode": "approval", "cooldown_sec": 0, "public_scoreboard": true, "allow_other_pages": false,
		"desc_before": "BEFORE", "desc_during": "DURING", "desc_after": "AFTER",
	})
	root.must(204, "PUT", cp+"/manage/problems", []map[string]any{
		{"problem_id": p1, "score_type": "best", "result_style": "total"},
		{"problem_id": hidden, "score_type": "best", "result_style": "full"},
	})

	// Management is closed to non-administrators of the contest.
	alice.must(403, "GET", cp+"/manage", nil)
	alice.must(403, "PATCH", cp, map[string]any{"name": "hacked"})
	alice.must(403, "PUT", cp+"/manage/problems", []map[string]any{})
	alice.must(403, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, aliceID), map[string]any{"status": "admin"})
	guest.must(401, "GET", cp+"/manage", nil)

	// Registration with approval.
	guest.must(401, "POST", cp+"/register", nil)
	if alice.must(200, "POST", cp+"/register", nil)["status"] != "requested" {
		t.Fatal("approval contests should queue registrations")
	}
	alice.must(403, "POST", cp+"/register", nil)
	bob.must(200, "POST", cp+"/register", nil)
	bob.must(204, "DELETE", cp+"/register", nil)
	bob.must(200, "POST", cp+"/register", nil)
	root.must(204, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, aliceID), map[string]any{"status": "approved"})
	root.must(204, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, bobID), map[string]any{"status": "approved"})
	root.must(400, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, 999999), map[string]any{"status": "approved"})

	// A contest manager who is not a site admin cannot pull in hidden problems or mint managers.
	root.must(204, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, carolID), map[string]any{"status": "admin"})
	carol.must(200, "GET", cp+"/manage", nil)
	other := mk("Other secret", "hidden")
	carol.must(400, "PUT", cp+"/manage/problems", []map[string]any{{"problem_id": other, "score_type": "best", "result_style": "full"}})
	carol.must(403, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, bobID), map[string]any{"status": "admin"})
	carol.must(403, "DELETE", fmt.Sprintf("%s/manage/users/%d", cp, 1), nil)
	carol.must(403, "DELETE", cp, nil)

	// Before the start: nothing is visible to contestants.
	info := alice.must(200, "GET", cp, nil)
	if info["description"] != "BEFORE" || info["access"].(map[string]any)["phase"] != "before" {
		t.Fatalf("info before: %v", info)
	}
	alice.must(403, "GET", cp+"/problems", nil)
	alice.must(403, "GET", fmt.Sprintf("%s/problems/%d", cp, p1), nil)
	alice.must(403, "GET", cp+"/scoreboard", nil)
	alice.must(403, "POST", "/api/submissions", map[string]any{"problem_id": p1, "contest_id": cid, "language": "g++", "code": "x"})
	alice.must(404, "GET", fmt.Sprintf("/api/problems/%d/files/fig.png", p1), nil)
	alice.must(404, "GET", fmt.Sprintf("/api/problems/%d", p1), nil) // contest problems are not on the public site

	// Start the contest.
	if _, err := e.db.Exec(ctx, `UPDATE contests SET start_at = now() - interval '1 hour', reg_end_at = now() - interval '2 hours' WHERE id = $1`, cid); err != nil {
		t.Fatal(err)
	}
	info = alice.must(200, "GET", cp, nil)
	if info["description"] != "DURING" || info["access"].(map[string]any)["can_submit"] != true {
		t.Fatalf("info during: %v", info)
	}
	_, _, raw := alice.do("GET", cp+"/problems", nil)
	var listed []map[string]any
	json.Unmarshal(raw, &listed)
	if len(listed) != 1 || int(listed[0]["id"].(float64)) != p1 {
		t.Fatalf("contestants must not see hidden problems: %s", raw)
	}
	alice.must(200, "GET", fmt.Sprintf("%s/problems/%d", cp, p1), nil)
	alice.must(404, "GET", fmt.Sprintf("%s/problems/%d", cp, hidden), nil)
	alice.must(404, "GET", fmt.Sprintf("%s/problems/%d", cp, online), nil)
	alice.must(200, "GET", fmt.Sprintf("/api/problems/%d/files/fig.png", p1), nil)
	guest.must(403, "GET", cp+"/problems", nil)
	guest.must(404, "GET", fmt.Sprintf("/api/problems/%d/files/fig.png", p1), nil)
	dave, _ := e.signUp("dave") // not registered, registration closed
	dave.must(403, "POST", cp+"/register", nil)
	dave.must(403, "GET", cp+"/problems", nil)
	dave.must(403, "POST", "/api/submissions", map[string]any{"problem_id": p1, "contest_id": cid, "language": "g++", "code": "x"})

	// Lockdown: contestants are confined to the contest while it runs.
	lock := alice.must(200, "GET", "/api/session", nil)
	if int(lock["lockdown"].(float64)) != cid {
		t.Fatalf("lockdown not reported: %v", lock)
	}
	alice.must(403, "GET", "/api/problems", nil)
	alice.must(403, "GET", fmt.Sprintf("/api/problems/%d", online), nil)
	alice.must(403, "GET", "/api/submissions", nil)
	alice.must(403, "POST", "/api/submissions", map[string]any{"problem_id": online, "language": "g++", "code": "x"})
	dave.must(200, "GET", "/api/problems", nil)

	// Submissions inside the contest.
	alice.must(404, "POST", "/api/submissions", map[string]any{"problem_id": hidden, "contest_id": cid, "language": "g++", "code": "x"})
	alice.must(404, "POST", "/api/submissions", map[string]any{"problem_id": online, "contest_id": cid, "language": "g++", "code": "x"})
	sa := int(alice.must(201, "POST", "/api/submissions", map[string]any{"problem_id": p1, "contest_id": cid, "language": "g++", "code": "alice code"})["id"].(float64))
	sb := int(bob.must(201, "POST", "/api/submissions", map[string]any{"problem_id": p1, "contest_id": cid, "language": "g++", "code": "bob code"})["id"].(float64))
	var tid int
	e.db.QueryRow(ctx, `SELECT id FROM testcases WHERE problem_id = $1`, p1).Scan(&tid)
	for _, verdict := range []struct{ id, state int }{{sa, 1}, {sb, 3}} {
		task := j.must(200, "POST", "/api/judge/claim", nil)
		if int(task["id"].(float64)) != verdict.id {
			t.Fatalf("queue order: %v", task["id"])
		}
		j.must(204, "POST", fmt.Sprintf("/api/judge/submissions/%d/result", verdict.id),
			map[string]any{"testcases": []map[string]any{{"id": tid, "state": verdict.state}}})
	}

	// While running, contestants see only their own submissions, with the configured detail level.
	bob.must(404, "GET", fmt.Sprintf("/api/submissions/%d", sa), nil)
	guest.must(404, "GET", fmt.Sprintf("/api/submissions/%d", sa), nil)
	dave.must(404, "GET", fmt.Sprintf("/api/submissions/%d", sa), nil)
	own := alice.must(200, "GET", fmt.Sprintf("/api/submissions/%d", sa), nil)
	if own["result_style"] != "total" || own["result"].(map[string]any)["testcases"] != nil || own["code"] != "alice code" {
		t.Fatalf("result style not applied: %v", own)
	}
	if n := len(items(bob.must(200, "GET", fmt.Sprintf("/api/submissions?contest_id=%d", cid), nil))); n != 1 {
		t.Fatalf("bob sees %d contest submissions", n)
	}
	if n := len(items(bob.must(200, "GET", fmt.Sprintf("/api/submissions?contest_id=%d&account_id=%d", cid, aliceID), nil))); n != 0 {
		t.Fatal("account filter bypassed the contest scope")
	}
	if n := len(items(guest.must(200, "GET", fmt.Sprintf("/api/submissions?contest_id=%d", cid), nil))); n != 0 {
		t.Fatal("guest sees contest submissions")
	}
	if n := len(items(dave.must(200, "GET", "/api/submissions", nil))); n != 0 {
		t.Fatal("contest submissions leak into the public list")
	}
	if n := len(items(carol.must(200, "GET", fmt.Sprintf("/api/submissions?contest_id=%d", cid), nil))); n != 2 {
		t.Fatalf("contest admin sees %d", n)
	}
	carol.must(204, "POST", fmt.Sprintf("/api/submissions/%d/rejudge", sb), nil)
	bob.must(404, "POST", fmt.Sprintf("/api/submissions/%d/rejudge", sb), nil)
	j.must(200, "POST", "/api/judge/claim", nil)
	j.must(204, "POST", fmt.Sprintf("/api/judge/submissions/%d/result", sb), map[string]any{"testcases": []map[string]any{{"id": tid, "state": 3}}})

	board := guest.must(200, "GET", cp+"/scoreboard", nil)
	rows := board["rows"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["name"] != "alice" || rows[0].(map[string]any)["total"].(float64) != 100 {
		t.Fatalf("scoreboard: %v", board)
	}

	// Questions and announcements.
	dave.must(403, "POST", cp+"/questions", map[string]any{"subject": "s", "content": "c"})
	alice.must(201, "POST", cp+"/questions", map[string]any{"subject": "Clarify", "content": "Is n > 0?"})
	bob.must(403, "POST", cp+"/manage/announcements", map[string]any{"subject": "s", "content": "c"})
	root.must(201, "POST", cp+"/manage/announcements", map[string]any{"subject": "Note", "content": "n > 0"})
	if q := bob.must(200, "GET", cp+"/qa", nil); len(q["questions"].([]any)) != 0 || len(q["announcements"].([]any)) != 1 {
		t.Fatalf("bob sees someone else's question: %v", q)
	}
	qa := carol.must(200, "GET", cp+"/qa", nil)
	qid := int(qa["questions"].([]any)[0].(map[string]any)["id"].(float64))
	bob.must(403, "POST", fmt.Sprintf("%s/manage/questions/%d/reply", cp, qid), map[string]any{"reply": "x"})
	carol.must(204, "POST", fmt.Sprintf("%s/manage/questions/%d/reply", cp, qid), map[string]any{"reply": "Yes"})
	if alice.must(200, "GET", cp, nil)["unread"].(float64) != 2 {
		t.Fatal("unread counter")
	}
	alice.must(200, "GET", cp+"/qa", nil)
	if alice.must(200, "GET", cp, nil)["unread"].(float64) != 0 {
		t.Fatal("unread counter not cleared")
	}

	// After the end: results open up, submitting closes, lockdown lifts.
	if _, err := e.db.Exec(ctx, `UPDATE contests SET start_at = now() - interval '3 hours', end_at = now() - interval '1 minute' WHERE id = $1`, cid); err != nil {
		t.Fatal(err)
	}
	alice.must(403, "POST", "/api/submissions", map[string]any{"problem_id": p1, "contest_id": cid, "language": "g++", "code": "late"})
	alice.must(200, "GET", "/api/problems", nil)
	other2 := bob.must(200, "GET", fmt.Sprintf("/api/submissions/%d", sa), nil)
	if other2["code"] != nil || other2["result_style"] != "full" {
		t.Fatalf("after the contest: %v", other2)
	}
	if alice.must(200, "GET", cp, nil)["description"] != "AFTER" {
		t.Fatal("description after")
	}
	guest.must(200, "GET", cp+"/problems", nil)
	guest.must(404, "GET", fmt.Sprintf("%s/problems/%d", cp, hidden), nil)

	if n := len(items(root.must(200, "GET", cp+"/manage/logs", nil))); n == 0 {
		t.Fatal("contest audit log is empty")
	}
	alice.must(403, "GET", cp+"/manage/logs", nil)
	root.must(204, "DELETE", cp, nil)
	guest.must(404, "GET", cp, nil)
}

func TestFlexibleContestSession(t *testing.T) {
	e := setup(t)
	root := e.signIn("root@example.com", "root-password")
	alice, aliceID := e.signUp("alice")
	cid := int(root.must(201, "POST", "/api/contests", map[string]any{"name": "Flex"})["id"].(float64))
	cp := fmt.Sprintf("/api/contests/%d", cid)
	now := time.Now()
	root.must(400, "PATCH", cp, map[string]any{"time_mode": "flexible"}) // needs a duration
	root.must(204, "PATCH", cp, map[string]any{"time_mode": "flexible", "duration_sec": 3600,
		"start_at": now.Add(-time.Hour), "end_at": now.Add(30 * time.Minute)})
	root.must(204, "PUT", fmt.Sprintf("%s/manage/users/%d", cp, aliceID), map[string]any{"status": "approved"})

	alice.must(403, "GET", cp+"/problems", nil)
	out := alice.must(200, "POST", cp+"/start", nil)
	start, _ := time.Parse(time.RFC3339Nano, out["start"].(string))
	end, _ := time.Parse(time.RFC3339Nano, out["end"].(string))
	if d := end.Sub(start); d < 29*time.Minute || d > 31*time.Minute {
		t.Fatalf("session must be clipped to the contest end, got %v", d)
	}
	alice.must(403, "POST", cp+"/start", nil) // cannot restart
	alice.must(200, "GET", cp+"/problems", nil)
	root.must(204, "POST", fmt.Sprintf("%s/manage/users/%d/reset-session", cp, aliceID), nil)
	alice.must(403, "GET", cp+"/problems", nil)
}

func TestSiteFeatures(t *testing.T) {
	e := setup(t)
	root := e.signIn("root@example.com", "root-password")
	alice, aliceID := e.signUp("alice")
	bob, _ := e.signUp("bob")
	guest := e.client()

	// Bulletins.
	alice.must(403, "POST", "/api/admin/bulletins", map[string]any{"title": "t", "content": "c"})
	root.must(400, "POST", "/api/admin/bulletins", map[string]any{"title": "t", "content": "c", "color": "url(x)"})
	bid := int(root.must(201, "POST", "/api/admin/bulletins", map[string]any{"title": "Hello", "content": "**hi**", "color": "red", "pinned": true})["id"].(float64))
	guest.must(200, "GET", fmt.Sprintf("/api/bulletins/%d", bid), nil)
	root.must(204, "PUT", fmt.Sprintf("/api/admin/bulletins/%d", bid), map[string]any{"title": "Hello 2", "content": "x"})
	root.must(204, "DELETE", fmt.Sprintf("/api/admin/bulletins/%d", bid), nil)
	guest.must(404, "GET", fmt.Sprintf("/api/bulletins/%d", bid), nil)

	// Problem classes: private classes stay private, only owners may edit.
	alice.must(403, "POST", "/api/classes", map[string]any{"name": "Official", "problem_ids": []int{1}, "official": true})
	cls := int(alice.must(201, "POST", "/api/classes", map[string]any{"name": "Mine", "problem_ids": []int{3, 1, 3}, "public": false})["id"].(float64))
	bob.must(404, "GET", fmt.Sprintf("/api/classes/%d", cls), nil)
	bob.must(404, "PUT", fmt.Sprintf("/api/classes/%d", cls), map[string]any{"name": "Stolen", "problem_ids": []int{1}, "public": true})
	bob.must(404, "DELETE", fmt.Sprintf("/api/classes/%d", cls), nil)
	bob.must(204, "PUT", fmt.Sprintf("/api/classes/%d/collect", cls), nil) // silently ignored
	var collected int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM class_collections`).Scan(&collected)
	if collected != 0 {
		t.Fatal("collected an invisible class")
	}
	alice.must(204, "PUT", fmt.Sprintf("/api/classes/%d", cls), map[string]any{"name": "Mine", "problem_ids": []int{1}, "public": true})
	bob.must(204, "PUT", fmt.Sprintf("/api/classes/%d/collect", cls), nil)
	if bob.must(200, "GET", fmt.Sprintf("/api/classes/%d", cls), nil)["collected"] != true {
		t.Fatal("collect")
	}
	alice.must(204, "DELETE", fmt.Sprintf("/api/classes/%d", cls), nil)

	// Questions: private to the asker.
	qid := int(alice.must(201, "POST", "/api/questions", map[string]any{"content": "Help?"})["id"].(float64))
	bob.must(404, "DELETE", fmt.Sprintf("/api/questions/%d", qid), nil)
	bob.must(403, "POST", fmt.Sprintf("/api/admin/questions/%d/reply", qid), map[string]any{"reply": "x"})
	root.must(204, "POST", fmt.Sprintf("/api/admin/questions/%d/reply", qid), map[string]any{"reply": "Sure"})
	if alice.must(200, "GET", "/api/session", nil)["account"].(map[string]any)["unread_replies"].(float64) != 1 {
		t.Fatal("unread reply counter")
	}
	_, _, raw := alice.do("GET", "/api/questions", nil)
	if !strings.Contains(string(raw), "Sure") {
		t.Fatalf("questions: %s", raw)
	}
	_, _, raw = bob.do("GET", "/api/questions", nil)
	if strings.Contains(string(raw), "Help?") {
		t.Fatal("question leaked to another account")
	}

	// Boards.
	now := time.Now()
	board := map[string]any{"name": "Class A", "visible": false, "start_at": now.Add(-time.Hour), "end_at": now.Add(time.Hour),
		"problem_ids": []int{1}, "account_ids": []int{aliceID}}
	alice.must(403, "POST", "/api/admin/boards", board)
	bd := int(root.must(201, "POST", "/api/admin/boards", board)["id"].(float64))
	guest.must(404, "GET", fmt.Sprintf("/api/boards/%d", bd), nil)
	board["visible"] = true
	root.must(204, "PUT", fmt.Sprintf("/api/admin/boards/%d", bd), board)
	if rows := guest.must(200, "GET", fmt.Sprintf("/api/boards/%d", bd), nil)["rows"].([]any); len(rows) != 1 {
		t.Fatalf("board rows: %v", rows)
	}

	// Audit log and dashboard are admin-only and record sign-ins.
	alice.must(403, "GET", "/api/admin/logs", nil)
	if n := len(items(root.must(200, "GET", "/api/admin/logs?type=signin", nil))); n == 0 {
		t.Fatal("sign-ins are not audited")
	}
	root.must(200, "GET", "/api/admin/dashboard", nil)
	guest.must(404, "GET", "/api/nope", nil)
}

func mustURL(raw string) *urlT { u, _ := parseURL(raw); return u }

type urlT = url.URL

var parseURL = url.Parse

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }
