package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxPool struct {
	pool *pgxpool.Pool
}

func newPGXPool(pool *pgxpool.Pool) physicalPool {
	return &pgxPool{pool: pool}
}

func (pool *pgxPool) acquire(ctx context.Context) (physicalConnection, error) {
	connection, err := pool.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	return &pgxPhysicalConnection{connection: connection}, nil
}

type pgxPhysicalConnection struct {
	connection *pgxpool.Conn
}

func (connection *pgxPhysicalConnection) beginTx(
	ctx context.Context,
	options pgx.TxOptions,
) (tenantTransaction, error) {
	transaction, err := connection.connection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}

	return &pgxTenantTransaction{transaction: transaction}, nil
}

func (connection *pgxPhysicalConnection) queryRow(
	ctx context.Context,
	sql string,
	arguments ...any,
) rowScanner {
	return connection.connection.QueryRow(ctx, sql, arguments...)
}

func (connection *pgxPhysicalConnection) txStatus() byte {
	return connection.connection.Conn().PgConn().TxStatus()
}

func (connection *pgxPhysicalConnection) release() {
	connection.connection.Release()
}

func (connection *pgxPhysicalConnection) hijackAndClose(ctx context.Context) error {
	hijacked := connection.connection.Hijack()
	return hijacked.Close(ctx)
}

type pgxTenantTransaction struct {
	transaction pgx.Tx
}

func (transaction *pgxTenantTransaction) queryRow(
	ctx context.Context,
	sql string,
	arguments ...any,
) rowScanner {
	return transaction.transaction.QueryRow(ctx, sql, arguments...)
}

func (transaction *pgxTenantTransaction) commit(ctx context.Context) error {
	return transaction.transaction.Commit(ctx)
}

func (transaction *pgxTenantTransaction) rollback(ctx context.Context) error {
	return transaction.transaction.Rollback(ctx)
}
