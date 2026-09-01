package fk

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForIngestReportsChangesAndStopsAtDone(t *testing.T) {
	// pending(nil%) -> probing(nil%) -> archiving(10%) -> archiving(90%, same
	// state, must still be reported) -> done. Each state is served twice to
	// verify that a repeated (state, percentage) pair is reported only once.
	states := []struct {
		state      string
		percentage *int
	}{
		{IngestStatePending, nil},
		{IngestStatePending, nil},
		{IngestStateProbing, nil},
		{IngestStateArchiving, intPtr(10)},
		{IngestStateArchiving, intPtr(90)},
		{IngestStateDone, intPtr(100)},
	}

	var call int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt32(&call, 1) - 1
		s := states[i]
		if s.state == "" {
			t.Fatalf("polled ingest status more times than expected (%d)", i+1)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"video":          1,
			"state":          s.state,
			"percentageDone": s.percentage,
		})
	})

	var reports []IngestJob
	job, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{
		StallTimeout: time.Second,
		OnUpdate: func(j IngestJob) {
			reports = append(reports, j)
		},
	})
	if err != nil {
		t.Fatalf("waitForIngest: %v", err)
	}
	if job.State != IngestStateDone {
		t.Errorf("final state = %q, want %q", job.State, IngestStateDone)
	}

	wantStates := []string{IngestStatePending, IngestStateProbing, IngestStateArchiving, IngestStateArchiving, IngestStateDone}
	if len(reports) != len(wantStates) {
		t.Fatalf("got %d reports, want %d: %+v", len(reports), len(wantStates), reports)
	}
	for i, want := range wantStates {
		if reports[i].State != want {
			t.Errorf("report[%d].State = %q, want %q", i, reports[i].State, want)
		}
	}
	if *reports[2].PercentageDone != 10 || *reports[3].PercentageDone != 90 {
		t.Errorf("archiving percentages = %d, %d, want 10, 90", *reports[2].PercentageDone, *reports[3].PercentageDone)
	}
}

func TestWaitForIngestFailedIsTerminal(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"video":     1,
			"state":     IngestStateFailed,
			"errorCode": "unsupported_codec",
		})
	})

	job, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{StallTimeout: time.Second})
	if err != nil {
		t.Fatalf("waitForIngest: %v", err)
	}
	if job.State != IngestStateFailed || job.ErrorCode != "unsupported_codec" {
		t.Errorf("unexpected job: %+v", job)
	}
}

func TestWaitForIngestGivesUpOnAStalledJob(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"video": 1, "state": IngestStateTranscoding})
	})

	_, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{StallTimeout: 5 * time.Millisecond})
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), IngestStateTranscoding) {
		t.Errorf("error = %q, want it to mention a timeout and the last state", err.Error())
	}
}

// A job that keeps reporting progress is followed however long it takes: the
// stall timeout bounds silence, not the length of the transcode.
func TestWaitForIngestFollowsASlowButMovingJob(t *testing.T) {
	const stall = 40 * time.Millisecond

	var call int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Percentage climbs by one per poll, so the job takes far longer
		// overall than it is ever allowed to stand still for.
		i := int(atomic.AddInt32(&call, 1))
		body := map[string]any{"video": 1, "state": IngestStateTranscoding, "percentageDone": i}
		if i > 40 {
			body = map[string]any{"video": 1, "state": IngestStateDone, "percentageDone": 100}
		}
		writeJSON(w, http.StatusOK, body)
	})

	job, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{StallTimeout: stall})
	if err != nil {
		t.Fatalf("waitForIngest: %v", err)
	}
	if job.State != IngestStateDone {
		t.Errorf("final state = %q, want %q", job.State, IngestStateDone)
	}
}

// A server that drops out for a while costs the watch nothing but a pause: it
// retries, and says where things stand once it is back.
func TestWaitForIngestRetriesTransientFailures(t *testing.T) {
	var call int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch i := atomic.AddInt32(&call, 1); {
		case i == 1:
			writeJSON(w, http.StatusOK, map[string]any{"video": 1, "state": IngestStateTranscoding, "percentageDone": 20})
		case i <= 4:
			http.Error(w, "bad gateway", http.StatusBadGateway)
		default:
			writeJSON(w, http.StatusOK, map[string]any{"video": 1, "state": IngestStateDone, "percentageDone": 100})
		}
	})

	var retries int
	var updates []string
	job, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{
		StallTimeout: time.Second,
		OnUpdate:     func(j IngestJob) { updates = append(updates, j.State) },
		OnRetry:      func(error, time.Duration) { retries++ },
	})
	if err != nil {
		t.Fatalf("waitForIngest: %v", err)
	}
	if job.State != IngestStateDone {
		t.Errorf("final state = %q, want %q", job.State, IngestStateDone)
	}
	if retries != 3 {
		t.Errorf("OnRetry called %d times, want 3", retries)
	}
	want := []string{IngestStateTranscoding, IngestStateDone}
	if len(updates) != len(want) || updates[0] != want[0] || updates[1] != want[1] {
		t.Errorf("updates = %v, want %v", updates, want)
	}
}

