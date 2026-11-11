/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package quickstart

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"

	"k8s.io/klog/v2"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// step runs fn as a named step and reports it the way kind does: a
// " ✓ name" line once it is done, " ✗ name" if it failed. On a terminal the
// step is shown while it runs, with a spinner and the time it has taken.
func (o *Options) step(name string, fn func() error) error {
	stop := o.spin(name)
	start := time.Now()
	err := fn()
	stop()

	mark := "✓"
	if err != nil {
		mark = "✗"
	}
	_, _ = fmt.Fprintf(o.ErrOut, " %s %s (%s)\n", mark, name, time.Since(start).Round(time.Second))
	return err
}

// spin animates a line for the running step until the returned function is
// called, which also clears the line. It does nothing when the output is not
// a terminal, or when debug lines (-v) would be written in between.
func (o *Options) spin(name string) (stop func()) {
	f, ok := o.ErrOut.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || klog.V(1).Enabled() {
		return func() {}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		start := time.Now()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			_, _ = fmt.Fprintf(f, "\r\x1b[K %s %s ... %s", spinnerFrames[i%len(spinnerFrames)], name, time.Since(start).Round(time.Second))
			select {
			case <-done:
				_, _ = fmt.Fprint(f, "\r\x1b[K")
				return
			case <-ticker.C:
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}
