// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package client

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dsh-cli/internal/protocol"

	"github.com/coder/websocket"
)

// DownlinkFrame is one decoded downlink frame from either stream.
type DownlinkFrame struct {
	RpcId   string
	Kind    string // the payload's "type" discriminator
	Payload []byte
	Raw     []byte
}

// Stream is the downlink of one DSH web server. The legacy build speaks
// it as the dual events.mux + events.host sockets; the cookie-gated
// build as the single multiplexed remote.mux socket. Both sockets are
// downlink-only; upstream traffic stays on HTTP. Stream delivers decoded
// frames on Frames() and connectivity on Status(). On any socket death
// the downlink re-dials and Status emits false, so callers re-baseline
// (refetch session.list + history tail) and resume.
//
// Drops pulses when the consumer was too slow for more than dropWatch
// consecutive frames (a GC pause or a blocked pump): the loss is repaired
// by a re-baseline, not by replay.
type Stream struct {
	base     string
	wsBase   string
	origin   string // the truthful Origin of the ws dial (scheme://authority)
	conn     *Conn  // shared per-base state (dialect, cookie, host facts)
	http     *http.Client
	mux      *muxStream // live remote.mux epoch (new dialect; nil between)
	followMu sync.Mutex
	follows  map[string]FollowAddr

	frames  chan DownlinkFrame
	status  chan bool
	drops   chan struct{}
	dropped atomic.Int64
	consec  atomic.Int64
}

// dropWatch bounds consecutive drops before a stale-state pulse. Two
// seconds of consumer stall already drops; eight in a row means the state
// behind the queue is likely stale, so the app re-baselines.
const dropWatch = 8

// NewStream builds a stream for the given base URL (http or https).
func NewStream(base string) *Stream {
	b := trimBase(base)
	wsBase := strings.NewReplacer("https://", "wss://", "http://", "ws://").Replace(b)
	scheme := "https"
	if strings.HasPrefix(b, "http://") {
		scheme = "http"
	}
	host := strings.TrimPrefix(wsBase, "wss://")
	host = strings.TrimPrefix(host, "ws://")
	return &Stream{
		base:    b,
		wsBase:  wsBase,
		origin:  scheme + "://" + host,
		conn:    ConnFor(b),
		http:    &http.Client{Timeout: 5 * time.Second},
		follows: map[string]FollowAddr{},
		frames:  make(chan DownlinkFrame, 256),
		status:  make(chan bool, 8),
		drops:   make(chan struct{}, 1),
	}
}

// Frames yields decoded frames from both streams.
func (s *Stream) Frames() <-chan DownlinkFrame { return s.frames }

// Status yields true when both sockets are open, false after a drop.
func (s *Stream) Status() <-chan bool { return s.status }

// Drops pulses when consecutive frame drops crossed dropWatch (the consumer
// was stalled and state may be stale).
func (s *Stream) Drops() <-chan struct{} { return s.drops }

// Start opens the downlinks and pumps frames until ctx is cancelled.
func (s *Stream) Start(ctx context.Context) {
	go s.loop(ctx)
}

