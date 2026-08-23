package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

type Dependencies struct {
	Store           eventstore.Store
	TransferService domain.TransferService
	DepositService  domain.DepositService
	CORSOrigins     string
}

func NewRouter(deps Dependencies) http.Handler {
	router := mux.NewRouter()
	router.HandleFunc("/transfer", handleTransfer(deps.TransferService)).Methods(http.MethodPost)
	router.HandleFunc("/deposit", handleDeposit(deps.DepositService)).Methods(http.MethodPost)
	router.HandleFunc("/accounts", handleListAccounts(deps.Store)).Methods(http.MethodGet)
	router.HandleFunc("/accounts/{id}/events", handleAccountEvents(deps.Store)).Methods(http.MethodGet)
	return withCORS(router, deps.CORSOrigins)
}

type transferRequest struct {
	Amount   float64 `json:"amount"`
	SourceID string  `json:"source_account_id"`
	DestID   string  `json:"destination_account_id"`
}

func handleTransfer(transferService domain.TransferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req transferRequest
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
	}
}

type depositRequest struct {
	Amount    float64 `json:"amount"`
	AccountID string  `json:"account_id"`
}

func handleDeposit(depositService domain.DepositService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req depositRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		receipt, err := depositService.Deposit(req.Amount, req.AccountID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	}
}

func handleListAccounts(store eventstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		accounts, err := listAccounts(store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
	}
}

func handleAccountEvents(store eventstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func withCORS(next http.Handler, corsOrigins string) http.Handler {
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
		next.ServeHTTP(w, req)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "*"
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidTransferAmount), errors.Is(err, domain.ErrInvalidDepositAmount):
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
			ID: id, Balance: domain.ReplayAccount(id, events).Balance, EventCount: len(events),
		})
	}
	return out, nil
}

type eventDTO struct {
	Seq        int     `json:"seq"`
	Type       string  `json:"type"`
	Amount     float64 `json:"amount,omitempty"`
	OccurredAt string  `json:"occurred_at"`
}

func toEventDTOs(events []domain.Event) []eventDTO {
	out := make([]eventDTO, 0, len(events))
	for _, event := range events {
		dto := eventDTO{Seq: event.Version(), Type: event.EventType(), OccurredAt: event.OccurredAt().UTC().Format(time.RFC3339)}
		switch typedEvent := event.(type) {
		case domain.AccountOpened:
			dto.Amount = typedEvent.InitialBalance
		case domain.MoneyDebited:
			dto.Amount = typedEvent.Amount
		case domain.MoneyCredited:
			dto.Amount = typedEvent.Amount
		}
		out = append(out, dto)
	}
	return out
}
