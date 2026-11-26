package fk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// VideoRecord fetches a video's record exactly as the API returns it, every
// field included, so it can be edited and sent back with ReplaceVideo.
// Read-only fields such as id, files and organization come along; the API
// ignores them on the way back in.
func (c *Client) VideoRecord(ctx context.Context, videoId int) (json.RawMessage, error) {
	resp, err := c.api.VideosRetrieveWithResponse(ctx, videoId)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// ReplaceVideo sends a whole video record with PUT /api/videos/{id} and
// returns the record as it stands afterwards. Writable fields left out keep
// their current value, but name and categories are required.
func (c *Client) ReplaceVideo(ctx context.Context, videoId int, record json.RawMessage) (json.RawMessage, error) {
	if err := checkObject(record); err != nil {
		return nil, err
	}
	resp, err := c.api.VideosUpdateWithBodyWithResponse(ctx, videoId, "application/json", bytes.NewReader(record))
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// PatchVideo changes only the fields given, with PATCH /api/videos/{id}, and
// returns the record as it stands afterwards.
func (c *Client) PatchVideo(ctx context.Context, videoId int, fields json.RawMessage) (json.RawMessage, error) {
	if err := checkObject(fields); err != nil {
		return nil, err
	}
	resp, err := c.api.VideosPartialUpdateWithBodyWithResponse(ctx, videoId, "application/json", bytes.NewReader(fields))
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// checkObject refuses anything but a JSON object before it reaches the
// server, whose answer to a stray array or truncated file is less clear.
func checkObject(body json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return fmt.Errorf("video record must be a JSON object: %w", err)
	}
	return nil
}
