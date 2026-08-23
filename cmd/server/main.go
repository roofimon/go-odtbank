package main

import (
	"encoding/json"
	"fmt"
	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventbus"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/repository"
	"go-odtbank/internal/service"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type TransferRequest struct {
	Amount   float64 `json:"amount"`
	SourceID string  `json:"source_account_id"`
	DestID   string  `json:"destination_account_id"`
}

func main() {
	// 1. Initialize infrastructure
	store := eventstore.NewMemoryStore()
	seedAccount(store, "acc1", 100.0)
	seedAccount(store, "acc2", 50.0)

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
	r.HandleFunc("/transfer", func(w http.ResponseWriter, r *http.Request) {
		var req TransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		receipt, err := transferService.Transfer(req.Amount, req.SourceID, req.DestID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(receipt)
	}).Methods("POST")

	_ = repo

	// 4. Start server
	srv := &http.Server{
		Handler:      r,
		Addr:         ":8080",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
	}

	fmt.Println("Server starting on :8080...")
	log.Fatal(srv.ListenAndServe())
}

// seedAccount appends an AccountOpened event so the aggregate exists in the store.
func seedAccount(store eventstore.Store, id string, initialBalance float64) {
	_ = store.Append(domain.AccountOpened{
		Aggregate:      id,
		Type:           "AccountOpened",
		Seq:            0,
		Occurred:        time.Now(),
		ID:             id,
		InitialBalance: initialBalance,
	}, 0)
}