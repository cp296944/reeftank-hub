// pi-bridge — a 24/7 K7 Pro controller for a Raspberry Pi that bridges the
// lamp's Wi-Fi AP (wlan0) to the home LAN (eth0), with online self-update.
//
// Sibling module to pc-bridge; it cannot import pc-bridge/internal/*, so the
// protocol layer is vendored (internal/k7tcp, kept in sync by CI).
//
// Zero third-party dependencies on purpose: a single static linux/arm64 binary
// keeps OTA updates trivial and reliable.
module github.com/cp296944/reeftank-hub

go 1.23
