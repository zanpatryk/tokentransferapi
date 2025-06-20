package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zanpatryk/tokentransferapi/graph/generated"
)

type WalletStore interface {
	GetByAddress(ctx context.Context, address string) (*generated.Wallet, error)
	ListAll(ctx context.Context) ([]*generated.Wallet, error)

	CreateIfNotExists(ctx context.Context, address string, initialBalance int) (*generated.Wallet, error)

	Transfer(ctx context.Context, from string, transfer TransferOp) (int, error)
}

type InMemWalletStore struct {
	mu      sync.Mutex
	wallets map[string]*generated.Wallet
}

func NewInMemWalletStore() *InMemWalletStore {
	return &InMemWalletStore{
		wallets: make(map[string]*generated.Wallet),
	}
}

type TransferOp struct {
	To     string
	Amount int
}

func (s *InMemWalletStore) GetByAddress(ctx context.Context, address string) (*generated.Wallet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, exists := s.wallets[address]

	if !exists {
		return nil, errors.New("wallet not found")
	}

	return &generated.Wallet{
		Address:   w.Address,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}, nil
}

func (s *InMemWalletStore) ListAll(ctx context.Context) ([]*generated.Wallet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*generated.Wallet

	for _, w := range s.wallets {
		out = append(out, &generated.Wallet{
			Address:   w.Address,
			Balance:   w.Balance,
			CreatedAt: w.CreatedAt,
			UpdatedAt: w.UpdatedAt,
		})
	}
	return out, nil
}

func (s *InMemWalletStore) CreateIfNotExists(ctx context.Context, address string, initialBalance int) (*generated.Wallet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if w, exists := s.wallets[address]; exists {
		return &generated.Wallet{
			Address:   w.Address,
			Balance:   w.Balance,
			CreatedAt: w.CreatedAt,
			UpdatedAt: w.UpdatedAt,
		}, nil
	}

	now := time.Now().UTC()
	w := &generated.Wallet{
		Address:   address,
		Balance:   initialBalance,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.wallets[address] = w

	return &generated.Wallet{
		Address:   w.Address,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}, nil
}

func (s *InMemWalletStore) Transfer(ctx context.Context, from string, op TransferOp) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	senderW, ok := s.wallets[from]

	if !ok {
		return 0, errors.New("sender not found")
	}

	now := time.Now().UTC()

	toAddr, rawAmt := op.To, op.Amount

	if rawAmt >= 0 {

		if senderW.Balance < rawAmt {
			return 0, fmt.Errorf("insufficient funds")
		}

		senderW.Balance -= rawAmt

		recW, ok := s.wallets[toAddr]
		if !ok {
			senderW.Balance += rawAmt
		}

		recW.Balance += rawAmt
		recW.UpdatedAt = now
	} else {
		absAmt := -rawAmt

		recW, exists := s.wallets[toAddr]

		if !exists || recW.Balance < absAmt {
			return 0, fmt.Errorf("insufficient funds")
		}

		recW.Balance -= absAmt
		recW.UpdatedAt = now

		senderW.Balance += absAmt
	}

	senderW.UpdatedAt = now

	return senderW.Balance, nil
}
