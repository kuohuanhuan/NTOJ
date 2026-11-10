// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr     string
	DatabaseURL    string
	DataDir        string
	SiteTitle      string
	CookieSecure   bool
	AllowSignup    bool
	TrustedProxies []netip.Prefix
	AllowedOrigins []string
	JudgeToken     string
	SubmitCooldown int // seconds between two submissions of one account outside contests

	// Optional first-run bootstrap of an administrator account.
	AdminEmail, AdminName, AdminPassword string
}

func Load() (Config, error) {
	c := Config{
		ListenAddr:    env("LISTEN_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		DataDir:       env("DATA_DIR", "./data"),
		SiteTitle:     env("SITE_TITLE", "NTOJ"),
		JudgeToken:    os.Getenv("JUDGE_TOKEN"),
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminName:     env("ADMIN_NAME", "admin"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	var err error
	if c.CookieSecure, err = envBool("COOKIE_SECURE", true); err != nil {
		return c, err
	}
	if c.AllowSignup, err = envBool("ALLOW_SIGNUP", true); err != nil {
		return c, err
	}
	if c.SubmitCooldown, err = strconv.Atoi(env("SUBMIT_COOLDOWN", "5")); err != nil || c.SubmitCooldown < 0 {
		return c, errors.New("SUBMIT_COOLDOWN must be a non-negative integer")
	}
	for _, p := range splitList(os.Getenv("TRUSTED_PROXIES")) {
		prefix, err := netip.ParsePrefix(p)
		if err != nil {
			addr, err2 := netip.ParseAddr(p)
			if err2 != nil {
				return c, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP or CIDR", p)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix)
	}
	c.AllowedOrigins = splitList(os.Getenv("ALLOWED_ORIGINS"))
	if c.JudgeToken != "" && len(c.JudgeToken) < 32 {
		return c, errors.New("JUDGE_TOKEN must be at least 32 characters")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s must be true or false", key)
	}
	return b, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
