// Package api implements the NTOJ HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kuohuanhuan/ntoj/backend/internal/config"
)

type Server struct {
	db      *pgxpool.Pool
	cfg     config.Config
	log     *slog.Logger
	limiter *limiter
	now     func() time.Time
}

func New(db *pgxpool.Pool, cfg config.Config, log *slog.Logger) *Server {
	return &Server{db: db, cfg: cfg, log: log, limiter: newLimiter(), now: time.Now}
}

// Error is an API error rendered as {"error": {"code", "message", ...}}.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Extra   map[string]any `json:"extra,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func badRequest(format string, a ...any) *Error {
	return &Error{Status: 400, Code: "bad_request", Message: fmt.Sprintf(format, a...)}
}

var (
	errUnauthorized = &Error{Status: 401, Code: "unauthorized", Message: "Sign in required"}
	errForbidden    = &Error{Status: 403, Code: "forbidden", Message: "Permission denied"}
	errNotFound     = &Error{Status: 404, Code: "not_found", Message: "Not found"}
	errTooMany      = &Error{Status: 429, Code: "rate_limited", Message: "Too many requests, please slow down"}
)

func conflict(msg string) *Error { return &Error{Status: 409, Code: "conflict", Message: msg} }

// Account is the authenticated caller.
type Account struct {
	ID        int
	Name      string
	Role      string
	SessionID int64
}

func (a *Account) Admin() bool { return a != nil && a.Role == "admin" }

// Ctx carries one request through a handler.
type Ctx struct {
	context.Context
	s    *Server
	w    http.ResponseWriter
	r    *http.Request
	acct *Account // nil for guests
	ip   string
}

func (c *Ctx) acctID() int {
	if c.acct == nil {
		return 0
	}
	return c.acct.ID
}

func (c *Ctx) json(status int, v any) error {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.Header().Set("Cache-Control", "no-store")
	c.w.WriteHeader(status)
	return json.NewEncoder(c.w).Encode(v)
}

func (c *Ctx) ok(v any) error { return c.json(http.StatusOK, v) }

func (c *Ctx) noContent() error {
	c.w.WriteHeader(http.StatusNoContent)
	return nil
}

const maxJSONBody = 1 << 20

// bind decodes a JSON request body, rejecting unknown fields and oversized
// or non-JSON payloads.
func (c *Ctx) bind(v any) error {
	mt, _, _ := mime.ParseMediaType(c.r.Header.Get("Content-Type"))
	if mt != "application/json" {
		return &Error{Status: 415, Code: "unsupported_media_type", Message: "Content-Type must be application/json"}
	}
	dec := json.NewDecoder(http.MaxBytesReader(c.w, c.r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &Error{Status: 413, Code: "too_large", Message: "Request body too large"}
		}
		return badRequest("Invalid JSON body")
	}
	if _, err := dec.Token(); err != io.EOF {
		return badRequest("Invalid JSON body")
	}
	return nil
}

func (c *Ctx) pathInt(name string) (int, error) {
	n, err := strconv.Atoi(c.r.PathValue(name))
	if err != nil || n <= 0 || n > 1<<31-1 {
		return 0, errNotFound
	}
	return n, nil
}

// queryInt parses an optional positive integer query parameter.
func (c *Ctx) queryInt(name string) (int, bool, error) {
	v := c.r.URL.Query().Get(name)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 1<<31-1 {
		return 0, false, badRequest("Invalid %s", name)
	}
	return n, true, nil
}

// page returns LIMIT and OFFSET from ?page=&per_page=.
func (c *Ctx) page(defPer int) (limit, offset, page int) {
	q := c.r.URL.Query()
	page, _ = strconv.Atoi(q.Get("page"))
	if page < 1 || page > 1_000_000 {
		page = 1
	}
	limit, _ = strconv.Atoi(q.Get("per_page"))
	if limit < 1 || limit > 100 {
		limit = defPer
	}
	return limit, (page - 1) * limit, page
}

