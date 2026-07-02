package surrealdb

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/contrib/rews"
	"github.com/surrealdb/surrealdb.go/pkg/connection"
	"github.com/surrealdb/surrealdb.go/pkg/connection/gws"
	"github.com/surrealdb/surrealdb.go/pkg/logger"
)

// defaultReconnectInterval is how often the auto-reconnecting WebSocket
// connection checks liveness and attempts to reconnect when disconnected.
const defaultReconnectInterval = 5 * time.Second

// dialConn establishes the SurrealDB connection for this dialector.
//
// For WebSocket DSNs it uses the SDK's reliable-websocket (rews) wrapper, which
// transparently reconnects on connection loss and replays the recorded SignIn
// token and USE namespace/database, so callers keep working across drops. Set
// Dialector.ReconnectInterval to a negative value to opt out (plain
// non-reconnecting connection), or to a positive duration to tune the interval.
//
// HTTP/embedded DSNs fall back to the standard connection since rews only wraps
// WebSocket connections.
func (dialector *Dialector) dialConn(ctx context.Context) (*surrealdb.DB, error) {
	interval := dialector.ReconnectInterval
	if interval == 0 {
		interval = defaultReconnectInterval
	}

	u, err := url.ParseRequestURI(dialector.DSN)
	if err != nil {
		return nil, err
	}

	// rews only applies to WebSocket connections and only when reconnection is
	// enabled; otherwise use the plain connection.
	if interval < 0 || (u.Scheme != "ws" && u.Scheme != "wss") {
		return surrealdb.FromEndpointURLString(ctx, dialector.DSN)
	}

	conf := connection.NewConfig(u)
	// Silence the SDK's default stdout logger; reconnection is surfaced through
	// query errors instead of noisy logs.
	conf.Logger = logger.New(slog.NewTextHandler(io.Discard, nil))

	rewsConn := rews.New(
		func(context.Context) (*gws.Connection, error) { return gws.New(conf), nil },
		interval,
		conf.Unmarshaler,
		conf.Logger,
	)
	if err := rewsConn.Connect(ctx); err != nil {
		return nil, err
	}
	return surrealdb.FromConnection(ctx, rewsConn)
}
