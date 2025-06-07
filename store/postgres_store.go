package store

import (
	"context"
	"fmt"
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
	_, err := s.db.Exec(ctx,
		`INSERT INTO wallets(address, balance, created_at, updated_at)
         VALUES($1, $2, now(), now())
         ON CONFLICT (address) DO NOTHING`, addr, strconv.Itoa(initialBalance))
	return &generated.Wallet{
		Address:   addr,
		Balance:   initialBalance,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, err
}

func (s *PostgresWalletStore) Transfer(ctx context.Context, from string, ops []TransferOp,
) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Lock sender row up front
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM wallets WHERE address = $1 FOR UPDATE`, from,
	); err != nil {
		return 0, err
	}

	failed := make([]int, 0, len(ops))

	for _, op := range ops {
		amt := op.Amount
		toAddr := op.To

		switch {
		case amt >= 0:
			res, err := tx.Exec(ctx,
				`UPDATE wallets
                     SET balance = balance - $1
                   WHERE address = $2
                     AND balance >= $1`,
				amt, from,
			)
			if err != nil {
				return 0, err
			}
			if rows := res.RowsAffected(); rows == 0 {
				failed = append(failed, amt)
				continue
			}

			if _, err := tx.Exec(ctx,
				`INSERT INTO wallets(address, balance, created_at, updated_at)
                     VALUES($1, 0, now(), now())
                  ON CONFLICT (address) DO NOTHING`,
				toAddr,
			); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(ctx,
				`SELECT 1 FROM wallets WHERE address = $1 FOR UPDATE`, toAddr,
			); err != nil {
				return 0, err
			}

			if _, err := tx.Exec(ctx,
				`UPDATE wallets
                     SET balance = balance + $1,
                         updated_at = $2
                   WHERE address = $3`,
				amt, now, toAddr,
			); err != nil {
				return 0, err
			}

		case amt < 0:
			absAmt := -amt

			if _, err := tx.Exec(ctx,
				`SELECT 1 FROM wallets WHERE address = $1 FOR UPDATE`, toAddr,
			); err != nil {
				return 0, err
			}

			res, err := tx.Exec(ctx,
				`UPDATE wallets
                     SET balance = balance - $1
                   WHERE address = $2
                     AND balance >= $1`,
				absAmt, toAddr,
			)
			if err != nil {
				return 0, err
			}
			if rows := res.RowsAffected(); rows == 0 {

				failed = append(failed, amt)
				continue
			}

			if _, err := tx.Exec(ctx,
				`UPDATE wallets
                     SET balance = balance + $1,
                         updated_at = $2
                   WHERE address = $3`,
				absAmt, now, from,
			); err != nil {
				return 0, err
			}
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE wallets
             SET updated_at = $1
           WHERE address = $2`,
		now, from,
	); err != nil {
		return 0, err
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

	if len(failed) > 0 {
		return finalBal, fmt.Errorf(
			"Insufficient funds for transaction(s): %v",
			failed,
		)
	}

	return finalBal, nil
}
