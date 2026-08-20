package cmd

import (
	"testing"

	"github.com/spf13/pflag"
)

// newVideoCreateFlagSet mirrors createCmd's flag registration in
// videoCreate.go's init(), so tests can drive newVideoFromFlags without
// going through cobra.
func newVideoCreateFlagSet(t *testing.T) *pflag.FlagSet {
	t.Helper()
	flags := pflag.NewFlagSet("create", pflag.ContinueOnError)
	flags.StringP("title", "t", "", "Title of video")
	flags.StringP("description", "d", "", "Description of video")
	flags.StringSliceP("category", "c", []string{}, "Category name (repeatable)")
	flags.IntP("org-id", "o", 0, "Organization ID")
	return flags
}

func TestNewVideoFromFlagsWithoutOrgId(t *testing.T) {
	flags := newVideoCreateFlagSet(t)
	if err := flags.Parse([]string{"--title", "My video", "--description", "desc", "--category", "news", "--category", "music"}); err != nil {
		t.Fatal(err)
	}

	req, err := newVideoFromFlags(flags)
	if err != nil {
		t.Fatalf("newVideoFromFlags: %v", err)
	}

	if req.Title != "My video" || req.Description != "desc" {
		t.Errorf("unexpected title/description: %+v", req)
	}
	if len(req.Categories) != 2 || req.Categories[0] != "news" || req.Categories[1] != "music" {
		t.Errorf("unexpected categories: %v", req.Categories)
	}
	if req.OrgId != nil {
		t.Errorf("OrgId = %v, want nil when --org-id was not passed", *req.OrgId)
	}
}

func TestNewVideoFromFlagsWithOrgId(t *testing.T) {
	flags := newVideoCreateFlagSet(t)
	if err := flags.Parse([]string{"--title", "My video", "--category", "news", "--org-id", "3"}); err != nil {
		t.Fatal(err)
	}

	req, err := newVideoFromFlags(flags)
	if err != nil {
		t.Fatalf("newVideoFromFlags: %v", err)
	}

	if req.OrgId == nil || *req.OrgId != 3 {
		t.Errorf("OrgId = %v, want pointer to 3", req.OrgId)
	}
}

func TestNewVideoFromFlagsOrgIdZeroIsStillExplicit(t *testing.T) {
	// A user who explicitly passes --org-id 0 should have that sent
	// through, distinct from not passing the flag at all.
	flags := newVideoCreateFlagSet(t)
	if err := flags.Parse([]string{"--title", "My video", "--category", "news", "--org-id", "0"}); err != nil {
		t.Fatal(err)
	}

	req, err := newVideoFromFlags(flags)
	if err != nil {
		t.Fatalf("newVideoFromFlags: %v", err)
	}

	if req.OrgId == nil || *req.OrgId != 0 {
		t.Errorf("OrgId = %v, want pointer to 0", req.OrgId)
	}
}
