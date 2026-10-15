package access

import (
	"sync"
	"time"
)

const (
	maxFails   = 5                // failed link attempts allowed per window
	failWindow = 10 * time.Minute // also how long a blocked user waits
)

// failures counts failed link attempts per Telegram user so the bot can't be
// used to guess subscription links.
type failures struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[int64]*failure
}

type failure struct {
	n     int
	first time.Time
}

func newFailures(now func() time.Time) *failures {
	return &failures{now: now, m: map[int64]*failure{}}
}

// live returns the user's record if its window is still open. Caller holds mu.
func (f *failures) live(id int64) *failure {
	e := f.m[id]
	if e != nil && f.now().Sub(e.first) >= failWindow {
		delete(f.m, id)
		return nil
	}
	return e
}

func (f *failures) blocked(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.live(id)
	return e != nil && e.n >= maxFails
}

func (f *failures) add(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.m) > 1000 {
		for k := range f.m {
			f.live(k)
		}
	}
	if e := f.live(id); e != nil {
		e.n++
		return
	}
	f.m[id] = &failure{n: 1, first: f.now()}
}

func (f *failures) reset(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, id)
}
