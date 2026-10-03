// jebao-probe performs discovery or read-only monitoring of known MACs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cp296944/reeftank-hub/internal/jebao"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	target := ""
	if len(os.Args) > 1 {
		if os.Args[1] == "--status" {
			m, err := jebao.Open("")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			m.Refresh(ctx, true)
			_ = json.NewEncoder(os.Stdout).Encode(m.Snapshot())
			return
		}
		target = os.Args[1]
	}
	ids, err := jebao.Discover(ctx, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(ids)
}