// Coming back in touch re-reports the job even when it did not move while we
// could not see it, so a display that said "reconnecting" can stop saying so.
func TestWaitForIngestReportsAgainAfterRecovering(t *testing.T) {
	var call int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt32(&call, 1)
		if i == 2 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		state := IngestStateTranscoding
		if i > 3 {
			state = IngestStateDone
		}
		writeJSON(w, http.StatusOK, map[string]any{"video": 1, "state": state, "percentageDone": 20})
	})

	var updates []IngestJob
	if _, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{
		StallTimeout: time.Second,
		OnUpdate:     func(j IngestJob) { updates = append(updates, j) },
	}); err != nil {
		t.Fatalf("waitForIngest: %v", err)
	}

	// transcoding 20%, transcoding 20% again after the gap, then done.
	if len(updates) != 3 {
		t.Fatalf("got %d updates, want 3: %+v", len(updates), updates)
	}
	if updates[0].State != IngestStateTranscoding || updates[1].State != IngestStateTranscoding {
		t.Errorf("updates = %+v, want the unchanged state repeated after recovery", updates)
	}
}

// A refusal will be repeated however often it is asked, so it ends the watch
// rather than being retried until the stall timeout runs out.
func TestWaitForIngestDoesNotRetryRefusals(t *testing.T) {
	var call int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&call, 1)
		writeJSON(w, http.StatusNotFound, map[string]any{
			"type":   "client_error",
			"errors": []map[string]any{{"code": "not_found", "detail": "No video matches the given query."}},
		})
	})

	var retries int
	_, err := c.waitForIngest(context.Background(), 1, time.Millisecond, IngestWatch{
		StallTimeout: time.Minute,
		OnRetry:      func(error, time.Duration) { retries++ },
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if retries != 0 {
		t.Errorf("OnRetry called %d times, want 0", retries)
	}
	if got := atomic.LoadInt32(&call); got != 1 {
		t.Errorf("polled %d times, want 1", got)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Errorf("err = %v, want an APIError with status 404", err)
	}
}

func TestWaitForIngestRespectsContextCancellation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"video": 1, "state": IngestStateTranscoding})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := c.waitForIngest(ctx, 1, 50*time.Millisecond, IngestWatch{StallTimeout: time.Minute})
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestUploadSendsExpectedMetadata(t *testing.T) {
	const videoId = 42
	const wantToken = "upload-tok-xyz"

	f, err := os.CreateTemp(t.TempDir(), "fk-upload-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("pretend this is video data"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	wantFileName := f.Name()[strings.LastIndex(f.Name(), "/")+1:]

	var uploadCreated bool
	// serverURL is filled in once httptest.NewServer returns, below. No
	// request reaches the handlers until then, so this is race-free.
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/videos/42/upload_token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"uploadToken": wantToken,
			"uploadUrl":   serverURL + "/upload",
		})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method creating upload: %s", r.Method)
		}

		got := decodeUploadMetadata(t, r.Header.Get("Upload-Metadata"))
		want := map[string]string{
			"videoID":      strconv.Itoa(videoId),
			"origFileName": wantFileName,
			"uploadToken":  wantToken,
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("Upload-Metadata[%q] = %q, want %q", k, got[k], v)
			}
		}

		uploadCreated = true
		w.Header().Set("Location", "/upload/session-1")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/upload/session-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method continuing upload: %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Upload-Offset", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	c, err := newClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Upload(context.Background(), videoId, f.Name()); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !uploadCreated {
		t.Error("upload was never created")
	}
}

func decodeUploadMetadata(t *testing.T, header string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, pair := range strings.Split(header, ",") {
		fields := strings.SplitN(strings.TrimSpace(pair), " ", 2)
		if len(fields) != 2 {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(fields[1])
		if err != nil {
			t.Fatalf("decoding Upload-Metadata value for %q: %v", fields[0], err)
		}
		out[fields[0]] = string(decoded)
	}
	return out
}
