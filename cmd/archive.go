package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

const (
	archiveFormatsChore  = "formats"
	archiveMetadataChore = "metadata"
	defaultQueuePriority = 0
	queueConcurrency     = 8
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Find and queue archive maintenance",
	Long: "Sweep django-api for videos whose registered files need maintenance.\n" +
		"This command does not access the media archive; workers inspect the archive\n" +
		"and make the final plan after a video is queued.",
	Args: cobra.NoArgs,
}

type archiveAPI interface {
	IngestFormats(context.Context) (*fk.DesiredFormats, error)
	ReadCatalogue(context.Context) (fk.Catalogue, error)
	IngestStatus(context.Context, int) (*fk.IngestJob, error)
	QueueBackfill(context.Context, int, int) error
}

type archiveOptions struct {
	apply    bool
	limit    int
	priority int
	quiet    bool
}

type archiveAction struct {
	name        string
	description string
}

type archivePlan struct {
	videoId int
	actions []archiveAction
	notes   []string
}

func (p archivePlan) describe() string {
	lines := []string{fmt.Sprintf("video %d", p.videoId)}
	for _, action := range p.actions {
		lines = append(lines, "  - "+action.description)
	}
	for _, note := range p.notes {
		lines = append(lines, "  ? "+note)
	}
	return strings.Join(lines, "\n")
}

type archiveSummary struct {
	videos   int
	withWork int
	actions  map[string]int
	notes    map[string]int
}

func newArchiveSummary() archiveSummary {
	return archiveSummary{actions: map[string]int{}, notes: map[string]int{}}
}

func (s *archiveSummary) add(plan archivePlan) {
	s.videos++
	if len(plan.actions) > 0 {
		s.withWork++
		for _, action := range plan.actions {
			s.actions[action.name]++
		}
	}
	for _, note := range plan.notes {
		s.notes[noteKind(note)]++
	}
}

func noteKind(note string) string {
	if before, _, ok := strings.Cut(note, ";"); ok {
		note = before
	}
	if before, _, ok := strings.Cut(note, " holds "); ok {
		note = before
	}
	return note
}

func (s archiveSummary) report() string {
	lines := []string{"", fmt.Sprintf("%d videos looked at, %d need something done.", s.videos, s.withWork)}
	if len(s.actions) > 0 {
		lines = append(lines, "")
		for _, item := range sortedCounts(s.actions) {
			lines = append(lines, fmt.Sprintf("  %7d  %s", item.count, item.name))
		}
	}
	if s.withWork > 0 {
		lines = append(lines,
			"",
			"  Every one of those fetches the original and re-encodes from it,",
			"  which is where essentially all of the wall-clock time goes.",
		)
	}
	if len(s.notes) > 0 {
		lines = append(lines, "", "  reported, not acted on:")
		for _, item := range sortedCounts(s.notes) {
			lines = append(lines, fmt.Sprintf("  %7d  %s", item.count, item.name))
		}
	}
	return strings.Join(lines, "\n")
}

type namedCount struct {
	name  string
	count int
}

func sortedCounts(counts map[string]int) []namedCount {
	items := make([]namedCount, 0, len(counts))
	for name, count := range counts {
		items = append(items, namedCount{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].name < items[j].name
		}
		return items[i].count > items[j].count
	})
	return items
}

func planFormats(video *fk.CatalogueVideo, desired map[string]int) archivePlan {
	result := archivePlan{videoId: video.Id}
	if !hasVariant(video.Files, "original") {
		if len(video.Files) > 0 {
			result.notes = append(result.notes, "no original is registered; nothing can be derived")
		}
		return result
	}

	formats := make([]string, 0, len(desired))
	for variant := range desired {
		formats = append(formats, variant)
	}
	sort.Strings(formats)

	for _, variant := range formats {
		registered := false
		have := 0
		for _, file := range video.Files {
			if file.Variant != variant {
				continue
			}
			registered = true
			if file.ProfileRevision > have {
				have = file.ProfileRevision
			}
		}
		want := desired[variant]
		if registered && have >= want {
			continue
		}

		description := fmt.Sprintf("produce %s (missing)", variant)
		if have != 0 {
			description = fmt.Sprintf("produce %s (revision %d -> %d)", variant, have, want)
		}
		result.actions = append(result.actions, archiveAction{
			name:        "ProduceFormat",
			description: description,
		})
	}
	return result
}

