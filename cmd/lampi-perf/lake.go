package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/recall"
)

// The private stdio channel waits for normalization between stages without
// exposing a diagnostics endpoint or including lake allocations in the agent.
func serveLake(data string, input io.Reader, output io.Writer, options api.NormalizeOptions) error {
	dir := filepath.Join(data, "lake")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	lake, err := api.OpenWithOptions(dir, options)
	if err != nil {
		return err
	}
	defer lake.Close()
	srv := httptest.NewServer(lake.Handler())
	defer srv.Close()
	enc, dec := json.NewEncoder(output), json.NewDecoder(input)
	if err := enc.Encode(srv.URL); err != nil {
		return err
	}
	for {
		var command string
		if err := dec.Decode(&command); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if command != "wait" {
			return fmt.Errorf("unknown lake command %q", command)
		}
		if err := lake.WaitNormalized(context.Background()); err != nil {
			return err
		}
		if err := enc.Encode(lake.NormalizeWork()); err != nil {
			return err
		}
	}
}

func startLake(data string, separate bool, options api.NormalizeOptions, observation *api.NormalizeWorkSnapshot, indexTarget *func() error) (url string, wait func() error, closeLake func() error, err error) {
	if !separate {
		lake, err := api.OpenWithOptions(filepath.Join(data, "lake"), options)
		if err != nil {
			return "", nil, nil, err
		}
		var index *recall.Index
		if indexTarget != nil {
			index, err = recall.OpenIndex(filepath.Join(data, "lake", recall.IndexFile), recall.NewReader(lake.Catalog, lake.Normalized))
			if err != nil {
				lake.Close()
				return "", nil, nil, err
			}
			*indexTarget = func() error {
				if err := index.Pass(context.Background()); err != nil {
					return err
				}
				cov := index.Coverage()
				if cov.Behind != 0 || cov.Failed != 0 || cov.Ready != cov.Indexed {
					return fmt.Errorf("incomplete search: %+v", cov)
				}
				return nil
			}
		}
		srv := httptest.NewServer(lake.Handler())
		return srv.URL, func() error {
				err := lake.WaitNormalized(context.Background())
				*observation = lake.NormalizeWork()
				return err
			}, func() error {
				srv.Close()
				return lake.Close()
			}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd := exec.CommandContext(ctx, exe, "--lake-worker", "--out", data, "--normalize-workers", fmt.Sprint(options.Workers), "--normalize-input-budget-mib", fmt.Sprint(options.InputBudgetBytes>>20))
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return "", nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return "", nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		cancel()
		return "", nil, nil, err
	}
	enc, dec := json.NewEncoder(in), json.NewDecoder(out)
	if err := dec.Decode(&url); err != nil {
		in.Close()
		cancel()
		_ = cmd.Wait()
		return "", nil, nil, fmt.Errorf("start isolated lake: %w", err)
	}
	wait = func() error {
		if err := enc.Encode("wait"); err != nil {
			return err
		}
		if err := dec.Decode(observation); err != nil {
			return fmt.Errorf("wait for isolated lake: %w", err)
		}
		return nil
	}
	closeLake = func() error {
		_ = in.Close()
		defer cancel()
		return cmd.Wait()
	}
	return url, wait, closeLake, nil
}
