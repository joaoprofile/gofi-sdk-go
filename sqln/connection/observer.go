package connection

import (
	"log/slog"
)

// ConnectionObserver implementa o padrão observer para fechar a conexão de forma segura.
// Registre-o no observer global após criar a conexão:
//
//	observer.Attach(connection.NewObserver("main", conn))
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
	slog.Info("closing database connection",
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
