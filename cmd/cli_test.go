package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is a stand-in django-api that records the token each request came
// with.
type fakeAPI struct {
	*httptest.Server
	mu     sync.Mutex
	tokens []string
	ingest map[string]any
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	api := &fakeAPI{ingest: map[string]any{"state": "done", "kind": "upload"}}
	api.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.tokens = append(api.tokens, r.Header.Get("Authorization"))
		api.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 1, "email": "editor@example.com",
				"editorOf": []map[string]any{{"id": 7, "name": "Frikanalen"}},
				"memberOf": []map[string]any{},
			})
		case "/api/obtain-token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body["otp"] {
			case "":
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"type":"validation_error","errors":[{"code":"otp_required","detail":"Skriv inn koden fra autentiseringsappen.","attr":"otp"}]}`))
			case "123456":
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "new-token"})
			default:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"type":"validation_error","errors":[{"code":"invalid","detail":"Koden stemmer ikke.","attr":"otp"}]}`))
			}
		case "/api/videos/7/ingest":
			_ = json.NewEncoder(w).Encode(api.ingest)
		case "/api/videos/500/ingest":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"type":"server_error","errors":[{"code":"error","detail":"it broke"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	return api
}

func (a *fakeAPI) lastToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.tokens) == 0 {
		return ""
	}
	return a.tokens[len(a.tokens)-1]
}

