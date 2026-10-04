// Package e2e runs the real server against a throwaway PostgreSQL database and drives it
// over HTTP, the way the app and the web CRM do. Nothing here touches a real database:
// each run creates its own database and drops it at the end.
//
// It needs a local PostgreSQL whose user may create databases. Point it at one with
// E2E_ADMIN_DATABASE_URL (a connection string to the "postgres" database); without that it
// uses the DB_* settings in backend/.env. With no PostgreSQL available the suite is skipped.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/server"
	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

var (
	baseURL string // http://127.0.0.1:port
	testDB  *database.DB
)

const (
	crmAPI = "/api/crm/v1"
	appAPI = "/api/v1"
)

func adminURL() string {
	if u := os.Getenv("E2E_ADMIN_DATABASE_URL"); u != "" {
		return u
	}
	_ = godotenv.Load("../../.env")
	host, port := envOr("DB_HOST", "localhost"), envOr("DB_PORT", "5432")
	user, pass := envOr("DB_USER", os.Getenv("USER")), os.Getenv("DB_PASSWORD")
	u := url.URL{Scheme: "postgres", Host: host + ":" + port, Path: "/postgres", RawQuery: "sslmode=disable"}
	if pass != "" {
		u.User = url.UserPassword(user, pass)
	} else {
		u.User = url.User(user)
	}
	return u.String()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	admin := adminURL()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		fmt.Println("e2e: skipped — no PostgreSQL to create a test database in:", err)
		os.Exit(0)
	}
	name := fmt.Sprintf("crm_e2e_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		fmt.Println("e2e: skipped — can't create a test database:", err)
		os.Exit(0)
	}
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		if testDB != nil {
			testDB.Close()
		}
		_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = conn.Close(ctx)
	}
	defer drop()

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	// A clean, explicit environment: local CRM, codes shown instead of sent, no email.
	for k, v := range map[string]string{
		"DATABASE_URL": u.String(), "ENV": "development", "CRM_APP_ENV": "local", "SMS_PROVIDER": "preview",
		"CRM_SMTP_HOST": "", "CRM_BREVO_API_KEY": "", "CRM_SEED_DEMO": "true", "REDIS_URL": "", "S3_ENDPOINT": "",
		"CRM_CARDFLOW_SYNC": "true", "CRM_SYNC_INTERVAL": "24h",
	} {
		os.Setenv(k, v)
	}
	cfg := config.Load()
	cfg.DatabaseURL = u.String()
	db, err := database.NewPostgresPool(ctx, cfg)
	if err != nil {
		fmt.Println("e2e: can't open the test database:", err)
		drop()
		os.Exit(1)
	}
	testDB = db
	if err := database.RunMigrations(ctx, db); err != nil {
		fmt.Println("e2e: app migrations failed:", err)
		drop()
		os.Exit(1)
	}
	handler, crmModule := server.New(server.Deps{Cfg: cfg, DB: db})
	if crmModule.Identity() == nil {
		fmt.Println("e2e: the CRM module did not start")
		drop()
		os.Exit(1)
	}
	srv := httptest.NewServer(handler)
	baseURL = srv.URL
	code := m.Run()
	srv.Close()
	drop()
	os.Exit(code)
}

// ---- small HTTP client ----

type resp struct {
	Status int
	Body   map[string]any
	Raw    string
	Header http.Header
}

func (r resp) str(path ...string) string {
	v, _ := r.at(path...).(string)
	return v
}

func (r resp) at(path ...string) any {
	var cur any = r.Body
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func (r resp) list(path ...string) []any {
	v, _ := r.at(path...).([]any)
	return v
}

// call sends a JSON request. token is a bearer session ("" for anonymous).
func call(t *testing.T, method, path, token string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Transport", "bearer")
	if clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: string(raw), Header: res.Header, Body: map[string]any{}}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func want(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: got %d, want %d — %s", what, r.Status, status, truncate(r.Raw, 400))
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var phoneSeq = 0

// clientIP: sign-ins come from a different address per person, as they would in real
// use (the server limits code requests per address as well as per number).
var clientIP = ""

// freshPhone returns a number no other test uses (each has its own SMS rate limit).
func freshPhone() string {
	phoneSeq++
	return fmt.Sprintf("90000%05d", phoneSeq)
}

// signIn signs a phone in through the real flow and returns the bearer token.
func signIn(t *testing.T, phone, name string) string {
	t.Helper()
	phoneSeq++
	clientIP = fmt.Sprintf("10.9.%d.%d", phoneSeq/250, phoneSeq%250+1)
	defer func() { clientIP = "" }()
	r := call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone})
	want(t, r, 200, "request code for "+phone)
	code := r.str("devCode")
	if len(code) != 6 {
		t.Fatalf("preview sender should return the code, got %q", r.Raw)
	}
	v := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": code, "name": name})
	want(t, v, 200, "verify code for "+phone)
	tok := v.str("token")
	if !strings.HasPrefix(tok, "crms_") {
		t.Fatalf("expected a bearer session token, got %q", truncate(v.Raw, 200))
	}
	return tok
}

// newBusiness creates a business for the signed-in person and returns its code.
func newBusiness(t *testing.T, token, name string) string {
	t.Helper()
	r := call(t, "POST", crmAPI+"/businesses", token, map[string]any{"name": name})
	want(t, r, 201, "create business "+name)
	code := r.str("business", "code")
	if code == "" {
		t.Fatalf("no business code in %s", r.Raw)
	}
	return code
}

// create makes a record and returns its id.
func create(t *testing.T, token, ws, object string, values map[string]any) string {
	t.Helper()
	r := call(t, "POST", crmAPI+"/w/"+ws+"/crm/"+object, token, map[string]any{"values": values})
	if r.Status != 200 && r.Status != 201 {
		t.Fatalf("create %s in %s: %d %s", object, ws, r.Status, truncate(r.Raw, 400))
	}
	id := r.str("id")
	if id == "" {
		t.Fatalf("create %s: no id in %s", object, truncate(r.Raw, 300))
	}
	return id
}

// total is the number of records a list call reports.
func total(t *testing.T, token, ws, object, query string) int {
	t.Helper()
	r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/"+object+query, token, nil)
	want(t, r, 200, "list "+object+" in "+ws)
	n, _ := r.at("total").(float64)
	return int(n)
}
