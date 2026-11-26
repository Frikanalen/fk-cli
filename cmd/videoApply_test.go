package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func (v *videoAPI) writes() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for _, req := range v.requests {
		if !strings.HasPrefix(req, "GET ") {
			out = append(out, req)
		}
	}
	return out
}

func TestVideoGetYAMLKeepsOrderAndWritesTextAsBlocks(t *testing.T) {
	api := newVideoAPI(t)
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "get", "43", "--yaml")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "description: |\n  Line one\n  Line two\n") {
		t.Errorf("description is not a literal block:\n%s", stdout)
	}
	if strings.Index(stdout, "id:") > strings.Index(stdout, "name:") {
		t.Errorf("fields out of order:\n%s", stdout)
	}
}

func TestVideoApplySendsOnlyWhatDiffers(t *testing.T) {
	api := newVideoAPI(t)
	// A whole record from get, read back as YAML, with one field edited.
	yamlOut, _, _ := runFK(t, api.fakeAPI, "video", "get", "42", "--yaml")
	edited := strings.Replace(yamlOut, "Old description", "New description", 1)
	file := filepath.Join(t.TempDir(), "video.yaml")
	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "apply", file)
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != "video 42 configured: description\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if body := api.lastBody(t); !reflect.DeepEqual(body, map[string]any{"description": "New description"}) {
		t.Errorf("sent %v", body)
	}

	// Applying the same file again finds nothing to do.
	stdout, _, code = runFK(t, api.fakeAPI, "video", "apply", file)
	if code != exitOK || stdout != "video 42 unchanged\n" {
		t.Errorf("second apply: exit %d, stdout %q", code, stdout)
	}
	if got := api.writes(); len(got) != 1 {
		t.Errorf("writes = %v", got)
	}
}