// runFK runs the CLI with a configuration whose active environment "test"
// talks to api, and a second environment "other" on the same server with its
// own token.
func runFK(t *testing.T, api *fakeAPI, argv ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FK_ENV", "")
	t.Setenv("NO_COLOR", "")
	config := "environment: test\nenvironments:\n" +
		"  test:\n    api: " + api.URL + "\n    token: test-token\n" +
		"  other:\n    api: " + api.URL + "\n    token: other-token\n"
	if err := os.WriteFile(filepath.Join(home, ".frikanalen.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code = run(argv, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestStrayArgumentsAreUsageErrors(t *testing.T) {
	api := newFakeAPI(t)
	for _, argv := range [][]string{
		{"video", "status", "7", "8"},
		{"video", "create", "a.mov", "b.mov", "-t", "x", "-c", "y"},
		{"video", "status", "7", "-i", "5"},
		{"org", "list", "7"},
		{"video", "status", "seven"},
	} {
		stdout, stderr, code := runFK(t, api, argv...)
		if code != exitUsage {
			t.Errorf("%v: exit %d, want %d (stderr %q)", argv, code, exitUsage, stderr)
		}
		if stdout != "" {
			t.Errorf("%v: wrote to stdout: %q", argv, stdout)
		}
		if !strings.HasPrefix(stderr, "fk: ") || !strings.Contains(stderr, "--help' for usage") {
			t.Errorf("%v: stderr = %q", argv, stderr)
		}
	}
	if len(api.tokens) != 0 {
		t.Errorf("a usage error reached the server %d times", len(api.tokens))
	}
}

func TestRuntimeErrorIsOneLineWithoutUsage(t *testing.T) {
	stdout, stderr, code := runFK(t, newFakeAPI(t), "video", "status", "500")
	if code != exitFailure {
		t.Errorf("exit %d, want %d", code, exitFailure)
	}
	if stdout != "" {
		t.Errorf("stdout = %q", stdout)
	}
	if stderr != "fk: 500: it broke\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestStatusPrintsStateAndJSON(t *testing.T) {
	api := newFakeAPI(t)
	stdout, stderr, code := runFK(t, api, "video", "status", "7")
	if code != exitOK || stdout != "done\n" || stderr != "" {
		t.Errorf("got %d %q %q", code, stdout, stderr)
	}

	stdout, _, code = runFK(t, api, "video", "status", "7", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || code != exitOK {
		t.Fatalf("exit %d, not JSON: %v\n%s", code, err, stdout)
	}
	if got["id"] != float64(7) || got["state"] != "done" || got["kind"] != "upload" {
		t.Errorf("json = %v", got)
	}
}

func TestStatusOfFailedIngestExitsOneQuietly(t *testing.T) {
	api := newFakeAPI(t)
	api.ingest = map[string]any{"state": "failed", "errorCode": "unreadable"}
	stdout, stderr, code := runFK(t, api, "video", "status", "7")
	if code != exitFailure || stdout != "failed: unreadable\n" || stderr != "" {
		t.Errorf("got %d %q %q", code, stdout, stderr)
	}
}

func TestPipedTablesArePlainTabSeparatedText(t *testing.T) {
	stdout, stderr, code := runFK(t, newFakeAPI(t), "org", "list")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != "id\tname\trole\n7\tFrikanalen\teditor\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestEnvFlagCarriesThatEnvironmentsToken(t *testing.T) {
	api := newFakeAPI(t)

	if _, _, code := runFK(t, api, "org", "list"); code != exitOK || api.lastToken() != "Token test-token" {
		t.Errorf("active environment: exit %d, token %q", code, api.lastToken())
	}
	if _, _, code := runFK(t, api, "--env", "other", "org", "list"); code != exitOK || api.lastToken() != "Token other-token" {
		t.Errorf("--env other: exit %d, token %q", code, api.lastToken())
	}

	t.Setenv("FK_ENV", "other")
	var out, errOut bytes.Buffer
	if code := run([]string{"org", "list"}, &out, &errOut); code != exitOK || api.lastToken() != "Token other-token" {
		t.Errorf("FK_ENV=other: exit %d, token %q, stderr %q", code, api.lastToken(), errOut.String())
	}
}

func TestUnknownEnvironmentIsAUsageError(t *testing.T) {
	_, stderr, code := runFK(t, newFakeAPI(t), "--env", "nowhere", "org", "list")
	if code != exitUsage || !strings.Contains(stderr, `unknown environment "nowhere"`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestTestcardWithoutOutputIsAUsageError(t *testing.T) {
	_, stderr, code := runFK(t, newFakeAPI(t), "dev", "testcard")
	if code != exitUsage || !strings.Contains(stderr, `"output" not set`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestEnvPrintsAndListsWithoutChatter(t *testing.T) {
	api := newFakeAPI(t)
	stdout, stderr, code := runFK(t, api, "env")
	if code != exitOK || stdout != "test\t"+api.URL+"\n" || stderr != "" {
		t.Errorf("env: %d %q %q", code, stdout, stderr)
	}

	stdout, _, _ = runFK(t, api, "env", "list", "--json")
	var envs []environmentView
	if err := json.Unmarshal([]byte(stdout), &envs); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	active := ""
	for _, env := range envs {
		if env.Active {
			active = env.Name
		}
	}
	if active != "test" {
		t.Errorf("active = %q in %+v", active, envs)
	}
}

func TestLoginRefusesToPromptWithoutATerminal(t *testing.T) {
	api := newFakeAPI(t)
	_, stderr, code := runFK(t, api, "auth", "login", "--email", "a@example.com")
	if code != exitUsage || !strings.Contains(stderr, "--password-stdin") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// withStdin feeds text to the command's stdin for the rest of the test.
func withStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(text); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; _ = r.Close() })
}

func TestLoginWithTwoFactorFromAScriptNeedsOTP(t *testing.T) {
	api := newFakeAPI(t)
	withStdin(t, "hunter2\n")
	_, stderr, code := runFK(t, api, "auth", "login", "--email", "a@example.com", "--password-stdin")
	if code != exitUsage || !strings.Contains(stderr, "--otp") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestLoginWithOTP(t *testing.T) {
	api := newFakeAPI(t)
	withStdin(t, "hunter2\n")
	_, stderr, code := runFK(t, api, "auth", "login", "--email", "a@example.com", "--password-stdin", "--otp", "123456")
	if code != exitOK || !strings.Contains(stderr, "Logged in") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestLoginWithAWrongOTP(t *testing.T) {
	api := newFakeAPI(t)
	withStdin(t, "hunter2\n")
	_, stderr, code := runFK(t, api, "auth", "login", "--email", "a@example.com", "--password-stdin", "--otp", "000000")
	if code != exitFailure || !strings.Contains(stderr, "Koden stemmer ikke") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestShellQuoteMakesHintsPasteable(t *testing.T) {
	for in, want := range map[string]string{
		"talk.mov":           "talk.mov",
		"/tmp/My Talk.mov":   "'/tmp/My Talk.mov'",
		"it's.mov":           `'it'\''s.mov'`,
		"/srv/a-b_c+d,e@f:1": "/srv/a-b_c+d,e@f:1",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHintFollowsTheError(t *testing.T) {
	var stderr bytes.Buffer
	code := reportError(&stderr, nil, withHint(errors.New("upload failed"), "fk video upload %d %s", 7, "a.mov"))
	if code != exitFailure || stderr.String() != "fk: upload failed\nhint: fk video upload 7 a.mov\n" {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}
