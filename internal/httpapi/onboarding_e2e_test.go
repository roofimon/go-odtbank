package httpapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/httpapi"
	"go-odtbank/internal/service"
)

func TestOnboardingEndToEnd(t *testing.T) {
	store := eventstore.NewMemoryStore()
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store:             store,
		OnboardingService: service.NewOnboardingService(store),
	}))
	t.Cleanup(server.Close)

	payload := map[string]any{
		"legal_first_name": "Ada", "legal_last_name": "Lovelace",
		"date_of_birth": "1990-12-10", "nationality": "GB",
		"email": "ada@example.com", "phone": "+66812345678",
		"password":        "correct-horse-battery-staple",
		"initial_deposit": 25,
		"residential_address": map[string]any{
			"line1": "1 Computing Lane", "city": "Bangkok",
			"postal_code": "10110", "country": "TH",
		},
		"government_document": map[string]any{
			"type": "passport", "number": "P123456", "issuing_country": "GB",
		},
	}
	response := postOnboarding(t, server, payload)
	var receipt domain.OnboardingReceipt
	decodeResponse(t, response, http.StatusCreated, &receipt)
	if receipt.CustomerID == "" || receipt.KYCStatus != domain.KYCWaiting {
		t.Fatalf("receipt = %+v", receipt)
	}

	duplicateResponse := postOnboarding(t, server, payload)
	var duplicateError map[string]string
	decodeResponse(t, duplicateResponse, http.StatusConflict, &duplicateError)
	if duplicateError["error"] != domain.ErrCustomerAlreadyExists.Error() {
		t.Fatalf("duplicate error = %+v", duplicateError)
	}
	ids, _ := store.ListAggregates()
	if len(ids) != 0 {
		t.Fatalf("aggregate count = %d, want 0", len(ids))
	}
}

func postOnboarding(t *testing.T, server *httptest.Server, payload map[string]any) *http.Response {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode onboarding request: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("payload", string(payloadJSON)); err != nil {
		t.Fatalf("write onboarding payload: %v", err)
	}
	image, err := writer.CreateFormFile("passport_image", "passport.png")
	if err != nil {
		t.Fatalf("create passport image part: %v", err)
	}
	if _, err := image.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}); err != nil {
		t.Fatalf("write passport image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart request: %v", err)
	}
	response, err := server.Client().Post(server.URL+"/onboarding", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("POST /onboarding: %v", err)
	}
	return response
}
