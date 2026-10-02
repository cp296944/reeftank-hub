package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Probe the actual listening server, not just an always-true startup flag.
func checkLocalHTTP(ctx context.Context, listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	base := "http://" + net.JoinHostPort(host, port)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/healthz", "/api/hub/storage/status", "/", "/hub-assets/app.js", "/hub-assets/app.css"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("local health %s: %w", path, err)
		}
		n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil || n == 0 {
			return fmt.Errorf("local health %s: HTTP %d, bytes=%d, error=%v", path, resp.StatusCode, n, readErr)
		}
	}
	return nil
}
