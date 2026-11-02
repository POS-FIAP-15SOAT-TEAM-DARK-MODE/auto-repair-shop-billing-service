package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"
)

const (
	tryAdvisoryLockQuery = `SELECT pg_try_advisory_lock($1)`
	advisoryUnlockQuery  = `SELECT pg_advisory_unlock($1)`
	unlockTimeout        = 5 * time.Second
)

// AdvisoryLock is a cross-replica lock held in the database for as long as a session keeps it. It is
// not a transaction: whoever holds it can call a remote service without holding a transaction open.
// If the process dies, its connection closes and Postgres releases the lock.
type AdvisoryLock struct {
	db  *sql.DB
	key int64
}

// NewAdvisoryLock returns the lock identified by key. Every replica must use the same key.
func NewAdvisoryLock(database *sql.DB, key int64) *AdvisoryLock {
	return &AdvisoryLock{db: database, key: key}
}

// TryLock takes the lock without waiting. ok is false when another session holds it. When ok is true
// the caller must call release, which gives the lock back even if ctx is already cancelled.
func (l *AdvisoryLock) TryLock(ctx context.Context) (release func(), ok bool, err error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRowContext(ctx, tryAdvisoryLockQuery, l.key).Scan(&ok); err != nil || !ok {
		_ = conn.Close()
		return nil, false, err
	}
	return func() { l.release(conn) }, true, nil
}

// release unlocks on the same connection that took the lock, since the lock belongs to the session.
// When the unlock fails the connection is discarded instead of going back to the pool, which ends the
// session and with it the lock.
func (l *AdvisoryLock) release(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()

	var unlocked bool
	if err := conn.QueryRowContext(ctx, advisoryUnlockQuery, l.key).Scan(&unlocked); err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}
