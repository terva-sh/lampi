package api

import (
	"context"
	"errors"
	"fmt"

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
	sample := catalog.StorageSample{At: at, Measures: make(map[string]catalog.StorageUse, len(use)+4)}
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
	referenced, unique, err := s.Catalog.ArtifactBytes(ctx)
	if err != nil {
		return catalog.StorageSample{}, err
	}
	sample.Measures[catalog.MeasureReferenced] = referenced
	sample.Measures[catalog.MeasureUnique] = unique
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
