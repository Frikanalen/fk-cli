package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	apiclient "github/frikanalen/fk-cli/fk-client/generated"
)

func intPtr(i int) *int { return &i }

// writeJSON writes v as a JSON response. The generated client only parses a
// response body as JSON if the Content-Type header says so, which
// http.ResponseWriter does not set on its own.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestCheckResponse(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string // "" means no error
	}{
		{name: "2xx is fine", status: 200, body: `{}`},
		{
			name:    "single structured error",
			status:  400,
			body:    `{"type":"validation_error","errors":[{"code":"required","detail":"This field is required.","attr":"name"}]}`,
			wantErr: `400: name: This field is required.`,
		},
		{
			name:   "multiple structured errors joined",
			status: 400,
			body: `{"type":"validation_error","errors":[
				{"code":"required","detail":"This field is required.","attr":"name"},
				{"code":"blank","detail":"Categories may not be empty.","attr":"categories"}
			]}`,
			wantErr: `400: name: This field is required.; categories: Categories may not be empty.`,
		},
		{
			name:    "error with no attr",
			status:  401,
			body:    `{"type":"client_error","errors":[{"code":"not_authenticated","detail":"Authentication credentials were not provided.","attr":null}]}`,
			wantErr: `401: Authentication credentials were not provided.`,
		},
		{
			name:    "non-JSON body falls back to raw text",
			status:  502,
			body:    "  upstream connect error  \n",
			wantErr: `502: upstream connect error`,
		},
		{
			name:    "JSON body with no errors array falls back to raw text",
			status:  500,
			body:    `{"detail":"internal server error"}`,
			wantErr: `500: {"detail":"internal server error"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkResponse(tc.status, []byte(tc.body))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestDeref(t *testing.T) {
	if got := deref[int](nil); got != 0 {
		t.Errorf("deref(nil) = %d, want 0", got)
	}
	if got := deref(intPtr(42)); got != 42 {
		t.Errorf("deref(&42) = %d, want 42", got)
	}
}

func TestSimpleOrgs(t *testing.T) {
	if got := simpleOrgs(nil); got != nil {
		t.Errorf("simpleOrgs(nil) = %v, want nil", got)
	}

	in := []apiclient.SimpleOrg{
		{Id: intPtr(1), Name: "Nabolaget"},
		{Id: nil, Name: "No ID"},
	}
	got := simpleOrgs(&in)
	want := []SimpleOrg{{Id: 1, Name: "Nabolaget"}, {Id: 0, Name: "No ID"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("simpleOrgs(%v) = %v, want %v", in, got, want)
	}
}

// newTestClient builds a Client against a test server, bypassing viper.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c, err := newClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func TestClientLoginSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/obtain-token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Username != "user@example.com" || body.Password != "hunter2" {
			t.Fatalf("unexpected credentials: %+v", body)
		}

		writeJSON(w, http.StatusOK, map[string]string{"token": "abc123"})
	})

	if err := c.Login(context.Background(), "user@example.com", "hunter2"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.Token() != "abc123" {
		t.Errorf("Token() = %q, want %q", c.Token(), "abc123")
	}
}

func TestClientLoginFailure(t *testing.T) {
	// newTestClient seeds a "test-token" the same way Open() would if a
	// previous login had already succeeded; a failed Login must leave it
	// alone rather than clobbering it with an empty one.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"validation_error","errors":[{"code":"authorization","detail":"Unable to log in with provided credentials.","attr":"non_field_errors"}]}`))
	})

	err := c.Login(context.Background(), "user@example.com", "wrong")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	const want = "400: non_field_errors: Unable to log in with provided credentials."
	if err.Error() != want {
		t.Errorf("Login error = %q, want %q", err.Error(), want)
	}
	if c.Token() != "test-token" {
		t.Errorf("Token() = %q after failed login, want the pre-existing token left untouched", c.Token())
	}
}

func TestClientProfileSendsAuthHeaderAndParsesOrgs(t *testing.T) {
	var gotAuth string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet || r.URL.Path != "/api/user" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         7,
			"email":      "editor@example.com",
			"isStaff":    false,
			"dateJoined": "2024-01-01T00:00:00Z",
			"editorOf":   []map[string]any{{"id": 1, "name": "Org One"}},
			"memberOf":   []map[string]any{},
			"firstName":  "Ada",
			"lastName":   "Lovelace",
		})
	})

	user, err := c.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}

	if gotAuth != "Token test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Token test-token")
	}
	if user.Id != 7 || user.Email != "editor@example.com" || user.FirstName != "Ada" {
		t.Errorf("unexpected user: %+v", user)
	}
	if len(user.EditorOf) != 1 || user.EditorOf[0].Name != "Org One" {
		t.Errorf("unexpected EditorOf: %+v", user.EditorOf)
	}
	if len(user.MemberOf) != 0 {
		t.Errorf("unexpected MemberOf: %+v", user.MemberOf)
	}
}

