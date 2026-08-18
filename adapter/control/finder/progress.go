package finder

// OpPhase is the kind of long-running Finder job.
type OpPhase string

const (
	PhaseCopying   OpPhase = "copying"
	PhaseMoving    OpPhase = "moving"
	PhaseExpanding OpPhase = "expanding"
	PhaseListing   OpPhase = "listing"
)

// OpProgress is one progress event streamed to the web UI during copy/move/expand.
type OpProgress struct {
	Phase      OpPhase `json:"phase"`
	Path       string  `json:"path,omitempty"`
	BytesDone  int64   `json:"bytesDone,omitempty"`
	BytesTotal int64   `json:"bytesTotal,omitempty"`
	DestName   string  `json:"destName,omitempty"`
	Done       bool    `json:"done,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// TransferRequest names a cross-session copy or move between two open catalogs.
type TransferRequest struct {
	SrcSession  string `json:"srcSession"`
	DestSession string `json:"destSession"`
	SrcID       uint32 `json:"srcId"`
	DestParent  uint32 `json:"destParentId"`
	DestName    string `json:"destName"`
	Replace     bool   `json:"replace"`
}

// ExpandRequest names an archive to expand in-place on a session catalog.
type ExpandRequest struct {
	SessionID string `json:"sessionId"`
	ID        uint32 `json:"id"`
}
