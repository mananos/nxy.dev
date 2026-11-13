package sqlite

import (
	"context"
	"database/sql"
)

// LatestSchemaVersion is the newest migration this binary knows.
func LatestSchemaVersion() (int, error) {
	migs, err := embeddedMigrations()
	if err != nil || len(migs) == 0 {
		return 0, err
	}
	return migs[len(migs)-1].version, nil
}

// Watch polls the database for changes on one fixed connection: PRAGMA data_version is per
// connection, so the pool (which may hand out a different one each time) would not do.
type Watch struct{ conn *sql.Conn }

// Watch pins a connection for polling; Close it when done.
func (s *Store) Watch(ctx context.Context) (*Watch, error) {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &Watch{conn: c}, nil
}

// Poll returns data_version (it changes when another connection commits) and the schema version
// recorded in the database.
func (w *Watch) Poll(ctx context.Context) (dataVersion int64, schema int, err error) {
	if err = w.conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&dataVersion); err != nil {
		return 0, 0, err
	}
	schema, err = currentVersion(ctx, w.conn)
	return dataVersion, schema, err
}

// Close releases the pinned connection.
func (w *Watch) Close() error { return w.conn.Close() }
