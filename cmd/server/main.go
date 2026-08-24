package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventbus"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/httpapi"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/service"
)

func main() {
	// 1. Initialize infrastructure
	store, err := openStore(context.Background())
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer closeStore(store)

	if err := seedIfEmpty(store); err != nil {
		log.Fatalf("seed accounts: %v", err)
	}

	eb := eventbus.NewEventBus()

	// 2. Initialize business logic
	feePolicy := &policy.ZeroFeePolicy{}
	timeService := &policy.DefaultTimeService{ServiceAvailable: true}

	eventBusFunc := func(event domain.TransferCompletedEvent) {
		eb.Publish(event)
	}

	transferService := service.NewTransferService(store, feePolicy, timeService, eventBusFunc)
	depositService := service.NewDepositService(store)
	withdrawService := service.NewWithdrawService(store)

	// 3. Setup HTTP transport
	handler := httpapi.NewRouter(httpapi.Dependencies{
		Store:           store,
		TransferService: transferService,
		DepositService:  depositService,
		WithdrawService: withdrawService,
		CORSOrigins:     os.Getenv("CORS_ORIGINS"),
	})

	// 4. Start server
	srv := &http.Server{
		Handler:      handler,
		Addr:         ":8080",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
	}

	fmt.Println("Server starting on :8080...")
	log.Fatal(srv.ListenAndServe())
}

// openStore picks the implementation based on whether DATABASE_URL is set.
// If set, it connects to Postgres. Otherwise it falls back to the in-memory store.
func openStore(ctx context.Context) (eventstore.Store, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Println("[store] DATABASE_URL not set, using in-memory store")
		return eventstore.NewMemoryStore(), nil
	}

	fmt.Println("[store] DATABASE_URL set, connecting to Postgres")
	pgCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(pgCtx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(pgCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return eventstore.NewPostgresStore(pool), nil
}

func closeStore(store eventstore.Store) {
	if pg, ok := store.(*eventstore.PostgresStore); ok {
		pg.Close()
	}
}

// seedIfEmpty appends AccountOpened events for the demo accounts if they don't
// exist yet. Idempotent — running twice does nothing on the second run.
func seedIfEmpty(store eventstore.Store) error {
	for _, acc := range []struct {
		id     string
		amount float64
	}{
		{"acc1", 100.0},
		{"acc2", 50.0},
	} {
		events, err := store.Load(acc.id)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			continue
		}
		if err := store.Append(domain.AccountOpened{
			Aggregate:      acc.id,
			Type:           "AccountOpened",
			Seq:            0,
			Occurred:       time.Now(),
			ID:             acc.id,
			InitialBalance: acc.amount,
		}, 0); err != nil {
			return err
		}
	}
	return nil
}
