// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// bootfake is a full-protocol stand-in for the cookie-gated dsh web's
// boot sequence: the host is down first (bind delay), the web server
// then listens before its routes are claimed (the 404 boot window),
// the gate 401s with the marker and mints the launch-token cookie, the
// /api endpoints gate on the minted cookie, and /api/remote.mux upgrades
// to the multiplexed downlink (the $events ready frame included). The
// token line is printed late — past the launch's ready grace.
//
// The launch passes its own arguments (`web --no-open --host h --port
// p`), so the token rides DSH_BOOTFAKE_TOKEN and the timings are fixed
// constants (they only need to outlast the grace, not be tuned).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"dsh-cli/internal/protocol"

	"github.com/coder/websocket"
)

const (
	bindDelay = 1 * time.Second
	window    = 3 * time.Second
	late      = 1500 * time.Millisecond
	cookie    = "dsh-auth-fake=fake.sig"
)

func argValue(args []string, name, def string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return def
}

func main() {
	port := argValue(os.Args[1:], "--port", "")
	token := os.Getenv("DSH_BOOTFAKE_TOKEN")
	if port == "" || token == "" {
		fmt.Fprintln(os.Stderr, "bootfake: want --port <port> and DSH_BOOTFAKE_TOKEN")
		os.Exit(2)
	}

	time.Sleep(bindDelay)

	gated := make(chan struct{})
	go func() {
		time.Sleep(window)
		close(gated)
	}()
	go func() {
		<-gated
		time.Sleep(late)
		fmt.Printf("dsh web: http://127.0.0.1:%s/?token=%s\n", port, token)
	}()

	unary := func(w http.ResponseWriter, r *http.Request, value any) {
		if r.Header.Get("Cookie") != cookie {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "unauthorized")
			return
		}
		var env protocol.Envelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(value)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.Envelope{
			Type:   protocol.TypeServerResponse,
			RpcId:  env.RpcId,
			Result: &protocol.Result{Ok: true, Value: b},
		})
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gated:
		default:
			w.WriteHeader(http.StatusNotFound) // the boot window
			return
		}
		switch r.URL.Path {
		case "/":
			if r.URL.Query().Has("token") {
				if r.URL.Query().Get("token") != token {
					w.WriteHeader(http.StatusUnauthorized)
					io.WriteString(w, "stale token")
					return
				}
				w.Header().Set("Set-Cookie", cookie+"; Path=/; HttpOnly; SameSite=Strict")
				w.Header().Set("Location", "/")
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			if r.Header.Get("Cookie") == cookie {
				io.WriteString(w, "<html/>")
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "dsh web authentication required")
		case "/api/remote.mux":
			if r.Header.Get("Cookie") != cookie {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close(websocket.StatusNormalClosure, "")
			for {
				_, data, err := c.Read(r.Context())
				if err != nil {
					return
				}
				var msg struct {
					Type     string `json:"type"`
					StreamId string `json:"streamId"`
					Endpoint string `json:"endpoint"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.Type != "open" {
					continue
				}
				if msg.Endpoint == "$events" {
					b, _ := json.Marshal(map[string]any{
						"type":     "item",
						"streamId": msg.StreamId,
						"value": map[string]any{
							"type": "ready", "clientId": "cli-1",
							"host": map[string]any{"home": "/tmp/bootfake-home"},
						},
					})
					_ = c.Write(r.Context(), websocket.MessageText, b)
				}
			}
		case "/api/session/modelCatalog":
			unary(w, r, map[string]any{"default": map[string]any{"provider": "deepseek", "model": "chat"}})
		case "/api/session/canOpenWorkspacePath":
			unary(w, r, true)
		case "/api/session/list":
			unary(w, r, protocol.SessionListResponse{Items: []protocol.SessionSummary{{SessionId: "s1", UpdatedAt: 5}}})
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "not found")
		}
	})

	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: h}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "bootfake:", err)
		os.Exit(1)
	}
}