type paged struct {
	Items   json.RawMessage `json:"items"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
}

// jsonRow runs query and returns its single row as a JSON object.
func (s *Server) jsonRow(ctx context.Context, query string, args ...any) (json.RawMessage, error) {
	var out []byte
	err := s.db.QueryRow(ctx, "SELECT to_jsonb(t) FROM ("+query+") t", args...).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNotFound
	}
	return out, err
}

// jsonList runs query and returns all rows as a JSON array, in query order.
func (s *Server) jsonList(ctx context.Context, query string, args ...any) (json.RawMessage, error) {
	var out []byte
	err := s.db.QueryRow(ctx,
		"SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY t._n) , '[]'::jsonb) FROM (SELECT q.*, row_number() OVER () AS _n FROM ("+query+") q) t",
		args...).Scan(&out)
	return out, err
}

// audit records a security-relevant action. Failures are logged, not returned.
func (c *Ctx) audit(typ, message string, params map[string]any, contestID *int) {
	if params == nil {
		params = map[string]any{}
	}
	var op *int
	if c.acct != nil {
		op = &c.acct.ID
	}
	_, err := c.s.db.Exec(context.WithoutCancel(c), `INSERT INTO audit_logs (type, message, params, operator_id, operator_ip, contest_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, typ, message, params, op, c.ip, contestID)
	if err != nil {
		c.s.log.Error("audit log failed", "type", typ, "err", err)
	}
}

type level int

const (
	guest level = iota
	user
	admin
)

type handler func(c *Ctx) error

// lockable marks routes that a contestant may not use while taking part in a
// running contest that forbids browsing the rest of the site.
type routeOpt struct{ lockable bool }

func (s *Server) wrap(min level, opt routeOpt, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Ctx{Context: r.Context(), s: s, w: w, r: r, ip: s.clientIP(r)}
		err := func() error {
			acct, err := s.authenticate(c)
			if err != nil {
				return err
			}
			c.acct = acct
			if min >= user && acct == nil {
				return errUnauthorized
			}
			if min >= admin && !acct.Admin() {
				return errForbidden
			}
			if opt.lockable {
				if err := c.checkLockdown(); err != nil {
					return err
				}
			}
			return h(c)
		}()
		if err != nil {
			s.writeError(c, err)
		}
	}
}

func (s *Server) writeError(c *Ctx, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			apiErr = &Error{Status: 413, Code: "too_large", Message: "Request body too large"}
		case errors.Is(err, context.Canceled):
			return
		default:
			s.log.Error("request failed", "method", c.r.Method, "path", c.r.URL.Path, "err", err)
			apiErr = &Error{Status: 500, Code: "internal", Message: "Internal server error"}
		}
	}
	c.json(apiErr.Status, map[string]any{"error": apiErr})
}

// clientIP returns the peer address, honouring X-Forwarded-For only when the
// direct peer is a configured trusted proxy.
func (s *Server) clientIP(r *http.Request) string {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	ip := ap.Addr().Unmap()
	if !s.trusted(ip) {
		return ip.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		ip = hop.Unmap()
		if !s.trusted(ip) {
			break
		}
	}
	return ip.String()
}

