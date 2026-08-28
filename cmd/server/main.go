package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventbus"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/httpapi"
	"go-odtbank/internal/objectstore"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/repository"
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

	transferStore, ok := store.(domain.TransferSagaStore)
	if !ok {
		log.Fatal("configured store does not support atomic transfers")
	}
	transferService := service.NewTransferService(transferStore, feePolicy, timeService, eventBusFunc)
	go transferService.Run(context.Background())
	depositService := service.NewDepositService(store)
	withdrawService := service.NewWithdrawService(store)
	onboardingStore, ok := store.(domain.OnboardingStore)
	if !ok {
		log.Fatal("configured store does not support onboarding")
	}
	onboardingService := service.NewOnboardingService(onboardingStore)
	authStore, ok := store.(domain.AuthStore)
	if !ok {
		log.Fatal("configured store does not support authentication")
	}
	authService := service.NewAuthService(authStore)
	reviewStore, ok := store.(domain.ReviewStore)
	if !ok {
		log.Fatal("configured store does not support application review")
	}
	reviewService := service.NewReviewService(reviewStore)
	adjustmentStore, ok := store.(domain.AdjustmentStore)
	if !ok {
		log.Fatal("configured store does not support adjustments")
	}
	adjustmentService := service.NewAdjustmentService(adjustmentStore)
	if _, memory := store.(*eventstore.MemoryStore); memory {
		email, password := os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD")
		if (email == "") != (password == "") {
			log.Fatal("ADMIN_EMAIL and ADMIN_PASSWORD must be set together")
		}
		if email != "" {
			hash, err := service.HashPassword(password)
			if err != nil {
				log.Fatal("invalid ADMIN_PASSWORD")
			}
			if err := authStore.UpsertAdmin(domain.Admin{ID: "adm_dev", Email: strings.ToLower(strings.TrimSpace(email)), PasswordHash: hash}); err != nil {
				log.Fatalf("seed admin: %v", err)
			}
		}
	}

	// 3. Setup HTTP transport
	accountRepo := repository.NewMemoryAccountRepository(store, envIntOrDefault("SNAPSHOT_THRESHOLD", 50))
	handler := httpapi.NewRouter(httpapi.Dependencies{
		Store:             store,
		AccountRepository: accountRepo,
		TransferService:   transferService,
		DepositService:    depositService,
		WithdrawService:   withdrawService,
		OnboardingService: onboardingService,
		AuthService:       authService,
		ReviewService:     reviewService,
		AdjustmentService: adjustmentService,
		CookieSecure:      os.Getenv("COOKIE_SECURE") == "true",
		CORSOrigins:       os.Getenv("CORS_ORIGINS"),
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
	endpoint := envOrDefault("MINIO_ENDPOINT", "localhost:9000")
	accessKey := envOrDefault("MINIO_ACCESS_KEY", "minioadmin")
	secretKey := envOrDefault("MINIO_SECRET_KEY", "minioadmin")
	bucket := envOrDefault("MINIO_BUCKET", "odtbank-passports")
	passports, err := objectstore.NewMinIOStore(ctx, endpoint, accessKey, secretKey, bucket, os.Getenv("MINIO_USE_SSL") == "true")
	if err != nil {
		pool.Close()
		return nil, err
	}
	fmt.Printf("[store] passport images use MinIO bucket %s\n", bucket)
	return eventstore.NewPostgresStore(pool, passports), nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envIntOrDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
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
		amount domain.Money
	}{
		{"acc1", 10000},
		{"acc2", 5000},
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
