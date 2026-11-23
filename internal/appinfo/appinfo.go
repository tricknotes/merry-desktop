// Package appinfo holds the names the app uses for itself.
//
// The product name is not decided yet (it must not contain "Docker"). Every
// user-visible name and every OS resource name derives from these constants,
// so renaming the app is a change in this file only.
package appinfo

const (
	// DisplayName is shown in the tray tooltip and window titles.
	DisplayName = "Merry Desktop"
	// ID is used for OS resources: the WSL distro, the named pipe, data directories.
	ID = "merry-desktop"
	// EngineName is the name of the VM that runs the container engine.
	EngineName = ID + "-engine"
)
