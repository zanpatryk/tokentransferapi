package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	postgresDriver "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

var testStore *PostgresWalletStore
var dbPool *pgxpool.Pool

func TestMain(m *testing.M) {

	if err := godotenv.Load("../.env"); err != nil {
		log.Fatalf("Error loading .env: %v", err)
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("TEST_DATABASE_URL must be set for tests")
	}

	sqlDB, err := sql.Open("postgres", dbURL)

	if err != nil {
		log.Fatalf("Could not open databse: %v", err)
	}

	driver, err := postgresDriver.WithInstance(sqlDB, &postgresDriver.Config{})

	if err != nil {
		log.Fatalf("Could not create migration driver: %v", err)
	}

	migrator, err := migrate.NewWithDatabaseInstance(
		"file://../db/migrations",
		"postgres",
		driver,
	)
	if err != nil {
		log.Fatalf("Could not initiante migration: %v", err)
	}

	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("Migration failed: %v", err)
	}
	sqlDB.Close()

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Could not open pgxpool")
	}

	dbPool = pool
	testStore = NewPostgresWalletStore(pool)

	code := m.Run()

	_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS wallets; DROP TABLE IF EXISTS schema_migrations;")
	pool.Close()
	os.Exit(code)

}

func resetWallets(t *testing.T) {
	_, err := dbPool.Exec(context.Background(), "TRUNCATE wallets;")
	if err != nil {
		t.Fatalf("Failed to reset wallets table: %v", err)
	}
}

func TestCreateAndGet(t *testing.T) {
	resetWallets(t)

	ctx := context.Background()

	w, err := testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000001", 10)

	if err != nil {
		t.Fatalf("CreateIfNotExists error: %v", err)
	}

	if w.Balance != 10 {
		t.Errorf("Expected balance does not match. Expected 10, got: %v", w.Balance)
	}

	w2, err := testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000001", 999)

	if err != nil {
		t.Fatalf("CreateIfNotExists, second call, error: %v", err)
	}

	if w2.Balance != 10 {
		t.Errorf("Expected balance does not match. Expected 10, got: %v", w2.Balance)
	}

	got, err := testStore.GetByAddress(ctx, "0x0000000000000000000000000000000000000001")

	if err != nil {
		t.Fatalf("GetByAddress error: %v", err)
	}

	if got.Balance != 10 {
		t.Errorf("GetByAddress: Expected balance does not match. Expected 10, got: %v", got.Balance)
	}

	allWallets, err := testStore.ListAll(ctx)

	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}

	if len(allWallets) != 1 || allWallets[0].Address != "0x0000000000000000000000000000000000000001" {
		t.Errorf("ListAll: Expected one wallet `0x0000000000000000000000000000000000000001`, got: %v", allWallets)
	}
}

func TestTransferSuccess(t *testing.T) {
	resetWallets(t)

	ctx := context.Background()

	_, _ = testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000000", 10)
	_, _ = testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000001", 10)

	newBalance, err := testStore.Transfer(ctx, "0x0000000000000000000000000000000000000001", TransferOp{
		To: "0x0000000000000000000000000000000000000000", Amount: 10,
	})

	if err != nil {
		t.Fatalf("Transfer error: %v", err)
	}

	if newBalance != 0 {
		t.Errorf("Transfer: Expected sender balance 0, got: %v", newBalance)
	}

	addr, err := testStore.GetByAddress(ctx, "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("Get address error: %v", err)
	}

	if addr.Balance != 20 {
		t.Fatalf("Post-transfer address balance: expected 20, got: %v", addr.Balance)
	}
}

func TestTransferInsufficientFunds(t *testing.T) {
	resetWallets(t)

	ctx := context.Background()

	_, _ = testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000000", 10)
	_, _ = testStore.CreateIfNotExists(ctx, "0x0000000000000000000000000000000000000001", 10)

	_, err := testStore.Transfer(ctx, "0x0000000000000000000000000000000000000001", TransferOp{
		To: "0x0000000000000000000000000000000000000000", Amount: 20,
	})
	if err == nil {
		t.Fatalf("Expected error: Insuficient Funds, got nil")
	}
}

func TestTransferRaceConditions(t *testing.T) {
	resetWallets(t)
	ctx := context.Background()

	recipient := "0x0000000000000000000000000000000000000000"
	senders := []string{
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
		"0x0000000000000000000000000000000000000003",
	}

	for _, addr := range append([]string{recipient}, senders...) {
		if _, err := testStore.CreateIfNotExists(ctx, addr, 10); err != nil {
			t.Fatalf("seeding %s failed: %v", addr, err)
		}
	}

	type job struct {
		from   string
		to     string
		amount int
	}

	jobs := []job{
		{from: senders[0], to: recipient, amount: -4},
		{from: senders[1], to: recipient, amount: -7},
		{from: senders[2], to: recipient, amount: 1},
	}

	var wg sync.WaitGroup
	wg.Add(len(senders))

	for _, j := range jobs {
		go func(j job) {
			defer wg.Done()
			op := TransferOp{To: j.to, Amount: j.amount}
			newBal, err := testStore.Transfer(ctx, j.from, op)
			if err != nil {
				t.Errorf("Transfer from %s failed: %v", j.from, err)
				return
			}

			expected := 10 - j.amount
			if newBal != expected {
				t.Errorf("Transfer from %s: expected newBal %d, got %d", j.from, expected, newBal)
			}
		}(j)
	}

	wg.Wait()

	time.Sleep(50 * time.Microsecond)

	got0, err := testStore.GetByAddress(ctx, recipient)
	if err != nil {
		t.Fatalf("GetByAddress for recipient failed: %v", err)
	}

	fmt.Printf("Balance after transactions: %v", got0.Balance)

}
