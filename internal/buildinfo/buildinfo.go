// Package buildinfo contains application identity shared by CLI and HTTP clients.
package buildinfo

const (
	// Version is the current yaqt release version.
	Version = "0.1.0"
	// UserAgent identifies yaqt HTTP requests.
	UserAgent = "yaqt/" + Version
)
