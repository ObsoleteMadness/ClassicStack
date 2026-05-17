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
//
// The legacy [SMB.Shares.<key>] table key is also accepted for now,
// with a one-time deprecation warning. Future commits may drop the
// alias.
func loadSMBShares(src config.Source, fromConfigFile bool, flagShares []string) []smb.ShareConfig {
	if fromConfigFile {
		return loadSMBSharesFromConfig(src)
	}
	return loadSMBSharesFromFlags(flagShares)
}

func loadSMBSharesFromConfig(src config.Source) []smb.ShareConfig {
	if src.K == nil {
		return nil
	}
	prefix := ""
	switch {
	case src.K.Exists("SMB.Volumes"):
		prefix = "SMB.Volumes"
	case src.K.Exists("SMB.Shares"):
		prefix = "SMB.Shares"
		netlog.Warn("[MAIN][SMB] [SMB.Shares.*] is deprecated; rename to [SMB.Volumes.*]")
	default:
		return nil
	}
	keys := src.K.MapKeys(prefix)
	if len(keys) == 0 {
		return nil
	}
	out := make([]smb.ShareConfig, 0, len(keys))
	for _, key := range keys {
		base := prefix + "." + key
		share := smb.ShareConfig{
			Name:     stringWithDefault(src.K, base+".name", key),
			Path:     stringWithDefault(src.K, base+".path", ""),
			FSType:   stringWithDefault(src.K, base+".fs_type", "local_fs"),
			ReadOnly: boolWithDefault(src.K, base+".read_only", false),
		}
		if strings.TrimSpace(share.Path) == "" {
			netlog.Warn("[MAIN][SMB] [%s.%s] missing path; skipping", prefix, key)
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
