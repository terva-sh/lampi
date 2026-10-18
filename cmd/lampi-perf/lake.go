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
)

// The private stdio channel waits for normalization between stages without
// exposing a diagnostics endpoint or including lake allocations in the agent.
func serveLake(data string, input io.Reader, output io.Writer) error {
	dir := filepath.Join(data, "lake")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	lake, err := api.Open(dir)
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
		if err := enc.Encode("done"); err != nil {
			return err
		}
	}
}

func startLake(data string, separate bool) (url string, wait func() error, closeLake func() error, err error) {
	if !separate {
		lake, err := api.Open(filepath.Join(data, "lake"))
		if err != nil {
			return "", nil, nil, err
		}
		srv := httptest.NewServer(lake.Handler())
		return srv.URL, func() error { return lake.WaitNormalized(context.Background()) }, func() error {
			srv.Close()
			return lake.Close()
		}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd := exec.CommandContext(ctx, exe, "--lake-worker", "--out", data)
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
		var reply string
		if err := dec.Decode(&reply); err != nil {
			return fmt.Errorf("wait for isolated lake: %w", err)
		}
		if reply != "done" {
			return fmt.Errorf("unexpected lake reply %q", reply)
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
