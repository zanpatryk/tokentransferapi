package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/golang-migrate/migrate/v4"
	postgresDriver "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/zanpatryk/tokentransferapi/graph"
	"github.com/zanpatryk/tokentransferapi/graph/generated"
	"github.com/zanpatryk/tokentransferapi/store"
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found!")
	}
}

func main() {

	port := os.Getenv("PORT")

	if port == "" {
		port = "8080"
	}

	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		log.Fatal("DATABASE_URL is not set !")
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		log.Fatal("MIGRATIONS_PATH must be set")
	}

	sqlDB, errSql := sql.Open("postgres", dbUrl)
	if errSql != nil {
		log.Fatalf("could not open sql.DB: %v", errSql)

	}
	defer sqlDB.Close()

	driver, err := postgresDriver.WithInstance(sqlDB, &postgresDriver.Config{})
	if err != nil {
		log.Fatalf("could not create migrate driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		log.Fatalf("failed to initialize migrations: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migration failed: %v", err)
	}
	log.Println("Database migrations applied")

	pool, err := pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		log.Fatalf("failed to connect to Postgres pool: %v", err)
	}
	defer pool.Close()

	resolverStore := store.NewPostgresWalletStore(pool)

	_, errAddr1 := resolverStore.CreateIfNotExists(
		context.Background(),
		"0x0000000000000000000000000000000000000000",
		1000,
	)

	if errAddr1 != nil {
		log.Fatalf("Failed to set initial wallet: %v", errAddr1)
	}

	_, errAddr2 := resolverStore.CreateIfNotExists(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		1000,
	)

	if errAddr2 != nil {
		log.Fatalf("Failed to set initial wallet: %v", errAddr2)
	}

	_, errAddr3 := resolverStore.CreateIfNotExists(
		context.Background(),
		"0x0000000000000000000000000000000000000002",
		1000,
	)

	if errAddr3 != nil {
		log.Fatalf("Failed to set initial wallet: %v", errAddr3)
	}

	server := handler.NewDefaultServer(
		generated.NewExecutableSchema(
			generated.Config{Resolvers: &graph.Resolver{Store: resolverStore}},
		),
	)

	http.Handle("/", playground.Handler("BTP Token Playground", "/graphql"))

	http.Handle("/graphql", server)

	log.Printf("Server started at http://localhost:%s/ (Playground)", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
