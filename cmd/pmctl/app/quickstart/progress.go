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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"k8s.io/klog/v2"
)

// progress serializes what the steps write to the terminal: the lines of the
// spinner, and the lines reported while it runs.
type progress struct {
	mu sync.Mutex
	// spinning is set while a spinner is on the terminal.
	spinning bool
	// below is the number of lines the spinner has drawn below its first.
	below int
	// tasks are what the running step does at the moment, each shown on a
	// line of its own below the spinner.
	tasks []*task
}

// task is one of several things a step does at a time.
type task struct {
	o     *Options
	name  string
	start time.Time
	// status is the last line the task wrote, guarded by progress.mu.
	status string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// step runs fn as a named step and reports it the way kind does: a
// " ✓ name" line once it is done, " ✗ name" if it failed. On a terminal the
// step is shown while it runs, with a spinner and the time it has taken.
func (o *Options) step(name string, fn func() error) error {
	stop := o.spin(name)
	start := time.Now()
	err := fn()
	stop()

	o.report(name, time.Since(start), err)
	return err
}

// begin starts a task of the running step. On a terminal it is shown below
// the spinner of the step until end, with the time it has taken and the last
// line it wrote.
func (o *Options) begin(name string) *task {
	t := &task{o: o, name: name, start: time.Now()}
	o.progress.mu.Lock()
	defer o.progress.mu.Unlock()
	o.progress.tasks = append(o.progress.tasks, t)
	return t
}

// Write takes the output of the task; its last line is the status shown.
func (t *task) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimRight(string(p), "\r\n"), "\n")
	last := lines[len(lines)-1]
	// The part after the last carriage return is what a terminal shows.
	if i := strings.LastIndex(last, "\r"); i >= 0 {
		last = last[i+1:]
	}
	if last = strings.TrimSpace(last); last != "" {
		t.o.progress.mu.Lock()
		t.status = last
		t.o.progress.mu.Unlock()
	}
	return len(p), nil
}

// end finishes the task and reports it like a step.
func (t *task) end(err error) {
	p := &t.o.progress
	p.mu.Lock()
	p.tasks = slices.DeleteFunc(p.tasks, func(other *task) bool { return other == t })
	p.mu.Unlock()
	t.o.report(t.name, time.Since(t.start), err)
}

// report prints the line of a finished step or task, above the spinner if
// one runs.
func (o *Options) report(name string, took time.Duration, err error) {
	mark := "✓"
	if err != nil {
		mark = "✗"
	}
	o.progress.mu.Lock()
	defer o.progress.mu.Unlock()
	// The spinner draws itself again below the line.
	_, _ = fmt.Fprintf(o.ErrOut, "%s %s %s (%s)\n", o.progress.erase(), mark, name, took.Round(time.Second))
}

// erase returns what removes the spinner from the terminal, leaving the
// cursor where its first line began. Called with mu held.
func (p *progress) erase() string {
	if !p.spinning {
		return ""
	}
	up := ""
	if p.below > 0 {
		up = fmt.Sprintf("\x1b[%dA", p.below)
	}
	p.below = 0
	// To the first column, and clear from there to the end of the screen.
	return up + "\r\x1b[J"
}

// spin animates a line for the running step, and one for each of its tasks,
// until the returned function is called, which also removes them. It does
// nothing when the output is not a terminal, or when debug lines (-v) would
// be written in between.
func (o *Options) spin(name string) (stop func()) {
	f, ok := o.ErrOut.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || klog.V(1).Enabled() {
		return func() {}
	}
	p := &o.progress

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		start := time.Now()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			frame := spinnerFrames[i%len(spinnerFrames)]
			// A line that wraps would break the count of lines to erase.
			width, _, err := term.GetSize(int(f.Fd()))
			if err != nil || width <= 0 {
				width = 80
			}

			p.mu.Lock()
			lines := make([]string, 0, 1+len(p.tasks))
			lines = append(lines, fmt.Sprintf(" %s %s ... %s", frame, name, time.Since(start).Round(time.Second)))
			for _, t := range p.tasks {
				line := fmt.Sprintf("   %s %s ... %s", frame, t.name, time.Since(t.start).Round(time.Second))
				if t.status != "" {
					line += "  " + t.status
				}
				lines = append(lines, line)
			}
			for j, line := range lines {
				lines[j] = runewidth.Truncate(line, width-1, "…")
			}
			erase := p.erase()
			p.spinning = true
			p.below = len(lines) - 1
			_, _ = fmt.Fprint(f, erase+strings.Join(lines, "\n"))
			p.mu.Unlock()

			select {
			case <-done:
				p.mu.Lock()
				_, _ = fmt.Fprint(f, p.erase())
				p.spinning = false
				p.mu.Unlock()
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
