package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONFailureReturnsNonzero(t *testing.T) {
	clearConfigEnv(t)
	server := mockPortal(t, `{"error":"password_error","error_msg":"password_error"}`)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"--base-url", server.URL,
		"--username", "user",
		"--ip", "10.0.0.2",
		"--password-stdin",
		"--json",
	}, strings.NewReader("pass\n"), &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "password_error") {
		t.Fatalf("JSON response missing from stdout: %q", stdout.String())
	}
}

func TestSuccessfulLogin(t *testing.T) {
	clearConfigEnv(t)
	server := mockPortal(t, `{"error":"ok","suc_msg":"login_ok"}`)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"--host", server.URL,
		"-u", "user",
		"-p", "pass",
		"--ip", "10.0.0.2",
	}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 || stdout.String() != "login ok\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestInvalidConfigurationReturnsUsageError(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("SRUN_TIMEOUT", "not-a-number")

	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"-u", "user", "-p", "pass"}, strings.NewReader(""), &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid SRUN_TIMEOUT") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestTimeoutFlagOverridesInvalidEnvironment(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("SRUN_TIMEOUT", "not-a-number")
	server := mockPortal(t, `{"error":"ok","suc_msg":"login_ok"}`)
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"--base-url", server.URL,
		"-u", "user",
		"-p", "pass",
		"--ip", "10.0.0.2",
		"--timeout", "5",
	}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestPasswordStdinRejectsOtherPasswordSources(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("SRUN_PASSWORD", "from-env")

	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"--password-stdin"}, strings.NewReader("from-stdin\n"), &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestVersionDoesNotRequireValidEnvironment(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("SRUN_TIMEOUT", "invalid")
	oldVersion := version
	oldCommit := commit
	version = "v1.2.3"
	commit = "1234567890abcdef"
	t.Cleanup(func() {
		version = oldVersion
		commit = oldCommit
	})

	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", exitCode, stderr.String())
	}
	if stdout.String() != "campuslink v1.2.3 (commit 1234567890ab)\n" {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestReadPassword(t *testing.T) {
	password, err := readPassword(strings.NewReader("contains spaces\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if password != "contains spaces" {
		t.Fatalf("password = %q", password)
	}
	if _, err := readPassword(strings.NewReader("\n")); err == nil {
		t.Fatal("expected empty password to fail")
	}
}

func mockPortal(t *testing.T, portalResult string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/get_challenge":
			callback := r.URL.Query().Get("callback")
			fmt.Fprintf(w, `%s({"challenge":"token123"});`, callback)
		case "/cgi-bin/srun_portal":
			callback := r.URL.Query().Get("callback")
			fmt.Fprintf(w, "%s(%s);", callback, portalResult)
		default:
			http.NotFound(w, r)
		}
	}))
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SRUN_USERNAME",
		"SRUN_PASSWORD",
		"SRUN_IP",
		"SRUN_HOST",
		"SRUN_BASE_URL",
		"SRUN_AC_ID",
		"SRUN_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
