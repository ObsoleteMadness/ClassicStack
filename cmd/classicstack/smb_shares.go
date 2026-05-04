package main

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service/smb"
)

// loadSMBShares assembles the SMB share list from whichever source is
// active. In TOML mode it reads [SMB.Volumes.<key>] sections; in flag
// mode it parses "Name:Path" entries from -smb-share. The two sources
// are not merged — flag-mode is mutually exclusive with -config.
func loadSMBShares(src config.Source, fromConfigFile bool, flagShares []string) []smb.ShareConfig {
	if fromConfigFile {
		return loadSMBSharesFromConfig(src)
	}
	return loadSMBSharesFromFlags(flagShares)
}

func loadSMBSharesFromConfig(src config.Source) []smb.ShareConfig {
	if src.K == nil || !src.K.Exists("SMB.Volumes") {
		return nil
	}
	raw := src.K.StringMap("SMB.Volumes")
	if len(raw) == 0 {
		return nil
	}
	keys := src.K.MapKeys("SMB.Volumes")
	out := make([]smb.ShareConfig, 0, len(keys))
	for _, key := range keys {
		base := "SMB.Volumes." + key
		share := smb.ShareConfig{
			Name:     stringWithDefault(src.K, base+".name", key),
			Path:     stringWithDefault(src.K, base+".path", ""),
			FSType:   stringWithDefault(src.K, base+".fs_type", "local_fs"),
			ReadOnly: boolWithDefault(src.K, base+".read_only", false),
		}
		if strings.TrimSpace(share.Path) == "" {
			netlog.Warn("[MAIN][SMB] [SMB.Volumes.%s] missing path; skipping", key)
			continue
		}
		out = append(out, share)
	}
	return out
}

func loadSMBSharesFromFlags(flagShares []string) []smb.ShareConfig {
	if len(flagShares) == 0 {
		return nil
	}
	out := make([]smb.ShareConfig, 0, len(flagShares))
	for _, raw := range flagShares {
		idx := strings.Index(raw, ":")
		if idx <= 0 || idx == len(raw)-1 {
			netlog.Warn("[MAIN][SMB] invalid -smb-share %q (want Name:Path); skipping", raw)
			continue
		}
		out = append(out, smb.ShareConfig{
			Name:   raw[:idx],
			Path:   raw[idx+1:],
			FSType: "local_fs",
		})
	}
	return out
}
