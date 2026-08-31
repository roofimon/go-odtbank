package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

// accountReader derives current account state. The httpapi depends only on the
// read contract so it can use a snapshot-aware repository without the rest of the
// write-only parts of the domain repository interface.
type accountReader interface {
	FindByID(id string) (*domain.Account, error)
}

type Dependencies struct {
	Store             eventstore.Store
	AccountRepository accountReader
	TransferService   domain.TransferService
	DepositService    domain.DepositService
	WithdrawService   domain.WithdrawService
	OnboardingService domain.OnboardingService
	AuthService       domain.AuthService
	ReviewService     domain.ReviewService
	AdjustmentService domain.AdjustmentService
	EventBusStore     domain.EventBusStore
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
	banking := protect
	admin := protect
	if deps.AuthService != nil {
		banking = func(h http.HandlerFunc) http.HandlerFunc {
			return requireAuth(deps.AuthService, requireApprovedCustomer(h))
		}
		admin = func(h http.HandlerFunc) http.HandlerFunc { return requireAuth(deps.AuthService, requireAdmin(h)) }
	}
	router.HandleFunc("/transfer", banking(handleTransfer(deps.TransferService))).Methods(http.MethodPost)
	router.HandleFunc("/transfers/{id}", banking(handleTransferStatus(deps.TransferService))).Methods(http.MethodGet)
	router.HandleFunc("/deposit", banking(handleDeposit(deps.DepositService))).Methods(http.MethodPost)
	router.HandleFunc("/withdraw", banking(handleWithdraw(deps.WithdrawService))).Methods(http.MethodPost)
	router.HandleFunc("/accounts", banking(handleListAccounts(deps.Store, deps.AccountRepository))).Methods(http.MethodGet)
	router.HandleFunc("/accounts/{id}/events", banking(handleAccountEvents(deps.Store))).Methods(http.MethodGet)
	router.HandleFunc("/admin/accounts/{id}/events", admin(handleAdminAccountEvents(deps.Store, deps.AccountRepository))).Methods(http.MethodGet)
	if deps.ReviewService != nil {
		router.HandleFunc("/admin/applications", admin(handleApplications(deps.ReviewService))).Methods(http.MethodGet)
		router.HandleFunc("/admin/applications/{id}", admin(handleApplication(deps.ReviewService))).Methods(http.MethodGet)
		router.HandleFunc("/admin/applications/{id}/passport", admin(handlePassport(deps.ReviewService))).Methods(http.MethodGet)
		router.HandleFunc("/admin/applications/{id}/approve", admin(handleApprove(deps.ReviewService))).Methods(http.MethodPost)
		router.HandleFunc("/admin/applications/{id}/reject", admin(handleReject(deps.ReviewService))).Methods(http.MethodPost)
	}
	if deps.AdjustmentService != nil {
		router.HandleFunc("/admin/adjustments", admin(handleCreateAdjustment(deps.AdjustmentService))).Methods(http.MethodPost)
		router.HandleFunc("/admin/adjustments", admin(handleListAdjustments(deps.AdjustmentService))).Methods(http.MethodGet)
		router.HandleFunc("/admin/adjustments/{id}", admin(handleGetAdjustment(deps.AdjustmentService))).Methods(http.MethodGet)
		router.HandleFunc("/admin/adjustments/{id}/approve", admin(handleApproveAdjustment(deps.AdjustmentService))).Methods(http.MethodPost)
		router.HandleFunc("/admin/adjustments/{id}/reject", admin(handleRejectAdjustment(deps.AdjustmentService))).Methods(http.MethodPost)
	}
	if deps.EventBusStore != nil {
		router.HandleFunc("/admin/event-bus", admin(handleListEvents(deps.EventBusStore))).Methods(http.MethodGet)
		router.HandleFunc("/admin/event-bus/{id}/requeue", admin(handleRequeueEvent(deps.EventBusStore))).Methods(http.MethodPost)
	}
	return withCORS(router, deps.CORSOrigins)
}

