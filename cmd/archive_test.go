package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github/frikanalen/fk-cli/fk-client"
)

func floatPtr(value float64) *float64 { return &value }

type fakeArchiveAPI struct {
	desired   *fk.DesiredFormats
	catalogue fk.Catalogue
	statuses  map[int]*fk.IngestJob
	queueErrs map[int]error

	mu     sync.Mutex
	queued map[int]int
}

func (f *fakeArchiveAPI) IngestFormats(context.Context) (*fk.DesiredFormats, error) {
	return f.desired, nil
}

func (f *fakeArchiveAPI) ReadCatalogue(context.Context) (fk.Catalogue, error) {
	return f.catalogue, nil
}

func (f *fakeArchiveAPI) IngestStatus(_ context.Context, id int) (*fk.IngestJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if job := f.statuses[id]; job != nil {
		return job, nil
	}
	return &fk.IngestJob{State: fk.IngestStateDone}, nil
}

func (f *fakeArchiveAPI) QueueBackfill(_ context.Context, id, priority int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.queueErrs[id]; err != nil {
		return err
	}
	f.queued[id] = priority
	return nil
}

func archiveFixture() *fakeArchiveAPI {
	desired := map[string]int{"dash": 2, "large_thumb": 1}
	settled := func(firstId int) []fk.CatalogueFile {
		return []fk.CatalogueFile{
			{Id: firstId, Variant: "original", IntegratedLufs: floatPtr(-23)},
			{Id: firstId + 1, Variant: "dash", ProfileRevision: 2},
			{Id: firstId + 2, Variant: "large_thumb", ProfileRevision: 1},
		}
	}
	return &fakeArchiveAPI{
		desired: &fk.DesiredFormats{Image: "ghcr.io/frikanalen/ingest:v1.2.3", Formats: desired},
		catalogue: fk.Catalogue{
			100: {Id: 100, Duration: "00:01:00", Framerate: 25000, Files: []fk.CatalogueFile{
				{Id: 1, Variant: "original", IntegratedLufs: floatPtr(-23)},
			}},
			200: {Id: 200, Duration: "00:01:00", Framerate: 0, Files: settled(10)},
			300: {Id: 300, Duration: "00:01:00", Framerate: 25000, Files: settled(20)},
			400: {Id: 400, Duration: "00:01:00", Framerate: 25000, Files: []fk.CatalogueFile{
				{Id: 30, Variant: "broadcast"},
			}},
		},
		statuses:  map[int]*fk.IngestJob{},
		queueErrs: map[int]error{},
		queued:    map[int]int{},
	}
}

func runArchiveFixture(t *testing.T, client *fakeArchiveAPI, chore string, args []string, opts archiveOptions) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := runArchiveSweep(context.Background(), client, chore, args, opts, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestArchiveBackfillDryRunReportsWithoutQueueing(t *testing.T) {
	client := archiveFixture()
	stdout, _, err := runArchiveFixture(t, client, archiveFormatsChore, nil, archiveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Ingest image: ghcr.io/frikanalen/ingest:v1.2.3",
		"video 100",
		"produce dash (missing)",
		"4 videos looked at, 1 need something done.",
		"2  ProduceFormat",
		"1  no original is registered",
		"Nothing was changed",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not contain %q:\n%s", want, stdout)
		}
	}
	if len(client.queued) != 0 {
		t.Errorf("dry run queued %#v", client.queued)
	}
}

func TestArchiveCommandsRunOnlyTheirOwnChore(t *testing.T) {
	client := archiveFixture()
	if _, _, err := runArchiveFixture(t, client, archiveMetadataChore, nil, archiveOptions{apply: true}); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(client.queued, map[int]int{200: 0}) {
		t.Errorf("queued = %#v, want only video 200", client.queued)
	}
}

