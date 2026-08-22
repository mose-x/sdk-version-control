// Package wailsrt defines the minimal Wails runtime surface that the
// internal service packages need. It deliberately does NOT import Wails:
// services depend on this interface (injected by main), so the business
// logic stays free of GUI-toolkit coupling and unit-testable with fakes.
package wailsrt

import "context"

// FileFilter mirrors wails runtime.FileFilter so internal packages never
// import the Wails module.
type FileFilter struct {
	DisplayName string
	Pattern     string
}

// Runtime is the minimal Wails runtime surface required by the service
// layer. Production code implements it with an adapter over
// github.com/wailsapp/wails/v2/pkg/runtime (see app.go); tests use fakes.
type Runtime interface {
	// Context returns the Wails runtime context: the event source for
	// install:progress / install:versions-refreshed / update:progress, the
	// parent for InstallSdk's installCtx, and the download context for
	// DownloadUpdate.
	Context() context.Context
	// EventsEmit pushes an event to the frontend. Event names and payload
	// shapes are part of the frontend contract and must not change.
	EventsEmit(eventName string, data ...any)
	// OpenFileDialog asks the user to pick a file. Used only by the
	// importer (SelectLocalFile).
	OpenFileDialog(title string, filters []FileFilter) (string, error)
	// OpenDirectoryDialog asks the user to pick a directory. Used only by
	// the importer (SelectLocalDir).
	OpenDirectoryDialog(title string) (string, error)
	// Quit closes the application window. Used only by the updater after
	// launching the platform swap script.
	Quit()
}
