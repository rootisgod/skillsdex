package skillsdex

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runServe(addr, dbPath string, trustedProxyCIDRs []string) error {
	if dbPath == "" {
		dir, err := skillsdexDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		dbPath = filepath.Join(dir, "skillsdex.db")
	}
	db, err := openDB(dbPath)
	if err != nil {
		return dbPathError(dbPath, err)
	}
	defer db.Close()
	if err := requireSafeBootstrap(context.Background(), db, addr); err != nil {
		return err
	}
	trustedProxies, err := parseTrustedProxies(trustedProxyCIDRs)
	if err != nil {
		return err
	}
	inviteEmailSender, err := inviteEmailSenderFromEnv()
	if err != nil {
		return err
	}

	a := &app{db: db, trustedProxies: trustedProxies, inviteEmailSender: inviteEmailSender}
	mux := http.NewServeMux()
	a.routes(mux)
	localURL := "http://127.0.0.1" + addr[strings.LastIndex(addr, ":"):]
	log.Printf("skillsdex web/API listening on http://%s", addr)
	log.Printf("open %s", localURL)
	return newHTTPServer(addr, a.securityHeaders(mux)).ListenAndServe()
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("/static/style.css", a.handleCSS)
	mux.HandleFunc("/login", a.handleLogin)
	mux.HandleFunc("/signup", a.handleSignup)
	mux.HandleFunc("/logout", a.requireWeb(a.handleLogout))
	mux.HandleFunc("/admin/audit", a.requireAdmin(a.handleAdminAudit))
	mux.HandleFunc("/admin/maintenance", a.requireAdmin(a.handleAdminMaintenance))
	mux.HandleFunc("/admin/invites", a.requireAdmin(a.handleAdminInvites))
	mux.HandleFunc("/admin/invites/", a.requireAdmin(a.handleAdminInviteAction))
	mux.HandleFunc("/admin/users", a.requireAdmin(a.handleAdminUsers))
	mux.HandleFunc("/admin/users/", a.requireAdmin(a.handleAdminUserEdit))
	mux.HandleFunc("/tokens", a.requireWeb(a.handleTokens))
	mux.HandleFunc("/tokens/", a.requireWeb(a.handleTokenAction))
	mux.HandleFunc("/sets", a.requireWeb(a.handleSets))
	mux.HandleFunc("/instructions", a.requireWeb(a.handleInstructions))
	mux.HandleFunc("/machines", a.requireWeb(a.handleMachines))
	mux.HandleFunc("/overview", a.requireWeb(a.handleOverview))
	mux.HandleFunc("/api/v1/devices/register", a.handleDeviceRegistration)
	mux.HandleFunc("/api/v1/sync/manifest", a.handleDeviceSync)
	mux.HandleFunc("/api/v1/sync/status", a.handleDeviceSync)
	mux.HandleFunc("/sets/", a.requireWeb(a.handleSetRoute))
	mux.HandleFunc("/skills", a.requireWeb(a.handleSkills))
	mux.HandleFunc("/skills/", a.requireWeb(a.handleSkillRoute))
	mux.HandleFunc("/setup/password", a.requireWeb(a.handleSetupPassword))
	mux.HandleFunc("/setup", a.requireWeb(a.handleSetup))
	mux.HandleFunc("/help", a.requireWeb(a.handleHelp))
	mux.HandleFunc("/v1/", a.handleAPI)
	mux.HandleFunc("/", a.requireWeb(a.handleHome))
}
