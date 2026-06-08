package main

import (
	"github.com/ObsoleteMadness/ClassicStack/core/buf"
	_ "github.com/ObsoleteMadness/ClassicStack/core/component"
)

// main references the TinyGo-safe core subset so the gate has real code to
// compile and link. Touch a value from core/buf so the import is not elided.
func main() {
	// Print is the only side effect; on TinyGo this exercises the runtime.
	println("cs-tinygo: core/buf.FrameMax =", buf.FrameMax)
}
