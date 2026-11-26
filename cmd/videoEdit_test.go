package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// videoAPI is a stand-in for /api/videos/42 that keeps one record and
// applies PUT and PATCH to it the way DRF does: fields given replace the
// stored ones.
type videoAPI struct {
	*fakeAPI
	mu      sync.Mutex
	record  map[string]any
	methods []string
	bodies  []map[string]any
}

func newVideoAPI(t *testing.T) *videoAPI {
	t.Helper()
	v := &videoAPI{record: map[string]any{
		"id": 42, "name": "Old title", "description": "Old description",
		"categories": []any{"Kultur"}, "publishOnWeb": true,
		"series": map[string]any{"id": 3, "name": "NUUG"},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		if r.URL.Path != "/api/videos/42" {
			http.NotFound(w, r)
			return
		}
		v.methods = append(v.methods, r.Method)
		if r.Method == http.MethodPut || r.Method == http.MethodPatch {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			v.bodies = append(v.bodies, body)
			if r.Method == http.MethodPut && body["name"] == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"type":"validation_error","errors":[{"code":"required","detail":"This field is required.","attr":"name"}]}`))
				return
			}
			for key, value := range body {
				if key != "id" && key != "series" {
					v.record[key] = value
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v.record)
	}))
	t.Cleanup(server.Close)
	v.fakeAPI = &fakeAPI{Server: server}
	return v
}

func (v *videoAPI) lastBody(t *testing.T) map[string]any {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.bodies) == 0 {
		t.Fatal("no request body reached the server")
	}
	return v.bodies[len(v.bodies)-1]
}

func TestVideoGetPrintsTheWholeRecord(t *testing.T) {
	api := newVideoAPI(t)
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "get", "42")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if got["name"] != "Old title" || got["publishOnWeb"] != true {
		t.Errorf("record = %v", got)
	}
}

func TestVideoGetOutputRoundTripsThroughPut(t *testing.T) {
	api := newVideoAPI(t)
	stdout, _, _ := runFK(t, api.fakeAPI, "video", "get", "42")
	edited := strings.Replace(stdout, "Old title", "New title", 1)
	file := filepath.Join(t.TempDir(), "video.json")
	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "put", "42", file)
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != api.URL+"/video/42\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if api.methods[len(api.methods)-1] != http.MethodPut {
		t.Errorf("methods = %v", api.methods)
	}
	if got := api.lastBody(t)["name"]; got != "New title" {
		t.Errorf("sent name %v", got)
	}
}

func TestVideoPutReadsStdinAndReportsValidationErrors(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, `{"description": "no name"}`)
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "put", "42", "-")
	if code != exitFailure || stdout != "" {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if stderr != "fk: 400: name: This field is required.\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestVideoPutRefusesNonObjects(t *testing.T) {
	api := newVideoAPI(t)
	for _, input := range []string{`[1, 2]`, `{"name": `, ``} {
		withStdin(t, input)
		_, stderr, code := runFK(t, api.fakeAPI, "video", "put", "42")
		if code != exitFailure {
			t.Errorf("%q: exit %d, stderr %q", input, code, stderr)
		}
	}
	if len(api.methods) != 0 {
		t.Errorf("bad input reached the server: %v", api.methods)
	}
}

func TestVideoPatchFromFlagsSendsOnlyThoseFields(t *testing.T) {
	api := newVideoAPI(t)
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "patch", "42",
		"-d", "Ny beskrivelse", "-c", "Debatt", "-c", "Kultur", "--series", "0", "--json")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	body := api.lastBody(t)
	want := map[string]any{
		"description": "Ny beskrivelse",
		"categories":  []any{"Debatt", "Kultur"},
		"seriesId":    nil,
	}
	if len(body) != len(want) {
		t.Errorf("sent %v, want %v", body, want)
	}
	for key, value := range want {
		got, ok := body[key]
		if !ok || !jsonEqual(got, value) {
			t.Errorf("sent %s = %v, want %v", key, got, value)
		}
	}
	if api.methods[len(api.methods)-1] != http.MethodPatch {
		t.Errorf("methods = %v", api.methods)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(stdout), &record); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, stdout)
	}
	if record["description"] != "Ny beskrivelse" || record["name"] != "Old title" {
		t.Errorf("record = %v", record)
	}
}

func TestVideoPatchFlagsWinOverFile(t *testing.T) {
	api := newVideoAPI(t)
	file := filepath.Join(t.TempDir(), "fields.json")
	if err := os.WriteFile(file, []byte(`{"name": "From file", "publishOnWeb": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runFK(t, api.fakeAPI, "video", "patch", "42", file, "-t", "From flag")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	body := api.lastBody(t)
	if body["name"] != "From flag" || body["publishOnWeb"] != false || len(body) != 2 {
		t.Errorf("sent %v", body)
	}
}

func TestVideoPatchWithoutFlagsReadsStdin(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, `{"publishOnWeb": false}`)
	_, stderr, code := runFK(t, api.fakeAPI, "video", "patch", "42")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if body := api.lastBody(t); body["publishOnWeb"] != false || len(body) != 1 {
		t.Errorf("sent %v", body)
	}
}

func TestVideoPatchWithNothingToChangeIsAUsageError(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, `{}`)
	_, stderr, code := runFK(t, api.fakeAPI, "video", "patch", "42")
	if code != exitUsage {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if len(api.methods) != 0 {
		t.Errorf("reached the server: %v", api.methods)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