func (s *Server) trusted(ip netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Handler builds the full HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(&Ctx{Context: r.Context(), s: s, w: w, r: r}, errNotFound)
	})

	// Cross-site state-changing requests are rejected outright: the session
	// cookie is the only credential, so this is the CSRF boundary.
	csrf := http.NewCrossOriginProtection()
	for _, o := range s.cfg.AllowedOrigins {
		if err := csrf.AddTrustedOrigin(o); err != nil {
			s.log.Error("invalid ALLOWED_ORIGINS entry", "origin", o, "err", err)
		}
	}
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.writeError(&Ctx{Context: r.Context(), s: s, w: w, r: r},
			&Error{Status: 403, Code: "cross_origin", Message: "Cross-origin request rejected"})
	}))

	var h http.Handler = mux
	h = csrf.Handler(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		hd.Set("Cross-Origin-Resource-Policy", "same-origin")
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "path", r.URL.Path, "value", v, "stack", string(debug.Stack()))
				http.Error(w, `{"error":{"code":"internal","message":"Internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

func (s *Server) routes(mux *http.ServeMux) {
	free := routeOpt{}
	lock := routeOpt{lockable: true}
	g := func(pattern string, min level, opt routeOpt, h handler) { mux.HandleFunc(pattern, s.wrap(min, opt, h)) }

	// Session and own account.
	g("GET /api/session", guest, free, s.getSession)
	g("POST /api/auth/signup", guest, free, s.signUp)
	g("POST /api/auth/signin", guest, free, s.signIn)
	g("POST /api/auth/signout", guest, free, s.signOut)
	g("GET /api/account/sessions", user, free, s.listSessions)
	g("DELETE /api/account/sessions/{id}", user, free, s.revokeSession)
	g("DELETE /api/account/sessions", user, free, s.revokeOtherSessions)
	g("PATCH /api/account", user, free, s.updateProfile)
	g("POST /api/account/password", user, free, s.changePassword)

	// Public site.
	g("GET /api/accounts/{id}", guest, lock, s.getAccount)
	g("GET /api/rank", guest, lock, s.userRank)
	g("GET /api/bulletins", guest, free, s.listBulletins)
	g("GET /api/bulletins/{id}", guest, free, s.getBulletin)
	g("GET /api/tags", guest, lock, s.listTags)
	g("GET /api/problems", guest, lock, s.listProblems)
	g("GET /api/problems/{id}", guest, lock, s.getProblem)
	g("GET /api/problems/{id}/rank", guest, lock, s.problemRank)
	g("GET /api/problems/{id}/files/{name}", guest, free, s.getProblemFile)
	g("POST /api/submissions", user, free, s.submit)
	g("GET /api/submissions", guest, free, s.listSubmissions)
	g("GET /api/submissions/{id}", guest, free, s.getSubmission)
	g("POST /api/submissions/{id}/rejudge", user, free, s.rejudgeSubmission)
	g("GET /api/classes", guest, lock, s.listClasses)
	g("GET /api/classes/{id}", guest, lock, s.getClass)
	g("POST /api/classes", user, lock, s.createClass)
	g("PUT /api/classes/{id}", user, lock, s.updateClass)
	g("DELETE /api/classes/{id}", user, lock, s.deleteClass)
	g("PUT /api/classes/{id}/collect", user, lock, s.collectClass)
	g("DELETE /api/classes/{id}/collect", user, lock, s.uncollectClass)
	g("GET /api/boards", guest, lock, s.listBoards)
	g("GET /api/boards/{id}", guest, lock, s.getBoard)
	g("GET /api/questions", user, free, s.listQuestions)
	g("POST /api/questions", user, free, s.askQuestion)
	g("DELETE /api/questions/{id}", user, free, s.deleteQuestion)

	// Contests.
	g("GET /api/contests", guest, free, s.listContests)
	g("POST /api/contests", admin, free, s.createContest)
	g("GET /api/contests/{id}", guest, free, s.getContest)
	g("PATCH /api/contests/{id}", user, free, s.updateContest)
	g("DELETE /api/contests/{id}", admin, free, s.deleteContest)
	g("POST /api/contests/{id}/register", user, free, s.registerContest)
	g("DELETE /api/contests/{id}/register", user, free, s.withdrawContest)
	g("POST /api/contests/{id}/start", user, free, s.startContestSession)
	g("GET /api/contests/{id}/problems", guest, free, s.listContestProblems)
	g("GET /api/contests/{id}/problems/{pid}", guest, free, s.getContestProblem)
	g("GET /api/contests/{id}/scoreboard", guest, free, s.contestScoreboard)
	g("GET /api/contests/{id}/qa", guest, free, s.contestQA)
	g("POST /api/contests/{id}/questions", user, free, s.askContestQuestion)
	g("GET /api/contests/{id}/manage", user, free, s.manageContest)
	g("PUT /api/contests/{id}/manage/problems", user, free, s.setContestProblems)
	g("PUT /api/contests/{id}/manage/users/{aid}", user, free, s.setContestUser)
	g("DELETE /api/contests/{id}/manage/users/{aid}", user, free, s.removeContestUser)
	g("POST /api/contests/{id}/manage/users/{aid}/reset-session", user, free, s.resetContestSession)
	g("POST /api/contests/{id}/manage/announcements", user, free, s.createAnnouncement)
	g("PUT /api/contests/{id}/manage/announcements/{nid}", user, free, s.updateAnnouncement)
	g("DELETE /api/contests/{id}/manage/announcements/{nid}", user, free, s.deleteAnnouncement)
	g("POST /api/contests/{id}/manage/questions/{qid}/reply", user, free, s.replyContestQuestion)
	g("POST /api/contests/{id}/manage/rejudge", user, free, s.rejudgeContest)
	g("GET /api/contests/{id}/manage/logs", user, free, s.contestLogs)

	// Site administration.
	g("GET /api/admin/dashboard", admin, free, s.adminDashboard)
	g("GET /api/admin/accounts", admin, free, s.adminListAccounts)
	g("PATCH /api/admin/accounts/{id}", admin, free, s.adminUpdateAccount)
	g("POST /api/admin/accounts/{id}/password", admin, free, s.adminResetPassword)
	g("GET /api/admin/problems", admin, free, s.adminListProblems)
	g("POST /api/admin/problems", admin, free, s.adminCreateProblem)
	g("GET /api/admin/problems/{id}", admin, free, s.adminGetProblem)
	g("PATCH /api/admin/problems/{id}", admin, free, s.adminUpdateProblem)
	g("DELETE /api/admin/problems/{id}", admin, free, s.adminDeleteProblem)
	g("POST /api/admin/problems/{id}/testcases", admin, free, s.adminUploadTestcases)
	g("DELETE /api/admin/problems/{id}/testcases/{tid}", admin, free, s.adminDeleteTestcase)
	g("GET /api/admin/problems/{id}/testcases/{tid}/{kind}", admin, free, s.adminDownloadTestcase)
	g("PUT /api/admin/problems/{id}/subtasks", admin, free, s.adminSetSubtasks)
	g("POST /api/admin/problems/{id}/files", admin, free, s.adminUploadFiles)
	g("DELETE /api/admin/problems/{id}/files/{name}", admin, free, s.adminDeleteFile)
	g("POST /api/admin/problems/{id}/rejudge", admin, free, s.adminRejudgeProblem)
	g("POST /api/admin/bulletins", admin, free, s.adminCreateBulletin)
	g("PUT /api/admin/bulletins/{id}", admin, free, s.adminUpdateBulletin)
	g("DELETE /api/admin/bulletins/{id}", admin, free, s.adminDeleteBulletin)
	g("GET /api/admin/boards", admin, free, s.adminListBoards)
	g("POST /api/admin/boards", admin, free, s.adminCreateBoard)
	g("PUT /api/admin/boards/{id}", admin, free, s.adminUpdateBoard)
	g("DELETE /api/admin/boards/{id}", admin, free, s.adminDeleteBoard)
	g("GET /api/admin/questions", admin, free, s.adminListQuestions)
	g("POST /api/admin/questions/{id}/reply", admin, free, s.adminReplyQuestion)
	g("GET /api/admin/logs", admin, free, s.adminLogs)

	// Judge workers (bearer token, no cookies).
	mux.HandleFunc("POST /api/judge/claim", s.judge(s.judgeClaim))
	mux.HandleFunc("POST /api/judge/submissions/{id}/result", s.judge(s.judgeResult))
	mux.HandleFunc("GET /api/judge/testcases/{tid}/{kind}", s.judge(s.judgeTestcase))

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
}
