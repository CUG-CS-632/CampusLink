package srun

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAuthValuesMatchKnownAuthVector(t *testing.T) {
	config := DefaultConfig()
	username := "user"
	password := "pass"
	ip := "10.0.0.2"
	token := "token123"

	hmd5 := HMACMD5Hex(token, password)
	if hmd5 != "d577c7ef5b341803e5311a44b1b4258d" {
		t.Fatalf("hmd5 mismatch: %s", hmd5)
	}

	info, err := Info(username, password, ip, token, config)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo := "{SRBX1}PRcvPe0z1SUzdwLuXaYzuPCoaiOPthLxJ6UdFPNsYrXs/uabXR/TZFudS3wjs59gxwvkCgIMbdbp3/6XORY5sKxTM1Og0xIMOViUhBNA8DlW/S6mt0lhGSFKCd+="
	if info != wantInfo {
		t.Fatalf("info mismatch:\nwant %s\n got %s", wantInfo, info)
	}

	chksum := Checksum(token, username, hmd5, ip, info, config)
	if chksum != "8ab41acd3eceee0198a04df56b02f9516a04a087" {
		t.Fatalf("chksum mismatch: %s", chksum)
	}
}

func TestParseJSONP(t *testing.T) {
	got, err := ParseJSONP(`jQuery123({"challenge":"abc","error":"ok"});`)
	if err != nil {
		t.Fatal(err)
	}
	if got["challenge"] != "abc" || got["error"] != "ok" {
		t.Fatalf("unexpected result: %#v", got)
	}

	got, err = ParseJSONP("jQuery123(\n{\"challenge\":\"multiline\"}\n);")
	if err != nil {
		t.Fatal(err)
	}
	if got["challenge"] != "multiline" {
		t.Fatalf("unexpected multiline result: %#v", got)
	}

	if _, err := ParseJSONP(`alert-before-callback({"error":"ok"})`); err == nil {
		t.Fatal("expected invalid callback wrapper to fail")
	}
	if _, err := ParseJSONP(`null`); err == nil {
		t.Fatal("expected null response to fail")
	}
}

