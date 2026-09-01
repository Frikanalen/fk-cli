package fk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/eventials/go-tus"
)

// ProgressFunc is called with the number of bytes uploaded so far and the
// total size of the file, as an upload proceeds.
type ProgressFunc func(uploaded, total int64)

// Upload streams filespec to the ingest server without reporting progress.
func (c *Client) Upload(ctx context.Context, videoId int, filespec string) error {
	return c.UploadWithProgress(ctx, videoId, filespec, nil)
}

// UploadWithProgress fetches an upload token for videoId and streams filespec
// to the ingest server over tus, tagging the upload with the fields its
// pre-create hook requires: videoID, origFileName and uploadToken. If
// progress is non-nil it is called as each chunk lands.
func (c *Client) UploadWithProgress(ctx context.Context, videoId int, filespec string, progress ProgressFunc) error {
	token, err := c.UploadToken(ctx, videoId)
	if err != nil {
		return fmt.Errorf("fetching upload token: %w", err)
	}

	f, err := os.Open(filespec)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	metadata := tus.Metadata{
		"videoID":      strconv.Itoa(videoId),
		"origFileName": fi.Name(),
		"uploadToken":  token.UploadToken,
	}
	fingerprint := fmt.Sprintf("%s-%d-%s", fi.Name(), fi.Size(), fi.ModTime())
	upload := tus.NewUpload(f, fi.Size(), metadata, fingerprint)

	config := tus.DefaultConfig()
	tusClient, err := tus.NewClient(token.UploadURL, config)
	if err != nil {
		return fmt.Errorf("creating tus client: %w", err)
	}

	uploader, err := tusClient.CreateUpload(upload)
	if err != nil {
		if ce, ok := err.(tus.ClientError); ok && len(ce.Body) > 0 {
			return fmt.Errorf("starting upload: %w (%s)", err, ce.Body)
		}
		return fmt.Errorf("starting upload: %w", err)
	}

	if progress != nil {
		progress(0, fi.Size())

		// go-tus broadcasts progress with a blocking send, so this channel
		// has to be drained for as long as the uploader is alive; stopped
		// tells the consumer to keep draining but stop reporting once the
		// upload is over.
		updates := make(chan tus.Upload)
		uploader.NotifyUploadProgress(updates)

		stopped := make(chan struct{})
		defer close(stopped)

		go func() {
			for u := range updates {
				select {
				case <-stopped:
				default:
					progress(u.Offset(), u.Size())
				}
			}
		}()
	}

	if err := uploader.Upload(); err != nil {
		return err
	}

	if progress != nil {
		progress(fi.Size(), fi.Size())
	}
	return nil
}

// DefaultIngestStall is how long a watch follows an ingest job that reports
// nothing new before giving up on it. It bounds silence, not the job: a
// transcode that keeps reporting progress is followed for as long as it
// takes, however many hours that is.
const DefaultIngestStall = 30 * time.Minute

const (
	// defaultPollInterval is how often a healthy watch asks for the job's
	// state.
	defaultPollInterval = 2 * time.Second
	// maxPollBackoff caps how far apart the retries of a failing poll are
	// spaced, so a watch that has backed off still notices promptly when
	// the server comes back.
	maxPollBackoff = 30 * time.Second
)

// IngestWatch configures how WaitForIngest follows a job and what it reports
// while doing so. The zero value is a usable watch that reports nothing.
type IngestWatch struct {
	// StallTimeout bounds how long the job may report nothing new before
	// the watch gives up. Zero means DefaultIngestStall.
	StallTimeout time.Duration

	// OnUpdate, when set, is called with every changed observation of the
	// job: a new state, or a new percentage within the state it is already
	// in.
	OnUpdate func(job IngestJob)

	// OnRetry, when set, is called before each retry of a failed poll, with
	// the failure and how long polling has been failing for. A watch that
	// recovers reports the job through OnUpdate again even if nothing about
	// it changed while it was out of touch, so a display that said so can
	// stop saying so.
	OnRetry func(err error, failingFor time.Duration)
}

// WaitForIngest follows a video's ingest job until it reaches a terminal
// state (done or failed), reporting progress through w. It survives a server
// that goes away for a while -- polls that fail are retried with a widening
// backoff -- and returns an error only once the job has been silent, or
// unreachable, for w's stall timeout. Requests the server refuses outright,
// like an expired token or an unknown video, end the watch immediately.
//
// The job is the server's business, not the caller's: giving up on watching
// one does not cancel it, and a later WaitForIngest picks the same job up
// wherever it has got to.
func (c *Client) WaitForIngest(ctx context.Context, videoId int, w IngestWatch) (*IngestJob, error) {
	return c.waitForIngest(ctx, videoId, defaultPollInterval, w)
}

// waitForIngest is WaitForIngest with the poll interval broken out so tests
// can drive it without waiting on the wall clock.
func (c *Client) waitForIngest(ctx context.Context, videoId int, pollInterval time.Duration, w IngestWatch) (*IngestJob, error) {
	stall := w.StallTimeout
	if stall <= 0 {
		stall = DefaultIngestStall
	}

	var (
		last      *IngestJob // the most recent job we managed to read
		lastState string
		// lastPercentage starts at a value no reading can take, so the
		// first observation always counts as a change worth reporting.
		lastPercentage = -2
		// newsAt is when the job last told us something we did not already
		// know; the stall timeout is measured from it.
		newsAt = time.Now()
		// failingSince is when polling started failing, zero while healthy.
		failingSince time.Time
		backoff      = pollInterval
	)

	for {
		job, err := c.IngestStatus(ctx, videoId)
		if err != nil {
			if !retryablePoll(err) {
				return last, err
			}
			if failingSince.IsZero() {
				failingSince = time.Now()
			}
			failingFor := time.Since(failingSince)
			if failingFor >= stall {
				return last, fmt.Errorf("timed out waiting for ingest: could not reach the server for %s: %w",
					failingFor.Round(time.Second), err)
			}
			if w.OnRetry != nil {
				w.OnRetry(err, failingFor)
			}
			if err := sleep(ctx, backoff); err != nil {
				return last, err
			}
			backoff = min(2*backoff, maxPollBackoff)
			continue
		}

		if !failingSince.IsZero() {
			// Back in touch. Say where things stand now, whether or not
			// they moved while we could not see them.
			failingSince, backoff = time.Time{}, pollInterval
			lastState, lastPercentage = "", -2
		}
		last = job

		percentage := -1
		if job.PercentageDone != nil {
			percentage = *job.PercentageDone
		}
		if job.State != lastState || percentage != lastPercentage {
			newsAt = time.Now()
			lastState, lastPercentage = job.State, percentage
			if w.OnUpdate != nil {
				w.OnUpdate(*job)
			}
		}

		switch job.State {
		case IngestStateDone, IngestStateFailed:
			return job, nil
		}

		if silent := time.Since(newsAt); silent >= stall {
			return job, fmt.Errorf("timed out waiting for ingest: no progress for %s, last state %q",
				silent.Round(time.Second), job.State)
		}

		if err := sleep(ctx, pollInterval); err != nil {
			return job, err
		}
	}
}

// retryablePoll reports whether a failed poll is worth trying again. Network
// trouble and server-side faults come and go; a request the server refused --
// a bad token, a video that is not there -- would be refused again, so those
// end the watch at once.
func retryablePoll(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests:
			return true
		}
		return apiErr.Status >= 500
	}
	// Anything else got no answer at all: a refused connection, a dropped
	// TLS handshake, a name that would not resolve. All worth another go.
	return true
}

// sleep waits for d, or returns the context's error if it is cancelled first.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