func TestVideoApplyTakesSeveralRecords(t *testing.T) {
	for name, input := range map[string]string{
		"yaml stream": "id: 42\ndescription: A\n---\nid: 43\nname: B\n",
		"yaml list":   "- id: 42\n  description: A\n- id: 43\n  name: B\n",
		"json array":  `[{"id": 42, "description": "A"}, {"id": 43, "name": "B"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			api := newVideoAPI(t)
			withStdin(t, input)
			stdout, stderr, code := runFK(t, api.fakeAPI, "video", "apply", "--json")
			if code != exitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			var results []applyResult
			if err := json.Unmarshal([]byte(stdout), &results); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, stdout)
			}
			if len(results) != 2 || results[0].Changed[0] != "description" || results[1].Changed[0] != "name" {
				t.Errorf("results = %+v", results)
			}
			want := []string{"PATCH /api/videos/42", "PATCH /api/videos/43"}
			if got := api.writes(); !reflect.DeepEqual(got, want) {
				t.Errorf("writes = %v, want %v", got, want)
			}
		})
	}
}

func TestVideoApplyChecksEveryRecordFirst(t *testing.T) {
	for name, input := range map[string]string{
		"missing id":   "id: 42\ndescription: A\n---\ndescription: B\n",
		"repeated id":  "id: 42\ndescription: A\n---\nid: 42\ndescription: B\n",
		"not a record": "id: 42\n---\n- just\n- a list\n",
		"broken yaml":  "id: 42\ndescription: [unclosed\n",
	} {
		t.Run(name, func(t *testing.T) {
			api := newVideoAPI(t)
			withStdin(t, input)
			_, stderr, code := runFK(t, api.fakeAPI, "video", "apply")
			if code == exitOK {
				t.Errorf("exit 0, stderr %q", stderr)
			}
			if len(api.requests) != 0 {
				t.Errorf("reached the server: %v", api.requests)
			}
		})
	}
}

func TestVideoApplyCarriesOnPastAFailure(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, "id: 99\ndescription: A\n---\nid: 43\ndescription: B\n")
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "apply")
	if code != exitFailure {
		t.Errorf("exit %d", code)
	}
	if stderr != "fk: video 99: 404: Not found.\n" || stdout != "video 43 configured: description\n" {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestVideoApplyDryRunChangesNothing(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, "id: 42\ndescription: A\nseries: {id: 9}\n")
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "apply", "--dry-run")
	if code != exitOK || stdout != "video 42 would change: description\n" {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, "series is read-only") || !strings.Contains(stderr, "seriesId") {
		t.Errorf("stderr = %q", stderr)
	}
	if got := api.writes(); len(got) != 0 {
		t.Errorf("writes = %v", got)
	}
}

func TestVideoPatchAndPutTakeYAML(t *testing.T) {
	api := newVideoAPI(t)
	withStdin(t, "description: |\n  First\n  Second\npublishOnWeb: false\n")
	if _, stderr, code := runFK(t, api.fakeAPI, "video", "patch", "42"); code != exitOK {
		t.Fatalf("patch: exit %d, stderr %q", code, stderr)
	}
	want := map[string]any{"description": "First\nSecond\n", "publishOnWeb": false}
	if body := api.lastBody(t); !reflect.DeepEqual(body, want) {
		t.Errorf("patch sent %v", body)
	}

	withStdin(t, "name: Ny\ncategories: [Kultur]\nuploadedTime: 2026-11-26T18:00:00Z\n")
	if _, stderr, code := runFK(t, api.fakeAPI, "video", "put", "42"); code != exitOK {
		t.Fatalf("put: exit %d, stderr %q", code, stderr)
	}
	// The timestamp stays the string it was written as.
	if got := api.lastBody(t)["uploadedTime"]; got != "2026-11-26T18:00:00Z" {
		t.Errorf("put sent uploadedTime %#v", got)
	}
}

// fakeEditor writes a shell script that edits its file with sed, and points
// EDITOR at it.
func fakeEditor(t *testing.T, sedExpr string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "editor")
	body := "#!/bin/sh\nsed -i '" + sedExpr + "' \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
}

func TestVideoEditSendsTheChangedFields(t *testing.T) {
	api := newVideoAPI(t)
	fakeEditor(t, "s/Line two/Line 2/; s/^name: .*/name: Renamed/")
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "edit", "43")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != "video 43 configured: description, name\n" {
		t.Errorf("stdout = %q", stdout)
	}
	want := map[string]any{"description": "Line one\nLine 2\n", "name": "Renamed"}
	if body := api.lastBody(t); !reflect.DeepEqual(body, want) {
		t.Errorf("sent %v", body)
	}
}

func TestVideoEditWithoutChangesSendsNothing(t *testing.T) {
	api := newVideoAPI(t)
	fakeEditor(t, "s/nothing-matches-this//")
	_, stderr, code := runFK(t, api.fakeAPI, "video", "edit", "42")
	if code != exitOK || !strings.Contains(stderr, "no changes made") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if got := api.writes(); len(got) != 0 {
		t.Errorf("writes = %v", got)
	}
}

func TestVideoEditKeepsTheFileWhenRefused(t *testing.T) {
	api := newVideoAPI(t)
	fakeEditor(t, "s/^description: .*/description: [broken/")
	_, stderr, code := runFK(t, api.fakeAPI, "video", "edit", "42")
	if code != exitFailure || !strings.Contains(stderr, "hint: fix ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	path := strings.Fields(stderr[strings.Index(stderr, "hint: fix ")+len("hint: fix "):])[0]
	if _, err := os.Stat(path); err != nil {
		t.Errorf("edits were not kept: %v", err)
	}
	_ = os.Remove(path)
}

func TestVideoApplyWarnsAboutUnknownFieldsAndIgnoresTrailingNewlines(t *testing.T) {
	api := newVideoAPI(t)
	// 43's description is stored as "Line one\nLine two\n"; a server that
	// strips it would hold the same text without the newline.
	api.records["/api/videos/43"]["description"] = "Line one\nLine two"
	withStdin(t, "id: 43\ndescription: |\n  Line one\n  Line two\ndescripton: typo\nseriesId: 5\n")
	stdout, stderr, code := runFK(t, api.fakeAPI, "video", "apply")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != "video 43 configured: seriesId\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, `no field "descripton"`) {
		t.Errorf("stderr = %q", stderr)
	}
}
