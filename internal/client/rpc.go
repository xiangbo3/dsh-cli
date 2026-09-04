// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package client is the DSH transport layer: unary HTTP RPC, the answer
// channel (POST /api/respond), and the dual WebSocket downlink.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"dsh-cli/internal/protocol"
	"dsh-cli/internal/textutil"
)

// maxBody bounds any unary response: the host answers with JSON of at most
// a few MB (the largest legitimate payload is a 200-event history page),
// so a runaway body is a faulty host, not data.
const maxBody = 16 << 20

// Client is a unary RPC caller for one DSH web server.
type Client struct {
	base string
	http *http.Client
	// token is the DSH_TOKEN bearer credential ("" = none; the host
	// decides whether a connection without one is acceptable).
	token string
	// insecure marks the DSH_INSECURE boot: the transport skips the
	// server certificate check (DSH_CA extra-trust does not count).
	insecure bool
}

// New builds a client for base (e.g. "http://127.0.0.1:3080").
//
// Transport environment:
//
//	DSH_TOKEN     send Authorization: Bearer <token> on every request
//	DSH_CA        extra CA bundle (PEM) for https verification (self-signed
//	              bridge certificates)
//	DSH_INSECURE  =1/=true: https without server certificate verification
func New(base string) *Client {
	var transport http.RoundTripper = http.DefaultTransport
	insecure := false
	if ca := os.Getenv("DSH_CA"); ca != "" {
		if pem, err := os.ReadFile(ca); err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(pem)
			if dt, ok := http.DefaultTransport.(*http.Transport); ok {
				t := dt.Clone()
				t.TLSClientConfig = &tls.Config{RootCAs: pool}
				transport = t
			}
		}
	} else if envBool("DSH_INSECURE") {
		if dt, ok := http.DefaultTransport.(*http.Transport); ok {
			t := dt.Clone()
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 — explicit env opt-in
			transport = t
			insecure = true
		}
	}
	return &Client{
		base:     strings.TrimRight(base, "/"),
		token:    os.Getenv("DSH_TOKEN"),
		insecure: insecure,
		http: &http.Client{
			Timeout:   60 * time.Second,
			Transport: transport,
			// A redirected RPC POST replays its JSON body (the prompt)
			// to the redirect target: treat any redirect as the final
			// answer instead of following it.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 1 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// Insecure reports whether DSH_INSECURE is active for this client (the
// transport skips the server certificate check).
func (c *Client) Insecure() bool { return c.insecure }

// IsDown reports whether err is a transport-level HTTP failure —
// connection refused, DNS miss, dead route: the kind of error that means
// no DSH server is listening at the configured address. A timeout is NOT
// down (the server exists but is slow), and an answered request (any HTTP
// status) is not down either: call() reports those without a *url.Error.
func IsDown(err error) bool {
	var ue *url.Error
	return errors.As(err, &ue) && !ue.Timeout()
}

// BaseURL returns the configured server base.
func (c *Client) BaseURL() string { return c.base }

// call performs one unary RPC and decodes the result value into target.
func (c *Client) call(ctx context.Context, method string, payload any, target any) error {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}
	body, err := json.Marshal(protocol.Envelope{
		Type:    protocol.TypeClientRequest,
		RpcId:   protocol.NewRPCId(),
		Method:  method,
		Payload: rawPayload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// The bridge refuses non-JSON content types with 415.
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnsupportedMediaType {
			return fmt.Errorf("%s: server refused content type (415)", method)
		}
		return fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, snippet(string(raw), 200))
	}
	var env protocol.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s: bad envelope: %v", method, err)
	}
	if env.Result == nil {
		return fmt.Errorf("%s: missing result", method)
	}
	if !env.Result.Ok {
		if env.Result.Error != nil {
			return env.Result.Error
		}
		return fmt.Errorf("%s: failed without error details", method)
	}
	if target != nil && len(env.Result.Value) > 0 {
		if err := json.Unmarshal(env.Result.Value, target); err != nil {
			return fmt.Errorf("%s: decode value: %w", method, err)
		}
	}
	return nil
}

// Call is the untyped escape hatch for methods without a facade.
func (c *Client) Call(ctx context.Context, method string, payload any, target any) error {
	return c.call(ctx, method, payload, target)
}

// Respond posts a client-response for one answerable server-request frame
// (approval or question), echoing its rpcId.
func (c *Client) Respond(ctx context.Context, rpcId string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body, err := json.Marshal(protocol.Envelope{
		Type:   protocol.TypeClientResponse,
		RpcId:  rpcId,
		Result: &protocol.Result{Ok: true, Value: raw},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+protocol.PathRespond, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("respond: HTTP %d: %s", resp.StatusCode, snippet(string(rawBody), 200))
	}
	// The carrier acks a rejected payload with HTTP 200 +
	// {accepted:false, reason}: without this check a bad answer closes
	// the modal and vanishes (the pending question never settles on the
	// host, and the user sees no toast).
	var receipt struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(rawBody, &receipt); err == nil && !receipt.Accepted {
		if receipt.Reason != "" {
			return fmt.Errorf("respond: %s", receipt.Reason)
		}
		return fmt.Errorf("respond: not accepted")
	}
	return nil
}

// auth stamps the bearer credential, if configured.
func (c *Client) auth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// envBool reports an env var set to 1/true/yes (case-insensitive).
func envBool(k string) bool {
	switch strings.ToLower(os.Getenv(k)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func snippet(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return textutil.Truncate(s, n, "…")
}