// loop settles the build generation (one unauthenticated probe; a failed
// probe just re-enters the ladder) and hands off to the dialect's
// reconnect loop.
func (s *Stream) loop(ctx context.Context) {
	var backoff time.Duration
	for {
		d, err := s.dialect(ctx)
		if err == nil {
			if d == DialectNew {
				s.muxLoop(ctx)
			} else {
				s.legacyLoop(ctx)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		backoff = nextBackoff(backoff)
		s.emitStatus(false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (s *Stream) legacyLoop(ctx context.Context) {
	var backoff time.Duration
	for {
		mux, errMux := s.dial(ctx, s.wsBase+protocol.StreamMux)
		host, errHost := s.dial(ctx, s.wsBase+protocol.StreamHost)
		if mux == nil && host == nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			backoff = nextBackoff(backoff)
			s.emitStatus(false)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		backoff = 0
		// At least one socket is up: a half-dead epoch is acceptable
		// (frames and the status pulse cover the loss), so the dial
		// errors are intentionally dropped here.
		_ = errMux
		_ = errHost

		dead := make(chan struct{}, 2)
		var once sync.Once
		die := func() { once.Do(func() { close(dead) }) }
		if mux != nil {
			go s.reader(ctx, mux, die)
		}
		if host != nil {
			go s.reader(ctx, host, die)
		}
		s.emitStatus(true)

		// Ping both sockets every 30s; the host answers protocol pings,
		// which also resets our read deadline.
		pingStop := make(chan struct{})
		var pingWG sync.WaitGroup
		for _, c := range []*websocket.Conn{mux, host} {
			if c == nil {
				continue
			}
			pingWG.Add(1)
			go func(c *websocket.Conn) {
				defer pingWG.Done()
				t := time.NewTicker(30 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-pingStop:
						return
					case <-t.C:
						if err := c.Ping(ctx); err != nil {
							die()
							return
						}
					}
				}
			}(c)
		}

		select {
		case <-ctx.Done():
		case <-dead:
			s.emitStatus(false)
		}
		close(pingStop)
		pingWG.Wait()
		for _, c := range []*websocket.Conn{mux, host} {
			if c != nil {
				c.Close(websocket.StatusNormalClosure, "")
			}
		}
	}
}

// reader pumps one downlink until it dies. The per-read deadline doubles as
// the idle watchdog: server pongs reset it, so a dead peer surfaces within
// the deadline.
func (s *Stream) reader(ctx context.Context, conn *websocket.Conn, die func()) {
	defer die()
	for {
		readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		s.dispatch(data)
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Stream) dial(ctx context.Context, url string) (*websocket.Conn, error) {
	// Bound the dial: a half-open TCP (no RST from the peer) must not stall
	// the backoff ladder for the full OS connect timeout.
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scheme, host := "https", strings.TrimPrefix(url, "https://")
	if strings.HasPrefix(url, "ws://") {
		scheme, host = "http", strings.TrimPrefix(url, "ws://")
	} else {
		host = strings.TrimPrefix(host, "wss://")
	}
	// The Origin header mirrors the scheme the socket actually rides on:
	// a ws:// dial presents as http://, wss:// as https://. A host that
	// validates the Origin sees a truthful source, not an inflated one.
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{scheme + "://" + host}},
	}
	conn, _, err := websocket.Dial(dctx, url, opts)
	if err != nil {
		return nil, err
	}
	// The library's 32KiB default read limit would kill the stream on any
	// oversized frame (a large approval arg dump or projection); 4MiB is
	// far above a legitimate frame. This cap is independent of the unary
	// response limit (16MiB, rpc.go): the two channels carry different
	// shapes and keep separate caps.
	conn.SetReadLimit(4 << 20)
	return conn, nil
}

func (s *Stream) dispatch(data []byte) {
	frame, ok := protocol.DecodeDownlink(data)
	if !ok {
		return
	}
	out := DownlinkFrame{RpcId: frame.RpcId, Kind: frame.Kind, Payload: frame.Payload, Raw: data}
	select {
	case s.frames <- out:
		s.consec.Store(0)
	case <-time.After(2 * time.Second):
		// Slow consumer; drop. Reconnect re-baselines and authoritative
		// snapshots repair the loss.
		s.dropped.Add(1)
		if s.consec.Add(1) >= dropWatch {
			s.consec.Store(0)
			select {
			case s.drops <- struct{}{}:
			default:
			}
		}
	}
}

func (s *Stream) emitStatus(up bool) {
	select {
	case s.status <- up:
	case <-time.After(200 * time.Millisecond):
	}
}

// nextBackoff is the reconnect delay ladder: 500ms, 1s, 2s, …, 10s cap.
// The first rung is returned by the 0 → 500ms step; every failure doubles
// the previous delay until the cap (a flapping peer must not be re-dialed
// at a fixed 2/s forever), and reset to 0 on a successful epoch.
func nextBackoff(prev time.Duration) time.Duration {
	if prev == 0 {
		return 500 * time.Millisecond
	}
	n := prev * 2
	if n > 10*time.Second {
		n = 10 * time.Second
	}
	return n
}