func TestClientCreateVideo(t *testing.T) {
	cases := []struct {
		name     string
		orgId    *int
		seriesId *int
		want     map[string]any
	}{
		{
			name:  "without an explicit org, the field is omitted so the server infers it",
			orgId: nil,
			want: map[string]any{
				"categories":  []any{"news"},
				"description": "a test video",
				"name":        "Test video",
			},
		},
		{
			name:  "an explicit org is sent through",
			orgId: intPtr(3),
			want: map[string]any{
				"categories":   []any{"news"},
				"description":  "a test video",
				"name":         "Test video",
				"organization": float64(3),
			},
		},
		{
			name:     "a series files the video as an episode of it",
			seriesId: intPtr(8),
			want: map[string]any{
				"categories":  []any{"news"},
				"description": "a test video",
				"name":        "Test video",
				"seriesId":    float64(8),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/videos" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				rawBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if err := json.Unmarshal(rawBody, &got); err != nil {
					t.Fatalf("decoding request body: %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("request body = %#v, want %#v", got, tc.want)
				}

				writeJSON(w, http.StatusCreated, map[string]any{
					"id":         42,
					"name":       "Test video",
					"categories": []string{"news"},
				})
			})

			req := CreateVideoRequest{
				Title:       "Test video",
				Description: "a test video",
				Categories:  []string{"news"},
				OrgId:       tc.orgId,
				SeriesId:    tc.seriesId,
			}

			id, err := c.CreateVideo(context.Background(), req)
			if err != nil {
				t.Fatalf("CreateVideo: %v", err)
			}
			if id != 42 {
				t.Errorf("CreateVideo id = %d, want 42", id)
			}
		})
	}
}

func TestClientUploadToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/videos/42/upload_token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"uploadToken": "tok-xyz",
			"uploadUrl":   "https://staging.frikanalen.no/upload",
		})
	})

	token, err := c.UploadToken(context.Background(), 42)
	if err != nil {
		t.Fatalf("UploadToken: %v", err)
	}
	if token.UploadToken != "tok-xyz" || token.UploadURL != "https://staging.frikanalen.no/upload" {
		t.Errorf("unexpected token: %+v", token)
	}
}

func TestClientIngestStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/videos/42/ingest" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"video":          42,
			"state":          "transcoding",
			"percentageDone": 55,
		})
	})

	job, err := c.IngestStatus(context.Background(), 42)
	if err != nil {
		t.Fatalf("IngestStatus: %v", err)
	}
	if job.State != IngestStateTranscoding {
		t.Errorf("job.State = %q, want %q", job.State, IngestStateTranscoding)
	}
	if job.PercentageDone == nil || *job.PercentageDone != 55 {
		t.Errorf("job.PercentageDone = %v, want 55", job.PercentageDone)
	}
}

func TestVideoURLPointsAtTheDeploymentsWebsite(t *testing.T) {
	cases := map[string]string{
		"https://frikanalen.no":  "https://frikanalen.no/video/628648",
		"https://frikanalen.no/": "https://frikanalen.no/video/628648",
		"http://localhost:8000":  "http://localhost:8000/video/628648",
	}
	for base, want := range cases {
		c, err := newClient(base, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := c.VideoURL(628648); got != want {
			t.Errorf("VideoURL with base %q = %q, want %q", base, got, want)
		}
	}
}

func TestClientListSeries(t *testing.T) {
	var gotAuth, gotOrg string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.URL.Query().Get("organization")
		if r.Method != http.MethodGet || r.URL.Path != "/api/series" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"count": 2,
			"results": []map[string]any{
				{"id": 1, "name": "Morgensending"},
				{"id": 4, "name": "Kveldssending"},
			},
		})
	})

	series, err := c.ListSeries(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}

	if gotAuth != "Token test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Token test-token")
	}
	// The org is filtered server-side; sending it is the whole point of
	// the mandatory argument.
	if gotOrg != "3" {
		t.Errorf("organization query = %q, want %q", gotOrg, "3")
	}
	want := []Series{{Id: 1, Name: "Morgensending"}, {Id: 4, Name: "Kveldssending"}}
	if !reflect.DeepEqual(series, want) {
		t.Errorf("ListSeries = %#v, want %#v", series, want)
	}
}

func TestClientListSeriesWalksEveryPage(t *testing.T) {
	// A full first page has to be followed by a request for the next one,
	// or a deployment with more series than fit in a page silently loses
	// the tail of the list.
	total := seriesPageSize + 3
	var gotOffsets []string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotOffsets = append(gotOffsets, r.URL.Query().Get("offset"))
		if got := r.URL.Query().Get("limit"); got != strconv.Itoa(seriesPageSize) {
			t.Errorf("limit = %q, want %d", got, seriesPageSize)
		}
		// Every page has to stay scoped to the same organization.
		if got := r.URL.Query().Get("organization"); got != "3" {
			t.Errorf("organization = %q, want %q", got, "3")
		}

		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("offset: %v", err)
		}

		results := []map[string]any{}
		for i := offset; i < total && i < offset+seriesPageSize; i++ {
			results = append(results, map[string]any{"id": i, "name": fmt.Sprintf("Series %d", i)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": total, "results": results})
	})

	series, err := c.ListSeries(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}

	if len(series) != total {
		t.Fatalf("got %d series, want %d", len(series), total)
	}
	if series[0].Id != 0 || series[total-1].Id != total-1 {
		t.Errorf("unexpected first/last series: %+v / %+v", series[0], series[total-1])
	}
	wantOffsets := []string{"0", strconv.Itoa(seriesPageSize)}
	if !reflect.DeepEqual(gotOffsets, wantOffsets) {
		t.Errorf("requested offsets = %v, want %v", gotOffsets, wantOffsets)
	}
}

func TestClientListSeriesPropagatesErrors(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"client_error","errors":[{"code":"not_authenticated","detail":"Authentication credentials were not provided.","attr":null}]}`))
	})

	_, err := c.ListSeries(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	const want = "401: Authentication credentials were not provided."
	if err.Error() != want {
		t.Errorf("ListSeries error = %q, want %q", err.Error(), want)
	}
}
