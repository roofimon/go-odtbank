package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

type Dependencies struct {
	Store             eventstore.Store
	TransferService   domain.TransferService
	DepositService    domain.DepositService
	WithdrawService   domain.WithdrawService
	OnboardingService domain.OnboardingService
	AuthService       domain.AuthService
	CookieSecure      bool
	CORSOrigins       string
}

func NewRouter(deps Dependencies) http.Handler {
	router := mux.NewRouter()
	router.HandleFunc("/onboarding", handleOnboarding(deps.OnboardingService)).Methods(http.MethodPost)
	router.HandleFunc("/login", handleLogin(deps.AuthService, deps.CookieSecure)).Methods(http.MethodPost)
	protect := func(handler http.HandlerFunc) http.HandlerFunc { return handler }
	if deps.AuthService != nil {
		protect = func(handler http.HandlerFunc) http.HandlerFunc { return requireAuth(deps.AuthService, handler) }
	}
	router.HandleFunc("/logout", protect(handleLogout(deps.AuthService, deps.CookieSecure))).Methods(http.MethodPost)
	router.HandleFunc("/me", protect(handleMe())).Methods(http.MethodGet)
	router.HandleFunc("/transfer", protect(handleTransfer(deps.TransferService))).Methods(http.MethodPost)
	router.HandleFunc("/deposit", protect(handleDeposit(deps.DepositService))).Methods(http.MethodPost)
	router.HandleFunc("/withdraw", protect(handleWithdraw(deps.WithdrawService))).Methods(http.MethodPost)
	router.HandleFunc("/accounts", protect(handleListAccounts(deps.Store))).Methods(http.MethodGet)
	router.HandleFunc("/accounts/{id}/events", protect(handleAccountEvents(deps.Store))).Methods(http.MethodGet)
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
		if !authorizedAccount(r, req.SourceID) {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}

		receipt, err := transferService.Transfer(req.Amount, req.SourceID, req.DestID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"InitialSourceAccount": receipt.InitialSourceAccount,
			"FinalSourceAccount":   receipt.FinalSourceAccount,
			"DestinationAccountID": req.DestID,
			"TransferAmount":       receipt.TransferAmount,
			"FeeAmount":            receipt.FeeAmount,
		})
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
		if !authorizedAccount(r, req.AccountID) {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
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

type withdrawRequest struct {
	Amount    float64 `json:"amount"`
	AccountID string  `json:"account_id"`
}

func handleWithdraw(withdrawService domain.WithdrawService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req withdrawRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !authorizedAccount(r, req.AccountID) {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}

		receipt, err := withdrawService.Withdraw(req.Amount, req.AccountID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	}
}

type onboardingRequest struct {
	LegalFirstName     string  `json:"legal_first_name"`
	LegalLastName      string  `json:"legal_last_name"`
	DateOfBirth        string  `json:"date_of_birth"`
	Nationality        string  `json:"nationality"`
	Email              string  `json:"email"`
	Phone              string  `json:"phone"`
	Password           string  `json:"password"`
	InitialDeposit     float64 `json:"initial_deposit"`
	ResidentialAddress struct {
		Line1           string `json:"line1"`
		Line2           string `json:"line2"`
		City            string `json:"city"`
		StateOrProvince string `json:"state_or_province"`
		PostalCode      string `json:"postal_code"`
		Country         string `json:"country"`
	} `json:"residential_address"`
	GovernmentDocument struct {
		Type           string `json:"type"`
		Number         string `json:"number"`
		IssuingCountry string `json:"issuing_country"`
	} `json:"government_document"`
}

func handleOnboarding(onboardingService domain.OnboardingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
		if err := r.ParseMultipartForm(6 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart request")
			return
		}
		var req onboardingRequest
		if err := json.Unmarshal([]byte(r.FormValue("payload")), &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid onboarding payload")
			return
		}
		image, _, err := r.FormFile("passport_image")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "passport_image is required", "field": "passport_image"})
			return
		}
		defer image.Close()
		passportImage, err := io.ReadAll(io.LimitReader(image, (5<<20)+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read passport image")
			return
		}
		command := domain.OnboardingCommand{
			LegalFirstName: req.LegalFirstName, LegalLastName: req.LegalLastName,
			DateOfBirth: req.DateOfBirth, Nationality: req.Nationality,
			Email: req.Email, Phone: req.Phone, Password: req.Password, InitialDeposit: req.InitialDeposit,
			ResidentialAddress: domain.ResidentialAddress{
				Line1: req.ResidentialAddress.Line1, Line2: req.ResidentialAddress.Line2,
				City: req.ResidentialAddress.City, StateOrProvince: req.ResidentialAddress.StateOrProvince,
				PostalCode: req.ResidentialAddress.PostalCode, Country: req.ResidentialAddress.Country,
			},
			GovernmentDocument: domain.GovernmentDocument{
				Type: req.GovernmentDocument.Type, Number: req.GovernmentDocument.Number,
				IssuingCountry: req.GovernmentDocument.IssuingCountry,
			},
			PassportImage: passportImage,
		}
		receipt, err := onboardingService.Onboard(command)
		if err != nil {
			var validationErr *domain.OnboardingValidationError
			if errors.As(err, &validationErr) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationErr.Message, "field": validationErr.Field})
				return
			}
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, receipt)
	}
}

func handleListAccounts(store eventstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if principal := principalFromRequest(r); principal != nil {
			events, err := store.Load(principal.AccountID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"accounts": []accountDTO{{ID: principal.AccountID, Balance: domain.ReplayAccount(principal.AccountID, events).Balance, EventCount: len(events)}}})
			return
		}
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
		if !authorizedAccount(r, id) {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}
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

const sessionCookie = "odtbank_session"

type principalContextKey struct{}

func requireAuth(auth domain.AuthService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, domain.ErrUnauthorized.Error())
			return
		}
		principal, err := auth.Authenticate(cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, domain.ErrUnauthorized.Error())
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	}
}

func principalFromRequest(r *http.Request) *domain.Principal {
	principal, _ := r.Context().Value(principalContextKey{}).(*domain.Principal)
	return principal
}

func authorizedAccount(r *http.Request, accountID string) bool {
	principal := principalFromRequest(r)
	return principal == nil || principal.AccountID == accountID
}

func handleLogin(auth domain.AuthService, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		token, principal, err := auth.Login(req.Email, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, domain.ErrInvalidCredentials.Error())
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
		writeJSON(w, http.StatusOK, principal)
	}
}

func handleLogout(auth domain.AuthService, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			_ = auth.Logout(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, principalFromRequest(r)) }
}

func withCORS(next http.Handler, corsOrigins string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		if origin != "" && corsOrigins != "" && !originAllowed(corsOrigins, origin) {
			origin = ""
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Credentials", "true")
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

func originAllowed(allowlist, origin string) bool {
	for _, allowed := range strings.Split(allowlist, ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
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

func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidTransferAmount), errors.Is(err, domain.ErrInvalidDepositAmount), errors.Is(err, domain.ErrInvalidWithdrawAmount):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrOutOfService):
		return http.StatusServiceUnavailable
	case errors.Is(err, domain.ErrAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, eventstore.ErrConcurrencyConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrCustomerAlreadyExists):
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
