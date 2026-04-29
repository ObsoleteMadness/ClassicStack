//go:build afp || all

package afp

import (
	"errors"
	"fmt"
)

// ErrCopySourceReadEOF indicates a source read failure during copy that should
// map to AFP ErrEOFErr.
var ErrCopySourceReadEOF = errors.New("copy source read eof")

// NotSupportedError indicates a filesystem operation exists but is not
// supported by a specific backend.
type NotSupportedError struct {
	Operation string
}

func (e *NotSupportedError) Error() string {
	if e == nil || e.Operation == "" {
		return "not supported"
	}
	return fmt.Sprintf("not supported: %s", e.Operation)
}

func newNotSupported(op string) error {
	return &NotSupportedError{Operation: op}
}

func isNotSupported(err error) bool {
	var ns *NotSupportedError
	return errors.As(err, &ns)
}
