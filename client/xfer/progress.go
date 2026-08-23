package xfer

// Progress reports byte progress during a CopyCtx transfer.
type Progress struct {
	Path       string
	BytesDone  int64
	BytesTotal int64
	IsDir      bool
}
