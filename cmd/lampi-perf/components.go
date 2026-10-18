package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/normalize"
)

// Independent passes show parse-only, parse+events encoding, and complete
// streaming output costs. They overlap in work; their times must not be added
// or subtracted as an exact decomposition of a concurrent service workload.
func streamWork(data string, paths []string, r *result) error {
	r.Scope = "independent streaming component passes; repeated parse; excludes agent and search"
	for i, path := range paths {
		uid := fmt.Sprintf("synthetic-%04d", i)
		count := 0
		produce := func(sink normalize.EventSink) error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			return (normalize.Claude{Now: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), NativeID: uid}).Stream(context.Background(), f, sink)
		}
		for _, task := range []struct {
			name string
			fn   func() error
		}{
			{"parse", func() error { return produce(func(normalize.Event) error { count++; return nil }) }},
			{"parse-events", func() error {
				f, err := os.Create(filepath.Join(data, uid+".events"))
				if err != nil {
					return err
				}
				defer f.Close()
				if err := normalize.WriteEventsStream(f, produce); err != nil {
					return err
				}
				return f.Close()
			}},
			{"parse-stage-publish", func() error {
				st, err := normalize.Stage(context.Background(), data, uid, produce)
				if err != nil {
					return err
				}
				defer st.Close()
				return st.Publish(filepath.Join(data, "normalized"), filepath.Join(data, "parquet"))
			}},
		} {
			s, err := measure(fmt.Sprintf("session-%d/%s", i, task.name), task.fn)
			s.Events = count
			r.Stages = append(r.Stages, s)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
