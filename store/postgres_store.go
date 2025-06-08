package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zanpatryk/tokentransferapi/graph/generated"
)

type PostgresWalletStore struct {
	db *pgxpool.Pool
}

func NewPostgresWalletStore(db *pgxpool.Pool) *PostgresWalletStore {
	return &PostgresWalletStore{db: db}
}

func (s *PostgresWalletStore) GetByAddress(ctx context.Context, addr string) (*generated.Wallet, error) {
	w := &generated.Wallet{}
	row := s.db.QueryRow(ctx,
		`SELECT address, balance, created_at, updated_at
	FROM wallets WHERE address=$1`, addr)
	var balanceStr string

	if err := row.Scan(&w.Address, &balanceStr, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	w.Balance, _ = strconv.Atoi(balanceStr)
	return w, nil
}

func (s *PostgresWalletStore) ListAll(ctx context.Context) ([]*generated.Wallet, error) {
	rows, err := s.db.Query(ctx,
		`SELECT address, balance, created_at, updated_at FROM wallets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*generated.Wallet
	for rows.Next() {
		w := &generated.Wallet{}
		var balanceStr string
		if err := rows.Scan(&w.Address, &balanceStr, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.Balance, _ = strconv.Atoi(balanceStr)
		result = append(result, w)
	}
	return result, nil
}

func (s *PostgresWalletStore) CreateIfNotExists(ctx context.Context, addr string, initialBalance int) (*generated.Wallet, error) {

	if _, err := s.db.Exec(ctx, `
        INSERT INTO wallets(address, balance, created_at, updated_at)
        VALUES ($1, $2, now(), now())
        ON CONFLICT (address) DO NOTHING
    `, addr, initialBalance); err != nil {
		return nil, fmt.Errorf("insert wallet: %w", err)
	}

	w := &generated.Wallet{}
	if err := s.db.QueryRow(ctx, `
        SELECT address, balance, created_at, updated_at
          FROM wallets
         WHERE address = $1
    `, addr).Scan(&w.Address, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, fmt.Errorf("fetch wallet: %w", err)
	}

	return w, nil
}

func (s *PostgresWalletStore) Transfer(ctx context.Context, from string, op TransferOp) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	addrs := []string{from, op.To}
	sort.Strings(addrs)

	for _, addr := range addrs {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, addr,
		); err != nil {
			return 0, err
		}
	}

	now := time.Now().UTC()
	amount := op.Amount

	if amount >= 0 {
		res, err := tx.Exec(ctx,
			`UPDATE wallets
               SET balance = balance - $1, updated_at = $2
             WHERE address = $3 AND balance >= $1`,
			amount, now, from,
		)

		if err != nil {
			return 0, err
		}

		if res.RowsAffected() == 0 {
			if err := tx.Commit(ctx); err != nil {
				return 0, err
			}

			var bal int
			_ = tx.QueryRow(ctx,
				`SELECT balance FROM wallets WHERE address = $1`, from,
			).Scan(&bal)

			return bal, fmt.Errorf("Insufficient funds")
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO wallets(address, balance, created_at, updated_at)
                 VALUES($1, $2, now(), now())
             ON CONFLICT (address)
               DO UPDATE SET balance = wallets.balance + EXCLUDED.balance,
                             updated_at = now()`,
			op.To, amount,
		); err != nil {
			return 0, err
		}
	} else {

		absAmt := -amount

		res, err := tx.Exec(ctx,
			`UPDATE wallets
               SET balance = balance - $1, updated_at = $2
             WHERE address = $3 AND balance >= $1`,
			absAmt, now, op.To,
		)

		if err != nil {
			return 0, err
		}

		if res.RowsAffected() == 0 {
			if err := tx.Commit(ctx); err != nil {
				return 0, err
			}

			var bal int
			_ = tx.QueryRow(ctx,
				`SELECT balance FROM wallets WHERE address = $1`, from,
			).Scan(&bal)
			return bal, fmt.Errorf("Insufficient funds on recipient")
		}

		if _, err := tx.Exec(ctx,
			`UPDATE wallets
			   SET balance = balance + $1, updated_at = $2
			WHERE address = $3`,
			absAmt, now, from,
		); err != nil {
			return 0, nil
		}

	}

	var finalBal int
	if err := tx.QueryRow(ctx,
		`SELECT balance FROM wallets WHERE address = $1`, from,
	).Scan(&finalBal); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	return finalBal, nil
}
