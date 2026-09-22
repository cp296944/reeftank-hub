// Package proxy exposes the lamp's raw 8266 binary protocol on the LAN side.
//
// It is a straight TCP pipe: a client on eth0 connects, proxy dials the lamp on
// wlan0, and bytes are copied both ways until either end closes. This lets the
// desktop pc-bridge — or any protocol-level tool — drive the lamp *through* the
// Pi without joining the lamp's Wi-Fi.
//
// The lamp accepts one connection at a time and has no locking, so the proxy
// serialises clients (one in-flight lamp connection) and, when the always-on
// scheduler is running, briefly yields to it via the shared gate.
package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"
)

type Proxy struct {
	Listen   string        // e.g. ":8266"
	LampAddr string        // e.g. "192.168.4.1:8266"
	Dial     time.Duration // lamp dial timeout
	Idle     time.Duration // drop a client after this much silence

	// Gate, if set, is acquired for the lifetime of each proxied connection so
	// the scheduler and the proxy don't talk to the lamp at the same time.
	Gate *sync.Mutex

	ln net.Listener
}

func (p *Proxy) Run(ctx context.Context) error {
	if p.Listen == "" {
		return nil // disabled
	}
	if p.Dial <= 0 {
		p.Dial = 4 * time.Second
	}
	if p.Idle <= 0 {
		p.Idle = 60 * time.Second
	}
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", p.Listen)
	if err != nil {
		return err
	}
	p.ln = ln
	slog.Info("proxy listening", "addr", p.Listen, "lamp", p.LampAddr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	// one client at a time
	sem := make(chan struct{}, 1)
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			slog.Warn("proxy accept", "err", err)
			continue
		}
		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				p.handle(ctx, c)
			}()
		default:
			slog.Debug("proxy busy, dropping client", "remote", c.RemoteAddr())
			_ = c.Close()
		}
	}
}

func (p *Proxy) handle(ctx context.Context, client net.Conn) {
	defer client.Close()

	if p.Gate != nil {
		p.Gate.Lock()
		defer p.Gate.Unlock()
	}

	d := net.Dialer{Timeout: p.Dial}
	lamp, err := d.DialContext(ctx, "tcp", p.LampAddr)
	if err != nil {
		slog.Warn("proxy: dial lamp", "err", err)
		return
	}
	defer lamp.Close()

	slog.Debug("proxy session", "client", client.RemoteAddr())
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 4096)
		for {
			_ = src.SetReadDeadline(time.Now().Add(p.Idle))
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		done <- struct{}{}
	}
	go pipe(lamp, client)
	go pipe(client, lamp)

	select {
	case <-done:
	case <-ctx.Done():
	}
	_ = client.SetDeadline(time.Now())
	_ = lamp.SetDeadline(time.Now())
}
