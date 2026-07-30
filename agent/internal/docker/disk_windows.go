//go:build windows

package docker

// checkDiskPressure is a stub for Windows. The agent runs on Linux only,
// but tests may compile on Windows. Always returns normal pressure.
func checkDiskPressure(dir string) (int, string) {
	return 0, ""
}

// GetTotalRAMMB is a stub for Windows. The agent runs on Linux only.
// Returns 0 since total RAM is unknown on Windows.
func GetTotalRAMMB() int64 {
	return 0
}
