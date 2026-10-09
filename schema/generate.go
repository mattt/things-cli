//go:build ignore

// Write batch.schema.json. Run with go generate ./schema.
package main

import (
	"log"
	"os"

	"github.com/mattt/things-cli/schema"
)

func main() {
	b, err := schema.Batch()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("batch.schema.json", b, 0644); err != nil {
		log.Fatal(err)
	}
}
