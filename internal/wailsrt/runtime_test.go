package wailsrt

import (
	"context"
	"errors"
	"testing"
)

// Fake is a scriptable Runtime implementation for tests in other packages.
// It records emitted events and returns canned dialog results.
type Fake struct {
	Ctx         context.Context
	Events      []FakeEvent
	FileResult  string
	FileErr     error
	DirResult   string
	DirErr      error
	QuitCalled  bool
	LastFilters []FileFilter
	LastTitle   string
}

// FakeEvent records one EventsEmit call.
type FakeEvent struct {
	Name string
	Data []any
}

func (f *Fake) Context() context.Context {
	if f.Ctx != nil {
		return f.Ctx
	}
	return context.Background()
}

func (f *Fake) EventsEmit(eventName string, data ...any) {
	f.Events = append(f.Events, FakeEvent{Name: eventName, Data: data})
}

func (f *Fake) OpenFileDialog(title string, filters []FileFilter) (string, error) {
	f.LastTitle = title
	f.LastFilters = filters
	return f.FileResult, f.FileErr
}

func (f *Fake) OpenDirectoryDialog(title string) (string, error) {
	f.LastTitle = title
	return f.DirResult, f.DirErr
}

func (f *Fake) Quit() { f.QuitCalled = true }

// TestFakeSatisfiesRuntime pins the contract: the test fake must implement
// Runtime, and the interface must carry the five methods the services rely
// on (compile-time check).
func TestFakeSatisfiesRuntime(t *testing.T) {
	var rt Runtime = &Fake{}

	rt.EventsEmit("install:progress", map[string]any{"sdkType": "go"})
	if len(rt.(*Fake).Events) != 1 || rt.(*Fake).Events[0].Name != "install:progress" {
		t.Fatalf("event not recorded: %+v", rt.(*Fake).Events)
	}

	f := rt.(*Fake)
	f.FileResult = "/picked/file.zip"
	got, err := rt.OpenFileDialog("pick", []FileFilter{{DisplayName: "Archives", Pattern: "*.zip"}})
	if err != nil || got != "/picked/file.zip" {
		t.Fatalf("OpenFileDialog = %q, %v", got, err)
	}
	if f.LastTitle != "pick" || len(f.LastFilters) != 1 || f.LastFilters[0].Pattern != "*.zip" {
		t.Fatalf("dialog args not passed through: %q %+v", f.LastTitle, f.LastFilters)
	}

	f.DirErr = errors.New("cancelled")
	if _, err := rt.OpenDirectoryDialog("dir"); err == nil {
		t.Fatal("expected directory dialog error to propagate")
	}

	rt.Quit()
	if !f.QuitCalled {
		t.Fatal("Quit not recorded")
	}

	if rt.Context() == nil {
		t.Fatal("Context must never be nil")
	}
}
