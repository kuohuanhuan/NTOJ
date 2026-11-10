package api

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kuohuanhuan/ntoj/backend/internal/auth"
)

const (
	sessionCookie = "ntoj_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// Languages a submission may use. The judge decides how to build each one.
var Languages = []map[string]string{
	{"id": "gcc", "name": "C (GCC, GNU11)"},
	{"id": "clang", "name": "C (Clang, C11)"},
	{"id": "g++", "name": "C++ (G++, GNU++17)"},
	{"id": "clang++", "name": "C++ (Clang++, C++17)"},
	{"id": "rust", "name": "Rust"},
	{"id": "python3", "name": "Python 3"},
	{"id": "java", "name": "Java"},
}

func validLanguage(id string) bool {
	for _, l := range Languages {
		if l["id"] == id {
			return true
		}
	}
	return false
}

// authenticate resolves the session cookie. Invalid, expired, disabled or
// IP-restricted sessions simply yield a guest.
func (s *Server) authenticate(c *Ctx) (*Account, error) {
	ck, err := c.r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" || len(ck.Value) > 128 {
		return nil, nil
	}
	var a Account
	var allowedIP, lastIP string
	var stale bool
	err = s.db.QueryRow(c, `
		SELECT s.id, a.id, a.name, a.role, a.allowed_ip, a.last_ip, s.last_seen_at < now() - interval '1 minute'
		FROM sessions s JOIN accounts a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND NOT a.disabled`,
		auth.HashToken(ck.Value)).Scan(&a.SessionID, &a.ID, &a.Name, &a.Role, &allowedIP, &lastIP, &stale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if allowedIP != "" && allowedIP != c.ip {
		return nil, nil
	}
	if stale || lastIP != c.ip {
		bg := context.WithoutCancel(c)
		s.db.Exec(bg, `UPDATE sessions SET last_seen_at = now(), ip = $2 WHERE id = $1`, a.SessionID, c.ip)
		if lastIP != c.ip {
			s.db.Exec(bg, `UPDATE accounts SET last_ip = $2 WHERE id = $1`, a.ID, c.ip)
			c.acct = &a
			c.audit("account.ip", "Account IP changed", map[string]any{"from": lastIP, "to": c.ip}, nil)
			c.acct = nil
		}
	}
	return &a, nil
}

func (c *Ctx) setSessionCookie(value string, maxAge int) {
	http.SetCookie(c.w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: c.s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (c *Ctx) startSession(accountID int) error {
	token, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	ua := c.r.UserAgent()
	if len(ua) > 256 {
		ua = ua[:256]
	}
	if _, err := c.s.db.Exec(c, `INSERT INTO sessions (account_id, token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval)`, accountID, hash, c.ip, strings.ToValidUTF8(ua, ""), sessionTTL.String()); err != nil {
		return err
	}
	// Opportunistic cleanup of this account's expired sessions.
	c.s.db.Exec(c, `DELETE FROM sessions WHERE account_id = $1 AND expires_at < now()`, accountID)
	c.setSessionCookie(token, int(sessionTTL.Seconds()))
	return nil
}

func (s *Server) getSession(c *Ctx) error {
	out := map[string]any{
		"site": map[string]any{
			"title":        s.cfg.SiteTitle,
			"allow_signup": s.cfg.AllowSignup,
			"languages":    Languages,
		},
		"account":  nil,
		"lockdown": nil,
	}
	if c.acct != nil {
		acct, err := s.jsonRow(c, `SELECT a.id, a.name, a.email, a.role, a.photo_url, a.cover_url, a.motto, a.last_language,
			(SELECT count(*) FROM questions q WHERE q.account_id = a.id AND q.reply IS NOT NULL AND NOT q.reply_seen) AS unread_replies
			FROM accounts a WHERE a.id = $1`, c.acct.ID)
		if err != nil {
			return err
		}
		out["account"] = acct
		if id, err := c.lockdownContest(); err != nil {
			return err
		} else if id != 0 {
			out["lockdown"] = id
		}
	}
	return c.ok(out)
}

func cleanText(s string) string { return strings.TrimSpace(strings.ToValidUTF8(s, "")) }

func validName(name string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 32 {
		return badRequest("Name must be 1 to 32 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return badRequest("Name contains invalid characters")
		}
	}
	return nil
}

func validEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 || !strings.Contains(email, "@") {
		return badRequest("Invalid email address")
	}
	return nil
}

// validImageURL accepts an empty string or an absolute https URL.
func validImageURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 512 {
		return badRequest("Image URL must be an https:// link")
	}
	return nil
}

func (s *Server) signUp(c *Ctx) error {
	if c.acct != nil {
		return conflict("Already signed in")
	}
	if !s.cfg.AllowSignup {
		return &Error{Status: 403, Code: "signup_disabled", Message: "Registration is closed"}
	}
	var in struct{ Email, Name, Password string }
	if err := c.bind(&in); err != nil {
		return err
	}
	if !s.limiter.allow("signup:"+c.ip, 10, time.Hour) {
		return errTooMany
	}
	in.Email, in.Name = cleanText(in.Email), cleanText(in.Name)
	if err := validEmail(in.Email); err != nil {
		return err
	}
	if err := validName(in.Name); err != nil {
		return err
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		return badRequest("%s", capitalize(err.Error()))
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	var id int
	err = s.db.QueryRow(c, `INSERT INTO accounts (email, name, password_hash, last_ip) VALUES ($1, $2, $3, $4) RETURNING id`,
		in.Email, in.Name, hash, c.ip).Scan(&id)
	if isUnique(err) {
		return conflict("This email is already registered")
	}
	if err != nil {
		return err
	}
	c.acct = &Account{ID: id, Name: in.Name, Role: "user"}
	c.audit("account.signup", "Account created", nil, nil)
	if err := c.startSession(id); err != nil {
		return err
	}
	return c.json(http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) signIn(c *Ctx) error {
	if c.acct != nil {
		return conflict("Already signed in")
	}
	var in struct{ Email, Password string }
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Email = cleanText(in.Email)
	if len(in.Email) > 254 || len(in.Password) > 256 {
		return badRequest("Invalid credentials")
	}
	if !s.limiter.allow("signin-ip:"+c.ip, 30, 5*time.Minute) ||
		!s.limiter.allow("signin-acct:"+strings.ToLower(in.Email), 10, 5*time.Minute) {
		return errTooMany
	}
	failed := &Error{Status: 401, Code: "invalid_credentials", Message: "Incorrect email or password"}

	var id int
	var hash, allowedIP, name, role string
	var disabled bool
	err := s.db.QueryRow(c, `SELECT id, password_hash, allowed_ip, disabled, name, role FROM accounts WHERE lower(email) = lower($1)`,
		in.Email).Scan(&id, &hash, &allowedIP, &disabled, &name, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.VerifyDummy(in.Password)
		c.audit("signin.failure", "Sign-in failed: unknown account", map[string]any{"email": in.Email}, nil)
		return failed
	}
	if err != nil {
		return err
	}
	if !auth.VerifyPassword(hash, in.Password) || disabled {
		c.audit("signin.failure", "Sign-in failed", map[string]any{"account_id": id}, nil)
		return failed
	}
	if allowedIP != "" && allowedIP != c.ip {
		c.audit("signin.failure", "Sign-in rejected: IP not allowed", map[string]any{"account_id": id}, nil)
		return &Error{Status: 403, Code: "ip_not_allowed", Message: "This account cannot sign in from your IP address"}
	}
	c.acct = &Account{ID: id, Name: name, Role: role}
	c.audit("signin.success", "Signed in", nil, nil)
	if err := c.startSession(id); err != nil {
		return err
	}
	return c.ok(map[string]any{"id": id})
}

func (s *Server) signOut(c *Ctx) error {
	if c.acct != nil {
		if _, err := s.db.Exec(c, `DELETE FROM sessions WHERE id = $1`, c.acct.SessionID); err != nil {
			return err
		}
		c.audit("signout", "Signed out", nil, nil)
	}
	c.setSessionCookie("", -1)
	return c.noContent()
}

func (s *Server) listSessions(c *Ctx) error {
	out, err := s.jsonList(c, `SELECT id, ip, user_agent, created_at, last_seen_at, id = $2 AS current
		FROM sessions WHERE account_id = $1 AND expires_at > now() ORDER BY last_seen_at DESC`, c.acct.ID, c.acct.SessionID)
	if err != nil {
		return err
	}
	return c.ok(out)
}

func (s *Server) revokeSession(c *Ctx) error {
	id, err := c.pathInt("id")
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(c, `DELETE FROM sessions WHERE id = $1 AND account_id = $2`, id, c.acct.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	if int64(id) == c.acct.SessionID {
		c.setSessionCookie("", -1)
	}
	return c.noContent()
}

func (s *Server) revokeOtherSessions(c *Ctx) error {
	if _, err := s.db.Exec(c, `DELETE FROM sessions WHERE account_id = $1 AND id <> $2`, c.acct.ID, c.acct.SessionID); err != nil {
		return err
	}
	return c.noContent()
}

func (s *Server) updateProfile(c *Ctx) error {
	var in struct {
		Name     string `json:"name"`
		PhotoURL string `json:"photo_url"`
		CoverURL string `json:"cover_url"`
		Motto    string `json:"motto"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	in.Name, in.Motto = cleanText(in.Name), cleanText(in.Motto)
	in.PhotoURL, in.CoverURL = strings.TrimSpace(in.PhotoURL), strings.TrimSpace(in.CoverURL)
	if err := validName(in.Name); err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Motto) > 100 {
		return badRequest("Motto must be at most 100 characters")
	}
	if err := errors.Join(validImageURL(in.PhotoURL), validImageURL(in.CoverURL)); err != nil {
		return badRequest("Image URLs must be https:// links")
	}
	_, err := s.db.Exec(c, `UPDATE accounts SET name = $2, photo_url = $3, cover_url = $4, motto = $5 WHERE id = $1`,
		c.acct.ID, in.Name, in.PhotoURL, in.CoverURL, in.Motto)
	if err != nil {
		return err
	}
	return c.noContent()
}

func (s *Server) changePassword(c *Ctx) error {
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := c.bind(&in); err != nil {
		return err
	}
	if !s.limiter.allow("password:"+c.ip, 10, 10*time.Minute) {
		return errTooMany
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		return badRequest("%s", capitalize(err.Error()))
	}
	var hash string
	if err := s.db.QueryRow(c, `SELECT password_hash FROM accounts WHERE id = $1`, c.acct.ID).Scan(&hash); err != nil {
		return err
	}
	if len(in.Old) > 256 || !auth.VerifyPassword(hash, in.Old) {
		return &Error{Status: 403, Code: "wrong_password", Message: "Current password is incorrect"}
	}
	newHash, err := auth.HashPassword(in.New)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(c, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c, `UPDATE accounts SET password_hash = $2 WHERE id = $1`, c.acct.ID, newHash); err != nil {
			return err
		}
		// Every other device must sign in again with the new password.
		_, err := tx.Exec(c, `DELETE FROM sessions WHERE account_id = $1 AND id <> $2`, c.acct.ID, c.acct.SessionID)
		return err
	})
	if err != nil {
		return err
	}
	c.audit("account.password", "Password changed", nil, nil)
	return c.noContent()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func isForeignKey(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}

// BootstrapAdmin creates the configured administrator when no admin exists.
func (s *Server) BootstrapAdmin(ctx context.Context) error {
	if s.cfg.AdminEmail == "" || s.cfg.AdminPassword == "" {
		return nil
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accounts WHERE role = 'admin')`).Scan(&exists); err != nil || exists {
		return err
	}
	if err := auth.ValidatePassword(s.cfg.AdminPassword); err != nil {
		return errors.New("ADMIN_PASSWORD: " + err.Error())
	}
	hash, err := auth.HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO accounts (email, name, password_hash, role) VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (lower(email)) DO UPDATE SET role = 'admin', disabled = false, password_hash = EXCLUDED.password_hash`, s.cfg.AdminEmail, s.cfg.AdminName, hash)
	if err == nil {
		s.log.Info("bootstrap administrator created", "email", s.cfg.AdminEmail)
	}
	return err
}
