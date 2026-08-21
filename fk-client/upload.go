package fk

import (
	"context"
	"fmt"
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

// WaitForIngest polls a video's ingest status until it reaches a terminal
// state (done or failed), calling report on every observed state change.
// It gives up and returns an error if timeout elapses first.
func (c *Client) WaitForIngest(ctx context.Context, videoId int, timeout time.Duration, report func(IngestJob)) (*IngestJob, error) {
	return c.waitForIngest(ctx, videoId, timeout, 2*time.Second, report)
}

// waitForIngest is WaitForIngest with the poll interval broken out so tests
// can drive it without waiting on the wall clock.
func (c *Client) waitForIngest(ctx context.Context, videoId int, timeout, pollInterval time.Duration, report func(IngestJob)) (*IngestJob, error) {
	deadline := time.Now().Add(timeout)
	var lastState string
	var lastPercentage int

	for {
		job, err := c.IngestStatus(ctx, videoId)
		if err != nil {
			return nil, err
		}

		percentage := -1
		if job.PercentageDone != nil {
			percentage = *job.PercentageDone
		}
		if report != nil && (job.State != lastState || percentage != lastPercentage) {
			report(*job)
			lastState = job.State
			lastPercentage = percentage
		}

		switch job.State {
		case IngestStateDone, IngestStateFailed:
			return job, nil
		}

		if time.Now().After(deadline) {
			return job, fmt.Errorf("timed out waiting for ingest to finish, last state %q", job.State)
		}

		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
