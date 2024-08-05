package gonotify

import (
	"context"
	"path/filepath"
	"sync"
)

// FileWatcher waits for events generated by filesystem for a specific list of file paths, including
// IN_CREATE for not yet existing files and IN_DELETE for removed.
type FileWatcher struct {
	C  chan FileEvent
	wg sync.WaitGroup
}

// WaitForStop blocks until all internal resources such as goroutines have terminated.
// Call this after cancelling the context passed to NewFileWatcher to deterministically ensure teardown is complete.
func (f *FileWatcher) WaitForStop() {
	f.wg.Wait()
}

// NewFileWatcher creates FileWatcher with provided inotify mask and list of files to wait events for.
func NewFileWatcher(ctx context.Context, mask uint32, files ...string) (*FileWatcher, error) {
	f := &FileWatcher{
		C: make(chan FileEvent),
	}

	ctx, cancel := context.WithCancel(ctx)

	inotify, err := NewInotify(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	expectedPaths := make(map[string]bool)

	for _, file := range files {
		err := inotify.AddWatch(filepath.Dir(file), mask)
		if err != nil {
			cancel()
			return nil, err
		}
		expectedPaths[file] = true
	}

	events := make(chan FileEvent)

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()

		for {
			raw, err := inotify.Read()

			if err != nil {
				close(events)
				return
			}

			for _, event := range raw {
				select {
				case <-ctx.Done():
					return
				case events <- FileEvent{
					InotifyEvent: event,
				}: //noop
				}
			}
		}
	}()

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:

				if !ok {
					select {
					case f.C <- FileEvent{
						Eof: true,
					}:
					case <-ctx.Done():
					}
					return
				}

				if !expectedPaths[event.Name] {
					continue
				}

				select {
				case f.C <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return f, nil
}
