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
//
// The lock belongs to a session, so it needs a connection in session mode: a pooler in transaction mode
// (PgBouncer) would hand the next statement to another session and the lock would be lost.
type AdvisoryLock struct {
	db  *sql.DB
	key int64
}

// NewAdvisoryLock returns the lock identified by key. Every replica must use the same key.
func NewAdvisoryLock(database *sql.DB, key int64) *AdvisoryLock {
	return &AdvisoryLock{db: database, key: key}
}

// Lease is a lock that was taken. Held says whether the session that holds it is still alive; Release
// gives the lock back, even if the context of the work is already cancelled.
type Lease struct {
	lock *AdvisoryLock
	conn *sql.Conn
}

// TryLock takes the lock without waiting. ok is false when another session holds it. When ok is true the
// caller must call Release on the lease.
func (l *AdvisoryLock) TryLock(ctx context.Context) (lease *Lease, ok bool, err error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRowContext(ctx, tryAdvisoryLockQuery, l.key).Scan(&ok); err != nil {
		// The server may have granted the lock before the answer was lost (the context ended
		// mid-query). The connection must not go back to the pool still holding it: advisory locks are
		// re-entrant, so the next TryLock on that connection would "succeed" and one unlock would not free it.
		discard(conn)
		return nil, false, err
	}
	if !ok {
		_ = conn.Close()
		return nil, false, nil
	}
	return &Lease{lock: l, conn: conn}, true, nil
}

// Held reports whether the session that holds the lock is still alive. A lock is lost with its
// session (the connection died, the server restarted); the work must stop, since another replica may
// have taken over.
func (le *Lease) Held(ctx context.Context) bool {
	return le.conn.PingContext(ctx) == nil
}

// Release unlocks on the same connection that took the lock, since the lock belongs to the session.
// When the unlock fails the connection is discarded instead of going back to the pool, which ends the
// session and with it the lock.
func (le *Lease) Release() {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()

	var unlocked bool
	if err := le.conn.QueryRowContext(ctx, advisoryUnlockQuery, le.lock.key).Scan(&unlocked); err != nil || !unlocked {
		discard(le.conn)
		return
	}
	_ = le.conn.Close()
}

// discard closes the connection for good, so its session (and any lock it holds) ends.
func discard(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}