type transferRequest struct {
	Amount   domain.Money `json:"amount"`
	SourceID string       `json:"source_account_id"`
	DestID   string       `json:"destination_account_id"`
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

		receipt, err := transferService.Transfer(domain.TransferCommand{Amount: req.Amount, SourceAccountID: req.SourceID, DestinationAccountID: req.DestID, IdempotencyKey: r.Header.Get("Idempotency-Key")})
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		status := http.StatusAccepted
		if receipt.Status != domain.TransferPending {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{
			"TransferID":           receipt.TransferID,
			"Status":               receipt.Status,
			"InitialSourceAccount": receipt.InitialSourceAccount,
			"FinalSourceAccount":   receipt.FinalSourceAccount,
			"DestinationAccountID": req.DestID,
			"TransferAmount":       receipt.TransferAmount,
			"FeeAmount":            receipt.FeeAmount,
			"CurrentStep":          receipt.CurrentStep,
		})
	}
}

func handleTransferStatus(service domain.TransferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principalFromRequest(r)
		source := ""
		if p != nil {
			source = p.AccountID
		}
		record, err := service.Find(mux.Vars(r)["id"], source)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	}
}

type depositRequest struct {
	Amount    domain.Money `json:"amount"`
	AccountID string       `json:"account_id"`
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
	Amount    domain.Money `json:"amount"`
	AccountID string       `json:"account_id"`
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
	LegalFirstName     string       `json:"legal_first_name"`
	LegalLastName      string       `json:"legal_last_name"`
	DateOfBirth        string       `json:"date_of_birth"`
	Nationality        string       `json:"nationality"`
	Email              string       `json:"email"`
	Phone              string       `json:"phone"`
	Password           string       `json:"password"`
	InitialDeposit     domain.Money `json:"initial_deposit"`
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

func handleListAccounts(store eventstore.Store, repo accountReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if principal := principalFromRequest(r); principal != nil {
			events, err := store.Load(principal.AccountID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			a := balanceAccount(repo, principal.AccountID, events)
			writeJSON(w, http.StatusOK, map[string]any{"accounts": []accountDTO{{ID: principal.AccountID, Balance: a.Balance, ReservedBalance: a.ReservedBalance, AvailableBalance: a.AvailableBalance, EventCount: len(events)}}})
			return
		}
		accounts, err := listAccounts(store, repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
	}
}

// balanceAccount derives current balances from the repository (snapshot-aware when
// present), falling back to a full replay of the loaded events otherwise. The
// caller still owns event_count from the full stream.
func balanceAccount(repo accountReader, id string, events []domain.Event) *domain.Account {
	if repo != nil {
		if a, err := repo.FindByID(id); err == nil {
			return a
		}
	}
	return domain.ReplayAccount(id, events)
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

func handleAdminAccountEvents(store eventstore.Store, repo accountReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(mux.Vars(r)["id"])
		if id == "" {
			writeError(w, http.StatusBadRequest, "account id is required")
			return
		}
		events, err := store.Load(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(events) == 0 {
			writeError(w, http.StatusNotFound, domain.ErrAccountNotFound.Error())
			return
		}
		account := balanceAccount(repo, id, events)
		writeJSON(w, http.StatusOK, map[string]any{"aggregate_id": id, "balance": account.Balance, "event_count": len(events), "events": toEventDTOs(events)})
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

func requireApprovedCustomer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principalFromRequest(r)
		if p == nil || p.Role != "customer" || p.KYCStatus != domain.KYCApproved || p.AccountID == "" {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}
		next(w, r)
	}
}
func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principalFromRequest(r)
		if p == nil || p.Role != "admin" {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}
		next(w, r)
	}
}

func handleApplications(s domain.ReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := s.List(r.URL.Query().Get("status"))
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"applications": items})
	}
}
func handleApplication(s domain.ReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Get(mux.Vars(r)["id"])
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
func handlePassport(s domain.ReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, mime, err := s.Passport(mux.Vars(r)["id"])
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
func handleApprove(s domain.ReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.Approve(mux.Vars(r)["id"], principalFromRequest(r).AdminID); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func handleReject(s domain.ReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.Reject(mux.Vars(r)["id"], principalFromRequest(r).AdminID, req.Reason); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleCreateAdjustment(s domain.AdjustmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request domain.AdjustmentRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		created, err := s.Create(request, principalFromRequest(r).AdminID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	}
}
func handleListAdjustments(s domain.AdjustmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := s.List(r.URL.Query().Get("status"))
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"adjustments": items})
	}
}
func handleGetAdjustment(s domain.AdjustmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Get(mux.Vars(r)["id"])
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
func handleApproveAdjustment(s domain.AdjustmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.Approve(mux.Vars(r)["id"], principalFromRequest(r).AdminID); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func handleRejectAdjustment(s domain.AdjustmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.Reject(mux.Vars(r)["id"], principalFromRequest(r).AdminID, request.Reason); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleListEvents returns outbox rows for the durable event bus. The status query
// param filters ("", "scheduled", "published", "dead_lettered").
func handleListEvents(store domain.EventBusStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListIntegrationEvents(r.URL.Query().Get("status"), 0)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

func handleRequeueEvent(store domain.EventBusStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid event id")
			return
		}
		if err := store.RequeueIntegrationEvent(id, time.Now().UTC()); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
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
	case errors.Is(err, domain.ErrApplicationNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrApplicationReviewed):
		return http.StatusConflict
	case errors.Is(err, domain.ErrInvalidReviewStatus), errors.Is(err, domain.ErrInvalidRejectionReason):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyKeyRequired), errors.Is(err, domain.ErrInvalidMoney):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrInvalidAdjustment):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrAdjustmentNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrAdjustmentReviewed), errors.Is(err, domain.ErrSelfApproval), errors.Is(err, domain.ErrAlreadyReversed):
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
	ID               string       `json:"id"`
	Balance          domain.Money `json:"balance"`
	ReservedBalance  domain.Money `json:"reserved_balance"`
	AvailableBalance domain.Money `json:"available_balance"`
	EventCount       int          `json:"event_count"`
}

func listAccounts(store eventstore.Store, repo accountReader) ([]accountDTO, error) {
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
		a := balanceAccount(repo, id, events)
		out = append(out, accountDTO{ID: id, Balance: a.Balance, ReservedBalance: a.ReservedBalance, AvailableBalance: a.AvailableBalance, EventCount: len(events)})
	}
	return out, nil
}