func TestOK(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{name: "login ok", in: map[string]any{"suc_msg": "login_ok"}, want: true},
		{name: "already online", in: map[string]any{"suc_msg": "ip_already_online_error"}, want: true},
		{name: "already online with message", in: map[string]any{"suc_msg": "ip_already_online_error", "error_msg": "IP already online"}, want: true},
		{name: "already online in error", in: map[string]any{"error": "ip_already_online_error", "error_msg": "IP already online"}, want: true},
		{name: "error ok", in: map[string]any{"error": "ok"}, want: true},
		{name: "conflicting error", in: map[string]any{"error": "ok", "error_msg": "password_error"}, want: false},
		{name: "failed", in: map[string]any{"error_msg": "password_error"}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OK(tc.in); got != tc.want {
				t.Fatalf("OK() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoginFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "CampusLink/1") {
			t.Errorf("unexpected User-Agent: %q", r.UserAgent())
		}
		switch r.URL.Path {
		case "/srun_portal_pc":
			fmt.Fprint(w, `<script>window.portal = {"ip": "10.0.0.2"};</script>`)
		case "/cgi-bin/get_challenge":
			if got := r.URL.Query().Get("username"); got != "user" {
				t.Errorf("username = %q", got)
			}
			callback := r.URL.Query().Get("callback")
			fmt.Fprintf(w, `%s({"challenge":"token123","error":"ok"});`, callback)
		case "/cgi-bin/srun_portal":
			query := r.URL.Query()
			if got := query.Get("password"); got != "{MD5}"+HMACMD5Hex("token123", "pass") {
				t.Errorf("password hash = %q", got)
			}
			wantOS, wantName := portalPlatform()
			if query.Get("os") != wantOS || query.Get("name") != wantName {
				t.Errorf("platform = %q/%q, want %q/%q", query.Get("os"), query.Get("name"), wantOS, wantName)
			}
			callback := query.Get("callback")
			fmt.Fprintf(w, `%s({"error":"ok","suc_msg":"login_ok"});`, callback)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := DefaultConfig()
	config.BaseURL = server.URL
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Login("user", "pass", "")
	if err != nil {
		t.Fatal(err)
	}
	if !OK(result) {
		t.Fatalf("login result was not successful: %#v", result)
	}
}

func TestLoginValidatesInput(t *testing.T) {
	client, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		username string
		password string
		ip       string
	}{
		{name: "missing username", password: "pass", ip: "10.0.0.2"},
		{name: "missing password", username: "user", ip: "10.0.0.2"},
		{name: "invalid IP", username: "user", password: "pass", ip: "not-an-ip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.Login(tc.username, tc.password, tc.ip); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	if _, err := client.LoginContext(nil, "user", "pass", "10.0.0.2"); err == nil {
		t.Fatal("expected nil context to fail")
	}
}

func TestNewClientValidatesConfig(t *testing.T) {
	cases := []Config{
		{BaseURL: "ftp://portal.example"},
		{BaseURL: "http://user:pass@portal.example"},
		{BaseURL: "http://portal.example?secret=value"},
		{BaseURL: "http://portal.example", Timeout: -time.Second},
		{BaseURL: "http://portal.example", MaxResponseBytes: maxResponseBytes + 1},
	}
	for _, config := range cases {
		if _, err := NewClient(config); err == nil {
			t.Fatalf("expected invalid config to fail: %#v", config)
		}
	}

	config := DefaultConfig()
	config.BaseURL = ""
	config.Host = "portal.example"
	if _, err := NewClient(config); err != nil {
		t.Fatalf("legacy host should remain supported: %v", err)
	}
	config = DefaultConfig()
	config.Host = "portal.example"
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("legacy host override should remain supported: %v", err)
	}
	if client.baseURL.Host != "portal.example" {
		t.Fatalf("legacy host was ignored: %q", client.baseURL.Host)
	}
}

func TestResponseSizeIsLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 65))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.BaseURL = server.URL
	config.MaxResponseBytes = 64
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DiscoverIP(); err == nil || !strings.Contains(err.Error(), "exceeds 64 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPErrorDoesNotIncludeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "sensitive-response-body", http.StatusBadGateway)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.BaseURL = server.URL
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DiscoverIP()
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if strings.Contains(err.Error(), "sensitive-response-body") {
		t.Fatalf("HTTP response body leaked into error: %v", err)
	}
}

func TestRequestErrorRedactsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/get_challenge":
			callback := r.URL.Query().Get("callback")
			fmt.Fprintf(w, `%s({"challenge":"token123"});`, callback)
		case "/cgi-bin/srun_portal":
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer does not support hijacking")
				return
			}
			connection, _, hijackErr := hijacker.Hijack()
			if hijackErr != nil {
				t.Errorf("hijack connection: %v", hijackErr)
				return
			}
			connection.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := DefaultConfig()
	config.BaseURL = server.URL
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Login("sensitive-user", "sensitive-password", "10.0.0.2")
	if err == nil {
		t.Fatal("expected timeout")
	}
	message := err.Error()
	if !strings.Contains(message, "/cgi-bin/srun_portal") {
		t.Fatalf("error did not come from login request: %s", message)
	}
	for _, secret := range []string{"sensitive-user", "password=", "info=", "chksum="} {
		if strings.Contains(message, secret) {
			t.Fatalf("error contains sensitive query data %q: %s", secret, message)
		}
	}
}

func TestLoginHonorsContextCancellation(t *testing.T) {
	client, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.LoginContext(ctx, "user", "pass", "10.0.0.2"); err == nil {
		t.Fatal("expected canceled context to fail")
	}
}

func TestPlatformMatchesRuntime(t *testing.T) {
	osValue, name := portalPlatform()
	if runtime.GOOS == "windows" && (osValue != "windows+10" || name != "windows") {
		t.Fatalf("unexpected Windows platform: %q/%q", osValue, name)
	}
	if runtime.GOOS == "darwin" && (osValue != "mac" || name != "mac") {
		t.Fatalf("unexpected macOS platform: %q/%q", osValue, name)
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" && (osValue != "linux" || name != "linux") {
		t.Fatalf("unexpected fallback platform: %q/%q", osValue, name)
	}
}
