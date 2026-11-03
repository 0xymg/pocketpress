package app

// Version is the PocketPress application version.
//
// It is set as a build-time default and can be overridden at link time, e.g.:
//
//	go build -ldflags "-X github.com/pocketpress/pocketpress/internal/app.Version=0.2.0"
var Version = "0.1.0"
