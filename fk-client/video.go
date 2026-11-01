package fk

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// VideoInfo is the part of a video record the CLI shows a person.
type VideoInfo struct {
	Id          int        `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Categories  []string   `json:"categories"`
	Duration    string     `json:"duration"`
	Org         *SimpleOrg `json:"org"`
	Series      *Series    `json:"series"`
	// Variants names the files registered for the video, spelt the way
	// "fk video download --variant" takes them.
	Variants []string `json:"variants"`
	URL      string   `json:"url"`
}

// Video fetches one video's record.
func (c *Client) Video(ctx context.Context, videoId int) (*VideoInfo, error) {
	resp, err := c.api.VideosRetrieveWithResponse(ctx, videoId)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	// Decoded by hand for the same reason as VideoFile: the files map is
	// keyed by variant, and variants the schema has not caught up with
	// should still show.
	var raw struct {
		Id           int      `json:"id"`
		Name         string   `json:"name"`
		Description  *string  `json:"description"`
		Categories   []string `json:"categories"`
		Duration     *string  `json:"duration"`
		Organization *struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
		Series *struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
		} `json:"series"`
		Files map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("decoding video %d: %w", videoId, err)
	}

	info := &VideoInfo{
		Id:          raw.Id,
		Title:       raw.Name,
		Description: deref(raw.Description),
		Categories:  raw.Categories,
		Duration:    deref(raw.Duration),
		Variants:    []string{},
		URL:         c.VideoURL(raw.Id),
	}
	if info.Categories == nil {
		info.Categories = []string{}
	}
	if raw.Organization != nil {
		info.Org = &SimpleOrg{Id: raw.Organization.Id, Name: raw.Organization.Name}
	}
	if raw.Series != nil {
		info.Series = &Series{Id: raw.Series.Id, Name: raw.Series.Name}
	}
	for key, value := range raw.Files {
		if string(value) == "null" {
			continue
		}
		info.Variants = append(info.Variants, snakeCase(key))
	}
	sort.Strings(info.Variants)
	return info, nil
}
