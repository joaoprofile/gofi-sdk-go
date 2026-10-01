package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"time"
)

// generateLoad sends a steady mix of orders, including invalid ones, so the
// dashboards have data as soon as the service starts. LOADGEN=false (in .env
// or the shell) turns it off; it is read here, after Build loaded .env.
func generateLoad(ctx context.Context, baseURL string) {
	if os.Getenv("LOADGEN") == "false" {
		<-ctx.Done()
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	methods := []string{"pix", "card", "boleto"}

	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		amount := 5 + rand.Float64()*600 // #nosec G404 -- simulated load, not a secret
		if rand.IntN(100) < 4 {          // #nosec G404 -- simulated load, not a secret
			amount = -1 // rejected by validation
		}
		body := fmt.Sprintf(`{"amount":%.2f,"payment_method":%q}`, amount, methods[rand.IntN(len(methods))]) // #nosec G404 -- simulated load, not a secret

		resp, err := client.Post(baseURL+"/orders", "application/json", strings.NewReader(body))
		if err != nil {
			continue
		}
		var order struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&order)
		resp.Body.Close()

		if order.ID != "" {
			if resp, err := client.Get(baseURL + "/orders/" + order.ID); err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}
	}
}