func planMetadata(video *fk.CatalogueVideo) archivePlan {
	result := archivePlan{videoId: video.Id}
	var original *fk.CatalogueFile
	for i := range video.Files {
		if video.Files[i].Variant == "original" {
			original = &video.Files[i]
			break
		}
	}
	if original == nil {
		return result
	}

	var missing []string
	if video.Duration == "" {
		missing = append(missing, "duration")
	}
	if video.Framerate == 0 {
		missing = append(missing, "framerate")
	}
	if len(missing) > 0 {
		if original.IntegratedLufs == nil {
			missing = append(missing, "loudness")
		}
		result.actions = append(result.actions, archiveAction{
			name:        "RefreshMetadata",
			description: "refresh " + strings.Join(missing, ", ") + " from the original",
		})
		return result
	}

	if original.IntegratedLufs == nil {
		result.notes = append(result.notes,
			"the original has no recorded loudness; measuring it would mean fetching a file "+
				"nothing else needs, and a track with nothing to measure would leave the column "+
				"as it found it and be asked for again on every run",
		)
	}
	return result
}

func hasVariant(files []fk.CatalogueFile, variant string) bool {
	for _, file := range files {
		if file.Variant == variant {
			return true
		}
	}
	return false
}

func runArchiveSweep(
	ctx context.Context,
	client archiveAPI,
	chore string,
	args []string,
	opts archiveOptions,
	out io.Writer,
	errOut io.Writer,
) error {
	if opts.limit < 0 {
		return fmt.Errorf("limit must not be negative")
	}
	if chore != archiveFormatsChore && chore != archiveMetadataChore {
		return fmt.Errorf("%q is not an archive chore", chore)
	}

	desired, err := client.IngestFormats(ctx)
	if err != nil {
		return err
	}
	image := desired.Image
	if image == "" {
		image = "(not reported)"
	}
	fmt.Fprintf(out, "Ingest image: %s\n", image)

	catalogue, err := client.ReadCatalogue(ctx)
	if err != nil {
		return err
	}
	selected, err := selectArchiveVideos(args, opts.limit, catalogue, errOut)
	if err != nil {
		return err
	}

	summary := newArchiveSummary()
	wanted := make([]int, 0)
	for _, id := range selected {
		video := catalogue[id]
		var plan archivePlan
		switch chore {
		case archiveFormatsChore:
			plan = planFormats(video, desired.Formats)
		case archiveMetadataChore:
			plan = planMetadata(video)
		}

		summary.add(plan)
		if len(plan.actions) > 0 {
			wanted = append(wanted, id)
		}
		if !opts.quiet && (len(plan.actions) > 0 || len(plan.notes) > 0) {
			fmt.Fprintln(out, plan.describe())
		}
	}

	fmt.Fprintln(out, summary.report())
	if !opts.apply {
		fmt.Fprintln(out, "\nNothing was changed. `--apply` puts this work in the queue.")
		return nil
	}
	if len(wanted) == 0 {
		fmt.Fprintln(out, "\nNothing to queue.")
		return nil
	}

	report := enqueueArchiveVideos(ctx, client, wanted, opts.priority)
	fmt.Fprintln(out)
	fmt.Fprintln(out, report.describe())
	fmt.Fprintln(out, "\nDrain it by scaling the pool: kubectl scale deployment/ingest-workers --replicas=N")
	if len(report.failed) > 0 {
		return fmt.Errorf("%d videos could not be queued", len(report.failed))
	}
	return nil
}

func selectArchiveVideos(args []string, limit int, catalogue fk.Catalogue, errOut io.Writer) ([]int, error) {
	if len(args) == 0 {
		ids := make([]int, 0, len(catalogue))
		for id := range catalogue {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		if limit > 0 && limit < len(ids) {
			ids = ids[:limit]
		}
		return ids, nil
	}

	selected := make([]int, 0, len(args))
	for _, raw := range args {
		id, err := strconv.Atoi(raw)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("invalid video ID %q", raw)
		}
		if catalogue[id] == nil {
			fmt.Fprintf(errOut, "No video %d in the catalogue; ignoring it\n", id)
			continue
		}
		selected = append(selected, id)
	}
	return selected, nil
}

