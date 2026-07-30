//go:build windows

package docker

// checkDiskPressure is a stub for Windows. The agent runs on Linux only,
// but tests may compile on Windows. Always returns normal pressure.
func checkDiskPressure(dir string) (int, string) {
	return 0, ""
}
