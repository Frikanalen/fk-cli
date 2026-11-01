package cmd

import (
	"testing"
)

func TestVideoCreateRequestLeavesUnsetIDsOut(t *testing.T) {
	opts := &videoCreateOptions{title: "My video", description: "desc", categories: []string{"news", "music"}}
	req := opts.request(func(string) bool { return false })

	if req.Title != "My video" || req.Description != "desc" {
		t.Errorf("unexpected title/description: %+v", req)
	}
	if len(req.Categories) != 2 || req.Categories[0] != "news" || req.Categories[1] != "music" {
		t.Errorf("unexpected categories: %v", req.Categories)
	}
	if req.OrgId != nil || req.SeriesId != nil {
		t.Errorf("expected no org or series, got %v / %v", req.OrgId, req.SeriesId)
	}
}

func TestVideoCreateRequestCarriesGivenIDs(t *testing.T) {
	opts := &videoCreateOptions{title: "x", categories: []string{"news"}, orgId: 42, seriesId: 7}
	req := opts.request(func(name string) bool { return name == "org" || name == "series" })

	if req.OrgId == nil || *req.OrgId != 42 {
		t.Errorf("expected org 42, got %v", req.OrgId)
	}
	if req.SeriesId == nil || *req.SeriesId != 7 {
		t.Errorf("expected series 7, got %v", req.SeriesId)
	}
}
