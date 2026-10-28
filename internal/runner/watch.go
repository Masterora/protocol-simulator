package runner

import (
	"os"
	"time"

	"gytest-go/internal/core"
)

// Poll the pathname (not an open descriptor) to support rename saves and
// directory-mounted ConfigMap/symlink updates. Require two stable observations.
type projectWatcher struct {
	path              string
	pending, accepted os.FileInfo
	since             time.Time
	lastError         string
}

func newProjectWatcher(path string) *projectWatcher { return &projectWatcher{path: path} }
func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
func (w *projectWatcher) poll() (core.Project, bool, error) {
	info, err := os.Stat(w.path)
	if err != nil {
		if err.Error() == w.lastError {
			return core.Project{}, false, nil
		}
		w.lastError = err.Error()
		w.pending = nil
		return core.Project{}, true, err
	}
	w.lastError = ""
	if sameFile(info, w.accepted) {
		return core.Project{}, false, nil
	}
	if !sameFile(info, w.pending) {
		w.pending = info
		w.since = time.Now()
		return core.Project{}, false, nil
	}
	if time.Since(w.since) < 400*time.Millisecond {
		return core.Project{}, false, nil
	}
	candidate, err := core.ReadProject(w.path)
	after, statErr := os.Stat(w.path)
	if statErr != nil || !sameFile(info, after) {
		w.pending = nil
		return core.Project{}, false, nil
	}
	w.accepted = info
	return candidate, true, err
}