type archiveQueueReport struct {
	enqueued       []int
	alreadyRunning []int
	failed         map[int]error
}

func (r archiveQueueReport) describe() string {
	lines := []string{fmt.Sprintf("%d videos queued.", len(r.enqueued))}
	if len(r.alreadyRunning) > 0 {
		ids := make([]string, 0, min(len(r.alreadyRunning), 5))
		for _, id := range r.alreadyRunning[:min(len(r.alreadyRunning), 5)] {
			ids = append(ids, strconv.Itoa(id))
		}
		suffix := ""
		if len(r.alreadyRunning) > 5 {
			suffix = "..."
		}
		lines = append(lines, fmt.Sprintf(
			"%d left alone: ingest is working on them now (%s%s).",
			len(r.alreadyRunning), strings.Join(ids, ", "), suffix,
		))
	}
	if len(r.failed) > 0 {
		lines = append(lines, fmt.Sprintf("%d could not be queued:", len(r.failed)))
		ids := make([]int, 0, len(r.failed))
		for id := range r.failed {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids[:min(len(ids), 10)] {
			lines = append(lines, fmt.Sprintf("  %d: %s", id, r.failed[id]))
		}
	}
	return strings.Join(lines, "\n")
}

type queueResult int

const (
	queueEnqueued queueResult = iota
	queueAlreadyRunning
)

func enqueueArchiveVideos(ctx context.Context, client archiveAPI, videoIds []int, priority int) archiveQueueReport {
	report := archiveQueueReport{failed: map[int]error{}}
	if len(videoIds) == 0 {
		return report
	}

	type result struct {
		state queueResult
		err   error
	}
	results := make([]result, len(videoIds))
	jobs := make(chan int)
	workers := min(queueConcurrency, len(videoIds))
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				state, err := enqueueArchiveVideo(ctx, client, videoIds[index], priority)
				results[index] = result{state: state, err: err}
			}
		}()
	}
	for index := range videoIds {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	for index, result := range results {
		id := videoIds[index]
		switch {
		case result.err != nil:
			report.failed[id] = result.err
		case result.state == queueAlreadyRunning:
			report.alreadyRunning = append(report.alreadyRunning, id)
		default:
			report.enqueued = append(report.enqueued, id)
		}
	}
	return report
}

func enqueueArchiveVideo(ctx context.Context, client archiveAPI, videoId, priority int) (queueResult, error) {
	job, err := client.IngestStatus(ctx, videoId)
	if err != nil {
		return queueEnqueued, err
	}
	if job.State == fk.IngestStateProbing ||
		job.State == fk.IngestStateArchiving ||
		job.State == fk.IngestStateTranscoding ||
		(job.State == fk.IngestStatePending && job.Kind == fk.IngestKindUpload) {
		return queueAlreadyRunning, nil
	}
	if err := client.QueueBackfill(ctx, videoId, priority); err != nil {
		return queueEnqueued, err
	}
	return queueEnqueued, nil
}

func newArchiveSweepCmd(use, short, chore string) *cobra.Command {
	opts := archiveOptions{}
	command := &cobra.Command{
		Use:   use + " [video-id ...]",
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := fk.Open()
			if err != nil {
				return err
			}
			return runArchiveSweep(
				cmd.Context(), client, chore, args, opts, cmd.OutOrStdout(), cmd.ErrOrStderr(),
			)
		},
	}
	command.Flags().BoolVar(&opts.apply, "apply", false, "Queue the work; without this, only print the plan")
	command.Flags().IntVar(&opts.limit, "limit", 0, "Stop after this many videos")
	command.Flags().IntVar(&opts.priority, "priority", defaultQueuePriority, "Claim order among waiting jobs; higher is sooner")
	command.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Print the image and summary only")
	return command
}

func init() {
	rootCmd.AddCommand(archiveCmd)
	archiveCmd.AddCommand(newArchiveSweepCmd(
		"backfill",
		"Report or queue videos with missing or stale derived formats",
		archiveFormatsChore,
	))
	archiveCmd.AddCommand(newArchiveSweepCmd(
		"refresh-metadata",
		"Report or queue videos missing duration, frame rate, or loudness",
		archiveMetadataChore,
	))
}