func TestArchiveApplyWithNoWorkSaysThereIsNothingToQueue(t *testing.T) {
	client := archiveFixture()
	stdout, _, err := runArchiveFixture(
		t, client, archiveFormatsChore, []string{"300"}, archiveOptions{apply: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Nothing to queue.") {
		t.Errorf("output = %q", stdout)
	}
}

func TestArchiveApplyCarriesPriorityAndLeavesOtherWorkAlone(t *testing.T) {
	client := archiveFixture()
	client.statuses[100] = &fk.IngestJob{State: fk.IngestStatePending, Kind: "upload"}

	stdout, _, err := runArchiveFixture(t, client, archiveFormatsChore, nil, archiveOptions{apply: true, priority: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.queued) != 0 {
		t.Errorf("queued = %#v, want none", client.queued)
	}
	if !strings.Contains(stdout, "1 left alone") {
		t.Errorf("output does not report the untouched upload:\n%s", stdout)
	}

	client.statuses[100] = &fk.IngestJob{State: fk.IngestStateDone}
	if _, _, err := runArchiveFixture(t, client, archiveFormatsChore, nil, archiveOptions{apply: true, priority: 7}); err != nil {
		t.Fatal(err)
	}
	if client.queued[100] != 7 {
		t.Errorf("priority = %d, want 7", client.queued[100])
	}
}

func TestArchiveQueueContinuesAfterOneFailure(t *testing.T) {
	client := archiveFixture()
	client.catalogue[101] = &fk.CatalogueVideo{
		Id: 101, Duration: "00:01:00", Framerate: 25000,
		Files: []fk.CatalogueFile{{Id: 50, Variant: "original", IntegratedLufs: floatPtr(-23)}},
	}
	client.queueErrs[100] = errors.New("django-api said no")

	stdout, _, err := runArchiveFixture(t, client, archiveFormatsChore, nil, archiveOptions{apply: true})
	if err == nil {
		t.Fatal("expected the failed queue write to fail the command")
	}
	if _, ok := client.queued[101]; !ok {
		t.Error("video 101 was not queued after video 100 failed")
	}
	if !strings.Contains(stdout, "100: django-api said no") {
		t.Errorf("failure is missing from output:\n%s", stdout)
	}
}

func TestArchiveSelectionIsNumericAndLimitOnlyAppliesToWholeCatalogue(t *testing.T) {
	client := archiveFixture()
	client.catalogue[2] = &fk.CatalogueVideo{Id: 2}
	var stderr bytes.Buffer

	selected, err := selectArchiveVideos(nil, 2, client.catalogue, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected, []int{2, 100}) {
		t.Errorf("selected = %v, want [2 100]", selected)
	}

	selected, err = selectArchiveVideos([]string{"300", "999", "100"}, 1, client.catalogue, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected, []int{300, 100}) {
		t.Errorf("explicit selection = %v", selected)
	}
	if !strings.Contains(stderr.String(), "No video 999") {
		t.Errorf("unknown video was not reported: %s", stderr.String())
	}
}

func TestArchiveQuietKeepsImageAndSummary(t *testing.T) {
	client := archiveFixture()
	stdout, _, err := runArchiveFixture(t, client, archiveFormatsChore, nil, archiveOptions{quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "video 100") {
		t.Errorf("quiet output contains per-video plan:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Ingest image:") || !strings.Contains(stdout, "4 videos looked at") {
		t.Errorf("quiet output lost image or summary:\n%s", stdout)
	}
}

func TestPlanFormatsUsesNewestRegisteredRevision(t *testing.T) {
	video := &fk.CatalogueVideo{Id: 1, Files: []fk.CatalogueFile{
		{Variant: "original"},
		{Variant: "dash", ProfileRevision: 1},
		{Variant: "dash", ProfileRevision: 3},
	}}
	plan := planFormats(video, map[string]int{"dash": 2})
	if len(plan.actions) != 0 {
		t.Errorf("current replacement was planned again: %#v", plan.actions)
	}

	plan = planFormats(video, map[string]int{"dash": 4})
	if got := plan.actions[0].description; got != "produce dash (revision 3 -> 4)" {
		t.Errorf("description = %q", got)
	}
}

func TestPlanMetadataDoesNotQueueLoudnessAlone(t *testing.T) {
	video := &fk.CatalogueVideo{
		Id: 1, Duration: "00:01:00", Framerate: 25000,
		Files: []fk.CatalogueFile{{Id: 5, Variant: "original"}},
	}
	plan := planMetadata(video)
	if len(plan.actions) != 0 || len(plan.notes) != 1 {
		t.Fatalf("plan = %#v, want one note and no action", plan)
	}

	video.Framerate = 0
	plan = planMetadata(video)
	if got := plan.actions[0].description; got != "refresh framerate, loudness from the original" {
		t.Errorf("description = %q", got)
	}
}

func TestArchiveQueueReportIsDeterministic(t *testing.T) {
	report := archiveQueueReport{failed: map[int]error{
		10: errors.New("ten"),
		2:  errors.New("two"),
	}}
	if !strings.Contains(report.describe(), "  2: two\n  10: ten") {
		t.Errorf("failures are not sorted:\n%s", report.describe())
	}
}
