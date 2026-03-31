package integration

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	binaryPath string
	buildOnce  sync.Once
	buildErr   error
)

func buildBroker(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		tmp := t.TempDir()
		binaryPath = filepath.Join(tmp, "nexus-broker"+ext)

		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/nexus-broker")
		// Build from the broker module root
		cmd.Dir = filepath.Join("..", "..")
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = err
			t.Logf("build output: %s", string(out))
		}
	})
	if buildErr != nil {
		t.Fatalf("failed to build broker binary: %v", buildErr)
	}
	return binaryPath
}

func genKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func runBroker(t *testing.T, env map[string]string) (output string, exitCode int) {
	t.Helper()
	bin := buildBroker(t)

	cmd := exec.Command(bin)
	cmd.Env = []string{}
	for _, e := range os.Environ() {
		k := strings.SplitN(e, "=", 2)[0]
		upper := strings.ToUpper(k)
		if upper == "ENCRYPTION_KEY" || upper == "STATE_KEY" ||
			upper == "DATABASE_URL" || upper == "BASE_URL" ||
			upper == "REDIS_URL" || upper == "PORT" {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	output = string(out)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, exitErr.ExitCode()
		}
		t.Fatalf("unexpected error running broker: %v", err)
	}
	return output, 0
}

func TestStartup_MissingEncryptionKey(t *testing.T) {
	out, code := runBroker(t, map[string]string{
		"STATE_KEY": genKey(t),
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero when ENCRYPTION_KEY is missing")
	}
	if !strings.Contains(out, "ENCRYPTION_KEY is not set") {
		t.Fatalf("expected actionable error about ENCRYPTION_KEY, got:\n%s", out)
	}
	if !strings.Contains(out, "openssl rand -base64 32") {
		t.Fatalf("expected generation hint in output, got:\n%s", out)
	}
}

func TestStartup_MissingStateKey(t *testing.T) {
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": genKey(t),
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero when STATE_KEY is missing")
	}
	if !strings.Contains(out, "STATE_KEY is not set") {
		t.Fatalf("expected actionable error about STATE_KEY, got:\n%s", out)
	}
}

func TestStartup_BothKeysMissing(t *testing.T) {
	out, code := runBroker(t, map[string]string{})
	if code == 0 {
		t.Fatal("broker should exit non-zero when both keys are missing")
	}
	if !strings.Contains(out, "ENCRYPTION_KEY") && !strings.Contains(out, "STATE_KEY") {
		t.Fatalf("expected error mentioning a missing key, got:\n%s", out)
	}
}

func TestStartup_InvalidBase64EncryptionKey(t *testing.T) {
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": "not!!valid!!base64$$",
		"STATE_KEY":      genKey(t),
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero for invalid base64 ENCRYPTION_KEY")
	}
	if !strings.Contains(out, "ENCRYPTION_KEY") && !strings.Contains(out, "base64") {
		t.Fatalf("expected base64 error for ENCRYPTION_KEY, got:\n%s", out)
	}
}

func TestStartup_InvalidBase64StateKey(t *testing.T) {
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": genKey(t),
		"STATE_KEY":      "garbage%%%",
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero for invalid base64 STATE_KEY")
	}
	if !strings.Contains(out, "STATE_KEY") && !strings.Contains(out, "base64") {
		t.Fatalf("expected base64 error for STATE_KEY, got:\n%s", out)
	}
}

func TestStartup_WrongLengthEncryptionKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": short,
		"STATE_KEY":      genKey(t),
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero for 16-byte ENCRYPTION_KEY")
	}
	if !strings.Contains(out, "16 bytes") || !strings.Contains(out, "ENCRYPTION_KEY") {
		t.Fatalf("expected length error for ENCRYPTION_KEY, got:\n%s", out)
	}
}

func TestStartup_WrongLengthStateKey(t *testing.T) {
	long := base64.StdEncoding.EncodeToString(make([]byte, 64))
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": genKey(t),
		"STATE_KEY":      long,
	})
	if code == 0 {
		t.Fatal("broker should exit non-zero for 64-byte STATE_KEY")
	}
	if !strings.Contains(out, "64 bytes") || !strings.Contains(out, "STATE_KEY") {
		t.Fatalf("expected length error for STATE_KEY, got:\n%s", out)
	}
}

func TestStartup_ValidKeys_FailsAtDB(t *testing.T) {
	// With valid keys but no real DB, broker should pass key validation
	// and fail later at the database connection step — not at key validation.
	out, code := runBroker(t, map[string]string{
		"ENCRYPTION_KEY": genKey(t),
		"STATE_KEY":      genKey(t),
		"DATABASE_URL":   "postgres://fake:fake@localhost:1/fake?sslmode=disable",
	})
	if code == 0 {
		t.Skip("broker started (DB available); can't test DB-failure path")
	}
	if strings.Contains(out, "ENCRYPTION_KEY") || strings.Contains(out, "STATE_KEY") {
		t.Fatalf("valid keys should not cause a key validation error, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "database") && !strings.Contains(strings.ToLower(out), "connect") {
		t.Fatalf("expected failure at database connection, got:\n%s", out)
	}
}
