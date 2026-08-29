// Package service is the parent of the per-service packages (afp, smb, netbios,
// macip). Each subpackage holds a Component consuming a DatagramLink (where
// applicable) and the core/fs + core/metastore interfaces.
//
// Ring: CORE (stdlib + core interfaces). Phase 1 placeholders land in step D3;
// real protocol logic is Phase 2.
package service
