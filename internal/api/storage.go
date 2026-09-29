package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/storage"
)

// SampleStorage measures the lake directory, its filesystem and the
// bytes the catalog references, and records the sample. It walks every
// file under the lake directory, so it belongs on a timer, never on a
// request. A platform that cannot report filesystem capacity records
// the rest.
func (s *Server) SampleStorage(ctx context.Context) (catalog.StorageSample, error) {
	if s.dataDir == "" {
		return catalog.StorageSample{}, fmt.Errorf("api: storage sample: no lake directory")
	}
	at := s.now()
	use, err := storage.Measure(ctx, s.dataDir)
	if err != nil {
		return catalog.StorageSample{}, err
	}
	sample := catalog.StorageSample{At: at, Measures: make(map[string]catalog.StorageUse, len(use)+6)}
	for c, u := range use {
		sample.Measures[c] = catalog.StorageUse{Bytes: u.Bytes, Files: u.Files}
	}
	fs, err := storage.Capacity(s.dataDir)
	switch {
	case err == nil:
		sample.Measures[catalog.MeasureFSTotal] = catalog.StorageUse{Bytes: clampInt64(fs.Total)}
		sample.Measures[catalog.MeasureFSFree] = catalog.StorageUse{Bytes: clampInt64(fs.Free)}
	case !errors.Is(err, storage.ErrUnsupported):
		return catalog.StorageSample{}, err
	}
	art, err := s.Catalog.ArtifactBytes(ctx)
	if err != nil {
		return catalog.StorageSample{}, err
	}
	sample.Measures[catalog.MeasureReferenced] = art.Referenced
	sample.Measures[catalog.MeasureUnique] = art.Unique
	sample.Measures[catalog.MeasureCurrent] = art.Current
	sample.Measures[catalog.MeasureCurrentUnique] = art.CurrentUnique
	if err := s.Catalog.RecordStorage(ctx, sample); err != nil {
		return catalog.StorageSample{}, err
	}
	return sample, nil
}

func clampInt64(n uint64) int64 {
	if n > 1<<63-1 {
		return 1<<63 - 1
	}
	return int64(n)
}

// Contacts returns the time of each device's last authenticated
// request, by device id. It is kept in memory and starts from each
// device's newest stored report, so a restart falls back to the last
// report rather than to nothing. A lake with no device tokens records
// none.
func (s *Server) Contacts() map[string]time.Time {
	out := map[string]time.Time{}
	s.contacts.Range(func(k, v any) bool {
		out[k.(string)] = v.(time.Time)
		return true
	})
	return out
}

// loadContacts seeds the contacts from the stored reports.
func (s *Server) loadContacts(ctx context.Context) error {
	reports, err := s.Catalog.DeviceReports(ctx)
	if err != nil {
		return err
	}
	for _, r := range reports {
		s.noteContact(r.DeviceID, r.Received)
	}
	return nil
}

// noteContact records at as the device's last contact unless a later
// one is already there: two requests can take their times in one order
// and arrive here in the other.
func (s *Server) noteContact(id string, at time.Time) {
	for {
		prev, loaded := s.contacts.LoadOrStore(id, at)
		if !loaded || !at.After(prev.(time.Time)) {
			return
		}
		if s.contacts.CompareAndSwap(id, prev, at) {
			return
		}
	}
}