type eventDTO struct {
	Seq                   int          `json:"seq"`
	Type                  string       `json:"type"`
	Amount                domain.Money `json:"amount,omitempty"`
	TransferID            string       `json:"transfer_id,omitempty"`
	Purpose               string       `json:"purpose,omitempty"`
	CounterpartyAccountID string       `json:"counterparty_account_id,omitempty"`
	AdjustmentID          string       `json:"adjustment_id,omitempty"`
	AdjustmentReason      string       `json:"adjustment_reason,omitempty"`
	CaseReference         string       `json:"case_reference,omitempty"`
	OriginalReference     string       `json:"original_reference,omitempty"`
	OccurredAt            string       `json:"occurred_at"`
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
			dto.TransferID, dto.Purpose, dto.CounterpartyAccountID = typedEvent.TransferID, typedEvent.Purpose, typedEvent.CounterpartyAccountID
			dto.AdjustmentID, dto.AdjustmentReason, dto.CaseReference, dto.OriginalReference = typedEvent.AdjustmentID, typedEvent.AdjustmentReason, typedEvent.CaseReference, typedEvent.OriginalReference
		case domain.MoneyCredited:
			dto.Amount = typedEvent.Amount
			dto.TransferID, dto.Purpose, dto.CounterpartyAccountID = typedEvent.TransferID, typedEvent.Purpose, typedEvent.CounterpartyAccountID
			dto.AdjustmentID, dto.AdjustmentReason, dto.CaseReference, dto.OriginalReference = typedEvent.AdjustmentID, typedEvent.AdjustmentReason, typedEvent.CaseReference, typedEvent.OriginalReference
		case domain.FundsReserved:
			dto.Amount, dto.TransferID, dto.Purpose = typedEvent.Amount, typedEvent.TransferID, "reservation"
		case domain.ReservationCaptured:
			dto.Amount, dto.TransferID, dto.Purpose = typedEvent.Amount, typedEvent.TransferID, "reservation_captured"
		case domain.ReservationReleased:
			dto.Amount, dto.TransferID, dto.Purpose = typedEvent.Amount, typedEvent.TransferID, "reservation_released"
		}
		out = append(out, dto)
	}
	return out
}
