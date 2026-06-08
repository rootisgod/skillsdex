package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

type testApp struct {
	Home   string
	DB     *sql.DB
	Admin  *User
	Token  string
	App    *app
	Mux    *http.ServeMux
	Server *httptest.Server
}

type testAppOption func(*app)

func withSourceClient(client httpDoer) testAppOption {
	return func(a *app) {
		a.httpClient = client
	}
}

func newTestApp(t *testing.T, opts ...testAppOption) *testApp {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test db: %v", err)
		}
	})

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test-cli")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	for _, opt := range opts {
		opt(a)
	}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &testApp{
		Home:   home,
		DB:     db,
		Admin:  admin,
		Token:  token,
		App:    a,
		Mux:    mux,
		Server: server,
	}
}

func (ta *testApp) authenticateCLI(t *testing.T) {
	t.Helper()
	if err := saveAuth(AuthConfig{ServerURL: ta.Server.URL, Token: ta.Token}); err != nil {
		t.Fatal(err)
	}
}

func (ta *testApp) createSkill(t *testing.T, in SkillInput) *Skill {
	t.Helper()
	skill, err := createSkill(context.Background(), ta.DB, ta.Admin, in)
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

func (ta *testApp) installedSkillPath(owner, slug string) string {
	return filepath.Join(ta.Home, ".codex", "skills", owner+"--"+slug, "SKILL.md")
}
