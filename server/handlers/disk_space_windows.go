//go:build windows

package handlers

// Windows desktop builds retain the configured application quotas. The
// production Ubuntu build supplies filesystem watermarks through Statfs.
func filesystemSpace(path string) (total, available uint64, ok bool) {
	return 0, 0, false
}
