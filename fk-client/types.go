package fk

// SimpleOrg is the minimal organization reference embedded in a User.
type SimpleOrg struct {
	Id   int
	Name string
}

// User is the authenticated user's own profile, as returned by GET /api/user.
type User struct {
	Id        int
	Email     string
	IsStaff   bool
	EditorOf  []SimpleOrg
	MemberOf  []SimpleOrg
	FirstName string
	LastName  string
}

// CreateVideoRequest is the input to Client.CreateVideo.
type CreateVideoRequest struct {
	Title       string
	Description string
	Categories  []string

	// OrgId only needs to be set when the creator edits more than one
	// organization; the server infers it otherwise.
	OrgId *int
}

// VideoUploadToken authorizes an upload of a video's file, as returned by
// GET /api/videos/{id}/upload_token.
type VideoUploadToken struct {
	UploadToken string
	UploadURL   string
}

// IngestJob reports the ingest pipeline's progress for a video's uploaded
// file, as returned by GET /api/videos/{id}/ingest.
type IngestJob struct {
	State          string
	PercentageDone *int
	ErrorCode      string
}

const (
	IngestStatePending     = "pending"
	IngestStateProbing     = "probing"
	IngestStateArchiving   = "archiving"
	IngestStateTranscoding = "transcoding"
	IngestStateDone        = "done"
	IngestStateFailed      = "failed"
)
