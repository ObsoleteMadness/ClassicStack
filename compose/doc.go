// Package compose is the wiring ring of the hexagonal architecture (§14).
//
// Ring: COMPOSE. Compose packages assemble the application: the component
// registry, the supervisor (lifecycle DAG, ordered start/stop, addressed
// reconfigure), and the stats/rate subscriber. Compose imports both core/ and
// adapter/ and decides which concrete adapters back which core interfaces.
//
// Compose is the only ring allowed to know about both contracts and their
// implementations; core/ and adapter/ never import compose/.
package compose
