// pi-bridge — a 24/7 K7 Pro controller for a Raspberry Pi that bridges the
// lamp's Wi-Fi AP (wlan0) to the home LAN (eth0), with online self-update.
//
// Sibling module to pc-bridge; it cannot import pc-bridge/internal/*, so the
// protocol layer is vendored (internal/k7tcp, kept in sync by CI).
//
// The release remains a single static linux/arm64 binary. Pure-Go SQLite and
// WebSocket dependencies provide the Phase 2/3 local data and HA live layers.
module github.com/cp296944/reeftank-hub

go 1.23.0

require (
	github.com/coder/websocket v1.8.15
	modernc.org/sqlite v1.36.3
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20230315142452-642cacee5cc0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	modernc.org/libc v1.61.13 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.8.2 // indirect
)
