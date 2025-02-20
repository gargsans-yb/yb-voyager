/*
Copyright (c) YugabyteDB, Inc.

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
package stats

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/gosuri/uilive"
	"github.com/samber/lo"
	"github.com/yugabyte/yb-voyager/yb-voyager/src/tgtdb"
)

type StreamImportStatsReporter struct {
	sync.Mutex
	migrationUUID       uuid.UUID
	totalEventsImported int64
	CurrImportedEvents  int64
	startTime           time.Time
	eventsSlidingWindow [61]int64 // stores events per 10 secs for last 10 mins
	remainingEvents     int64
	estimatedTimeToCatchUp time.Duration
}

func NewStreamImportStatsReporter() *StreamImportStatsReporter {
	return &StreamImportStatsReporter{}
}

func (s *StreamImportStatsReporter) Init(tdb tgtdb.TargetDB, migrationUUID uuid.UUID) error {
	s.migrationUUID = migrationUUID
	numInserts, numUpdates, numDeletes, err := tdb.GetTotalNumOfEventsImportedByType(migrationUUID)
	s.totalEventsImported = numInserts + numUpdates + numDeletes
	if err != nil {
		return fmt.Errorf("failed to fetch import stats meta info from target : %w", err)
	}
	s.startTime = time.Now()
	return nil
}

func (s *StreamImportStatsReporter) ReportStats() {
	displayTicker := time.NewTicker(10 * time.Second)
	defer displayTicker.Stop()
	table := uilive.New()
	headerRow := table.Newline()
	seperator1 := table.Newline()
	seperator2 := table.Newline()
	seperator3 := table.Newline()
	row1 := table.Newline()
	row2 := table.Newline()
	row3 := table.Newline()
	row4 := table.Newline()
	row5 := table.Newline()
	row6 := table.Newline()
	timerRow := table.Newline()

	table.Start()

	for range displayTicker.C {
		elapsedTime := math.Round(time.Since(s.startTime).Minutes()*100) / 100
		s.slideWindow()
		fmt.Fprint(seperator1, color.GreenString("| %-30s | %30s |\n", "-----------------------------", "-----------------------------"))
		fmt.Fprint(headerRow, color.GreenString("| %-30s | %30s |\n", "Metric", "Value"))
		fmt.Fprint(seperator2, color.GreenString("| %-30s | %30s |\n", "-----------------------------", "-----------------------------"))
		fmt.Fprint(row1, color.GreenString("| %-30s | %30s |\n", "Total Imported events", strconv.FormatInt(s.totalEventsImported, 10)))
		fmt.Fprint(row2, color.GreenString("| %-30s | %30s |\n", "Events Imported in this Run", strconv.FormatInt(s.CurrImportedEvents, 10)))
		var averageRateLast3Mins, averageRateLast10Mins int64
		if elapsedTime < 3 {
			averageRateLast3Mins = s.getIngestionRateForLastNMinutes(int64(elapsedTime) + 1)
		} else {
			averageRateLast3Mins = s.getIngestionRateForLastNMinutes(3)
		}
		if elapsedTime < 10 {
			averageRateLast10Mins = s.getIngestionRateForLastNMinutes(int64(elapsedTime) + 1)
		} else {
			averageRateLast10Mins = s.getIngestionRateForLastNMinutes(10)
		}
		fmt.Fprint(row3, color.GreenString("| %-30s | %30s |\n", "Ingestion Rate (last 3 mins)", fmt.Sprintf("%d events/sec", averageRateLast3Mins/60)))
		fmt.Fprint(row4, color.GreenString("| %-30s | %30s |\n", "Ingestion Rate (last 10 mins)", fmt.Sprintf("%d events/sec", averageRateLast10Mins/60)))
		fmt.Fprint(timerRow, color.GreenString("| %-30s | %30s |\n", "Time taken in this Run", fmt.Sprintf("%.2f mins", elapsedTime)))
		fmt.Fprint(row5, color.GreenString("| %-30s | %30s |\n", "Remaining Events", strconv.FormatInt(s.remainingEvents, 10)))
		fmt.Fprint(row6, color.GreenString("| %-30s | %30s |\n", "Estimated Time to catch up", s.estimatedTimeToCatchUp.String()))
		fmt.Fprint(seperator3, color.GreenString("| %-30s | %30s |\n", "-----------------------------", "-----------------------------"))
		table.Flush()
	}
}

func (s *StreamImportStatsReporter) slideWindow() {
	s.Mutex.Lock()
	for i := len(s.eventsSlidingWindow) - 1; i > 0; i-- {
		s.eventsSlidingWindow[i] = s.eventsSlidingWindow[i-1]
	}
	s.eventsSlidingWindow[0] = 0
	s.Mutex.Unlock()
}

func (s *StreamImportStatsReporter) BatchImported(numInserts, numUpdates, numDeletes int64) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	total := numInserts + numUpdates + numDeletes
	s.CurrImportedEvents += total
	s.totalEventsImported += total
	s.eventsSlidingWindow[0] += total
}

func (s *StreamImportStatsReporter) getIngestionRateForLastNMinutes(n int64) int64 {
	windowSize := 6*n + 1 //6*n as sliding window every 10 secs
	return lo.Sum(s.eventsSlidingWindow[1:windowSize]) / n
}

func (s *StreamImportStatsReporter) UpdateRemainingEvents(totalExportedEvents int64) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.remainingEvents = totalExportedEvents - s.totalEventsImported
	lastMinIngestionRate := s.getIngestionRateForLastNMinutes(1)
	if lastMinIngestionRate > 0 {
		s.estimatedTimeToCatchUp = time.Duration(s.remainingEvents/lastMinIngestionRate) * time.Minute
	}
}