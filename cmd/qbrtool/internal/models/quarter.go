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

package models

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Quarter represents a calendar quarter
type Quarter struct {
	Year    int
	Quarter int // 1-4
}

// ParseQuarter parses a quarter string in format "Q3-2024" or "Q1-2025"
func ParseQuarter(s string) (Quarter, error) {
	// Pattern: Q[1-4]-YYYY
	pattern := regexp.MustCompile(`^[Qq]([1-4])-(\d{4})$`)
	matches := pattern.FindStringSubmatch(s)
	if matches == nil {
		return Quarter{}, fmt.Errorf("invalid quarter format %q, expected Q1-2024, Q2-2024, etc.", s)
	}

	q, _ := strconv.Atoi(matches[1])
	year, _ := strconv.Atoi(matches[2])

	return Quarter{Year: year, Quarter: q}, nil
}

// String returns the string representation of the quarter
func (q Quarter) String() string {
	return fmt.Sprintf("Q%d-%d", q.Quarter, q.Year)
}

// StartDate returns the first day of the quarter
func (q Quarter) StartDate() time.Time {
	month := time.Month((q.Quarter-1)*3 + 1)
	return time.Date(q.Year, month, 1, 0, 0, 0, 0, time.UTC)
}

// EndDate returns the last day of the quarter (23:59:59.999999999)
func (q Quarter) EndDate() time.Time {
	// End month is the last month of the quarter
	endMonth := time.Month(q.Quarter * 3)
	// Get the last day by going to the first of next month and subtracting a day
	firstOfNextMonth := time.Date(q.Year, endMonth+1, 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstOfNextMonth.Add(-time.Nanosecond)
	return lastDay
}

// Contains checks if a time falls within this quarter
func (q Quarter) Contains(t time.Time) bool {
	start := q.StartDate()
	end := q.EndDate()
	return (t.Equal(start) || t.After(start)) && (t.Equal(end) || t.Before(end))
}

// QuarterMonths returns the months covered by each quarter
func QuarterMonths(q int) []time.Month {
	switch q {
	case 1:
		return []time.Month{time.January, time.February, time.March}
	case 2:
		return []time.Month{time.April, time.May, time.June}
	case 3:
		return []time.Month{time.July, time.August, time.September}
	case 4:
		return []time.Month{time.October, time.November, time.December}
	default:
		return nil
	}
}

// CurrentQuarter returns the current quarter
func CurrentQuarter() Quarter {
	now := time.Now()
	q := (int(now.Month())-1)/3 + 1
	return Quarter{Year: now.Year(), Quarter: q}
}

// PreviousQuarter returns the previous quarter
func (q Quarter) PreviousQuarter() Quarter {
	if q.Quarter == 1 {
		return Quarter{Year: q.Year - 1, Quarter: 4}
	}
	return Quarter{Year: q.Year, Quarter: q.Quarter - 1}
}

// NextQuarter returns the next quarter
func (q Quarter) NextQuarter() Quarter {
	if q.Quarter == 4 {
		return Quarter{Year: q.Year + 1, Quarter: 1}
	}
	return Quarter{Year: q.Year, Quarter: q.Quarter + 1}
}
