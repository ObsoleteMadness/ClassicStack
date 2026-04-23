package zip

import "github.com/pgodw/omnitalk/internal/testutil"

// Package-local aliases that let existing tests keep using the lowercase
// names. The real mocks live in internal/testutil so any future package
// with testing needs can share them.
type (
	mockPort   = testutil.MockPort
	mockRouter = testutil.MockRouter
)
