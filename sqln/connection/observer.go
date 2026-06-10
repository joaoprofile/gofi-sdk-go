package connection

import (
	"log/slog"
)

type ConnectionObserver struct {
	name string
	conn *Connection
}

func NewObserver(name string, conn *Connection) *ConnectionObserver {
	return &ConnectionObserver{name: name, conn: conn}
}

func (o *ConnectionObserver) Close() {
	db := o.conn.DB()

	stats := db.Stats()
	slog.Debug("closing database connection",
		slog.String("name", o.name),
		slog.Int("open", stats.OpenConnections),
		slog.Int("in_use", stats.InUse),
		slog.Int("idle", stats.Idle),
	)

	if err := db.Close(); err != nil {
		slog.Error("error closing database connection",
			slog.String("name", o.name),
			slog.Any("error", err),
		)
	}
}
