package objectstore_test

import (
	"context"
	"os"
	"testing"

	"go-odtbank/internal/objectstore"
)

func TestMinIOStoreIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("set MINIO_INTEGRATION=1 to run against local MinIO")
	}
	ctx := context.Background()
	store, err := objectstore.NewMinIOStore(ctx, "localhost:9000", "minioadmin", "minioadmin", "odtbank-passports-test", false)
	if err != nil {
		t.Fatal(err)
	}
	key := "passports/integration-test"
	want := []byte("passport-image")
	if err := store.Put(ctx, key, want, "image/png"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, key) })
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("object body = %q, want %q", got, want)
	}
}
