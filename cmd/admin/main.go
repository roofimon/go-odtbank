package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/service"
)

func main() {
	dsn, email, password := os.Getenv("DATABASE_URL"), strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))), os.Getenv("ADMIN_PASSWORD")
	if dsn == "" || email == "" || password == "" {
		log.Fatal("DATABASE_URL, ADMIN_EMAIL, and ADMIN_PASSWORD are required")
	}
	hash, err := service.HashPassword(password)
	if err != nil {
		log.Fatal("ADMIN_PASSWORD must contain 10 to 128 characters")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		log.Fatal(err)
	}
	store := eventstore.NewPostgresStore(pool)
	if err = store.UpsertAdmin(domain.Admin{ID: "adm_" + hex.EncodeToString(raw), Email: email, PasswordHash: hash}); err != nil {
		log.Fatal(err)
	}
	log.Printf("admin %s is ready", email)
}
