package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/zanpatryk/tokentransferapi/graph"
	"github.com/zanpatryk/tokentransferapi/graph/generated"
	"github.com/zanpatryk/tokentransferapi/store"
)

func main() {

	port := os.Getenv("PORT")

	if port == "" {
		port = "8080"
	}

	memStore := store.NewInMemWalletStore()

	_, err := memStore.CreateIfNotExists(
		context.Background(),
		"0x0000000000000000000000000000000000000000",
		10,
	)

	if err != nil {
		log.Fatalf("Failed to set initial wallet: %v", err)
	}

	_, err2 := memStore.CreateIfNotExists(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		10,
	)

	if err2 != nil {
		log.Fatalf("Failed to set initial wallet: %v", err)
	}

	resolver := &graph.Resolver{Store: memStore}

	server := handler.NewDefaultServer(
		generated.NewExecutableSchema(
			generated.Config{Resolvers: resolver},
		),
	)

	http.Handle("/", playground.Handler("BTP Token Playground", "/graphql"))

	http.Handle("/graphql", server)

	log.Printf("Server started at http://localhost:%s/ (Playground)", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
