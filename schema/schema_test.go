package schema

import (
	"bytes"
	_ "embed"
	"testing"
)

//go:embed batch.schema.json
var published []byte

func TestPublishedBatchSchema(t *testing.T) {
	b, err := Batch()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, published) {
		t.Fatal("batch.schema.json is stale; run go generate ./schema")
	}
}
