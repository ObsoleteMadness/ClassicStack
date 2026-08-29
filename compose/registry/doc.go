// Package registry is the name->factory component registry, populated by
// build-tagged init() so an absent build tag means a component is simply not
// registered (the §8 replacement for *_disabled.go).
//
// Ring: COMPOSE. Real types land in step C1.
package registry
