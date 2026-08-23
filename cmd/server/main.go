package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventbus"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/repository"
	"go-odtbank/internal/service"
)

type TransferRequest struct {
	Amount   float64 `json:"amount"`
	SourceID string  `json:"source_account_id"`
	DestID   string  `json:"destination_account_id"`
}

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

	repo := repository.NewMemoryAccountRepository(store)

	eb := eventbus.NewEventBus()

	// 2. Initialize business logic
	feePolicy := &policy.ZeroFeePolicy{}
	timeService := &policy.DefaultTimeService{ServiceAvailable: true}

	eventBusFunc := func(event domain.TransferCompletedEvent) {
		eb.Publish(event)
	}

	transferService := service.NewTransferService(store, feePolicy, timeService, eventBusFunc)

	// 3. Setup router
	r := mux.NewRouter()
	corsOrigins := os.Getenv("CORS_ORIGINS") // comma-separated allowlist; empty means reflect request Origin
	withCORS := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			origin := req.Header.Get("Origin")
			if corsOrigins != "" && !strings.Contains(corsOrigins, origin) {
				origin = ""
			}
			w.Header().Set("Access-Control-Allow-Origin", firstNonEmpty(origin, "*"))
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.ServeHTTP(w, req)
		})
	}
	r.HandleFunc("/transfer", func(w http.ResponseWriter, r *http.Request) {
		var req TransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		receipt, err := transferService.Transfer(req.Amount, req.SourceID, req.DestID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}

		writeJSON(w, http.StatusOK, receipt)
	}).Methods("POST")

	r.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		accounts, err := listAccounts(store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
	}).Methods("GET")

	r.HandleFunc("/accounts/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		events, err := store.Load(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"aggregate_id": id,
			"events":       toEventDTOs(events),
		})
	}).Methods("GET")

	_ = repo

	// 4. Start server
	srv := &http.Server{
		Handler:      withCORS(r),
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
// writeJSON encodes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// writeError sends a uniform JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// firstNonEmpty returns the first non-empty string, or fallback if none.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "*"
}

// statusForError maps domain errors to HTTP status codes.
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidTransferAmount):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrOutOfService):
		return http.StatusServiceUnavailable
	case errors.Is(err, domain.ErrAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, eventstore.ErrConcurrencyConflict):
		return http.StatusConflict
	default:
		var fundErr *domain.InsufficientFundsError
		if errors.As(err, &fundErr) {
			return http.StatusUnprocessableEntity
		}
		return http.StatusInternalServerError
	}
}

// accountDTO is the wire shape for GET /accounts entries.
type accountDTO struct {
	ID         string  `json:"id"`
	Balance    float64 `json:"balance"`
	EventCount int     `json:"event_count"`
}

func listAccounts(store eventstore.Store) ([]accountDTO, error) {
	ids, err := store.ListAggregates()
	if err != nil {
		return nil, err
	}
	out := make([]accountDTO, 0, len(ids))
	for _, id := range ids {
		events, err := store.Load(id)
		if err != nil {
			return nil, err
		}
		out = append(out, accountDTO{
			ID:         id,
			Balance:    domain.ReplayAccount(id, events).Balance,
			EventCount: len(events),
		})
	}
	return out, nil
}

// eventDTO is the wire shape for stored events.
type eventDTO struct {
	Seq        int     `json:"seq"`
	Type       string  `json:"type"`
	Amount     float64 `json:"amount,omitempty"`
	OccurredAt string  `json:"occurred_at"`
}

func toEventDTOs(events []domain.Event) []eventDTO {
	out := make([]eventDTO, 0, len(events))
	for _, ev := range events {
		dto := eventDTO{Seq: ev.Version(), Type: ev.EventType(), OccurredAt: ev.OccurredAt().UTC().Format(time.RFC3339)}
		switch e := ev.(type) {
		case domain.AccountOpened:
			dto.Amount = e.InitialBalance
		case domain.MoneyDebited:
			dto.Amount = e.Amount
		case domain.MoneyCredited:
			dto.Amount = e.Amount
		}
		out = append(out, dto)
	}
	return out
}

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
