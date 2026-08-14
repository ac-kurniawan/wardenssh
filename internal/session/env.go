package session

import (
	"os"
	"strings"
)

// MergeEnv returns the parent process environment with every key named in
// extra removed from the inherited list, then the extra entries appended.
// This makes the provided values deterministically win on all platforms: on
// Unix, os/exec keeps the FIRST occurrence of a duplicated key, so a stale
// SSH_ASKPASS / SSH_AUTH_SOCK in the ambient environment would otherwise
// silently override the values WardenSSH injects.
func MergeEnv(extra []string) []string {
	named := make(map[string]bool, len(extra))
	for _, kv := range extra {
		key, _, _ := strings.Cut(kv, "=")
		named[key] = true
	}

	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if named[key] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}
