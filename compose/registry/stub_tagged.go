//go:build registrytag

package registry

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

// taggedRegistered reflects whether the build-tag-gated factory is present in this build.
// Under the `registrytag` build tag this file wins, registers "stub-tagged", and sets it true.
const taggedRegistered = true

type taggedStub struct{}

func (taggedStub) Name() string                { return "stub-tagged" }
func (taggedStub) Start(context.Context) error { return nil }
func (taggedStub) Stop(context.Context) error  { return nil }

func init() {
	Register("stub-tagged", func(*BuildContext) (component.Component, error) {
		return taggedStub{}, nil
	})
}
