package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/api"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/domain"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/domain/mocks"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/gatewayerrors"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/handlers"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/repository"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGetPaymentHandler(t *testing.T) {
	savedPayment := models.PostPaymentResponse{
		Id:                 "test-id",
		PaymentStatus:      "test-successful-status",
		CardNumberLastFour: 1234,
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}
	ps := repository.NewPaymentsRepository()
	ps.AddPayment(savedPayment)

	expectedPayment := models.GetPaymentHandlerResponse{
		Id:                 "test-id",
		Status:             "test-successful-status",
		LastFourCardDigits: 1234,
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}

	payments := handlers.NewPaymentsHandler(ps, nil)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	t.Run("PaymentFound", func(t *testing.T) {
		// Create a new HTTP request for testing
		req, err := http.NewRequest("GET", "/api/payments/test-id", nil)
		require.NoError(t, err)

		// Create a new HTTP request recorder for recording the response
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		// Check the body is not nil
		require.NotNil(t, w.Body)

		var response models.GetPaymentHandlerResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)

		// Check the response body is what we expect
		assert.Equal(t, expectedPayment, response)
		assert.Equal(t, http.StatusOK, w.Code)
	})
	t.Run("PaymentNotFound", func(t *testing.T) {
		// Create a new HTTP request for testing with a non-existing payment ID
		req, err := http.NewRequest("GET", "/api/payments/NonExistingID", nil)
		require.NoError(t, err)

		// Create a new HTTP request recorder for recording the response
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		// Check the HTTP status code in the response
		assert.Equal(t, w.Code, http.StatusNotFound)
	})
}

func TestPostPaymentHandler(t *testing.T) {
	expectedPayment := models.PostPaymentResponse{
		Id:                 "test-id",
		PaymentStatus:      "test-successful-status",
		CardNumberLastFour: 1234,
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}
	ps := repository.NewPaymentsRepository()
	ps.AddPayment(expectedPayment)
	ctrl := gomock.NewController(t)
	mockPaymentService := mocks.NewMockPaymentService(ctrl)
	defer ctrl.Finish()

	mockDomain := &domain.Domain{
		PaymentService: mockPaymentService,
	}

	payments := handlers.NewPaymentsHandler(ps, mockDomain)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	// Arrange
	postPayment := &models.PostPaymentHandlerRequest{
		CardNumber:  2222405343248877,
		ExpiryMonth: 4,
		ExpiryYear:  2025,
		Currency:    "GBP",
		Amount:      100,
		Cvv:         123,
	}

	body, err := json.Marshal(postPayment)
	require.NoError(t, err)

	postPaymentResponseID := uuid.New().String()
	mockDomain.PaymentService.(*mocks.MockPaymentService).EXPECT().PostPayment(postPayment).Return(&models.PostPaymentResponse{
		Id:                 postPaymentResponseID,
		PaymentStatus:      "authorized",
		CardNumberLastFour: 8877,
		ExpiryMonth:        4,
		ExpiryYear:         2025,
		Currency:           "GBP",
		Amount:             100,
	}, nil)

	// Act
	req, err := http.NewRequest("POST", "/api/payments", bytes.NewBuffer(body))
	require.NoError(t, err)

	// Create a new HTTP request recorder for recording the response
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Check the HTTP status code in the response
	assert.Equal(t, http.StatusOK, w.Code)

	var response models.PostPaymentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	lastFourCharacters, err := strconv.Atoi(getLastFourCharacters(t, postPayment.CardNumber))
	require.NoError(t, err)

	// Assert
	assert.Equal(t, postPaymentResponseID, response.Id)
	assert.Equal(t, lastFourCharacters, response.CardNumberLastFour)
	assert.Equal(t, postPayment.ExpiryMonth, response.ExpiryMonth)
	assert.Equal(t, postPayment.ExpiryYear, response.ExpiryYear)
	assert.Equal(t, postPayment.Currency, response.Currency)
	assert.Equal(t, postPayment.Amount, response.Amount)
	assert.Equal(t, "authorized", response.PaymentStatus)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

}

func TestPostPaymentHandler_NoBody(t *testing.T) {

	payments := handlers.NewPaymentsHandler(nil, nil)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	// Create a new HTTP request for testing with a non-existing payment ID
	req, err := http.NewRequest("POST", "/api/payments", nil)
	require.NoError(t, err)

	// Create a new HTTP request recorder for recording the response
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Check the HTTP status code in the response
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostPaymentHandler_InvalidJson(t *testing.T) {

	payments := handlers.NewPaymentsHandler(nil, nil)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	// Create a new HTTP request for testing with a non-existing payment ID
	req, err := http.NewRequest("POST", "/api/payments", bytes.NewBuffer([]byte("invalid json")))
	require.NoError(t, err)

	// Create a new HTTP request recorder for recording the response
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Check the HTTP status code in the response
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBankError_DomainError(t *testing.T) {

	expectedPayment := models.PostPaymentResponse{
		Id:                 "test-id",
		PaymentStatus:      "test-successful-status",
		CardNumberLastFour: 1234,
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}
	ps := repository.NewPaymentsRepository()
	ps.AddPayment(expectedPayment)
	ctrl := gomock.NewController(t)
	mockPaymentService := mocks.NewMockPaymentService(ctrl)
	defer ctrl.Finish()

	mockDomain := &domain.Domain{
		PaymentService: mockPaymentService,
	}

	payments := handlers.NewPaymentsHandler(ps, mockDomain)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	// Arrange
	postPayment := &models.PostPaymentHandlerRequest{
		CardNumber:  2222405343248877,
		ExpiryMonth: 4,
		ExpiryYear:  2025,
		Currency:    "GBP",
		Amount:      100,
		Cvv:         123,
	}

	body, err := json.Marshal(postPayment)
	require.NoError(t, err)

	mockDomain.PaymentService.(*mocks.MockPaymentService).EXPECT().PostPayment(postPayment).Return(nil, errors.New("boom"))

	// Act
	req, err := http.NewRequest("POST", "/api/payments", bytes.NewBuffer(body))
	require.NoError(t, err)

	// Create a new HTTP request recorder for recording the response
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestBankError_ServiceUnavailable(t *testing.T) {

	expectedPayment := models.PostPaymentResponse{
		Id:                 "test-id",
		PaymentStatus:      "test-successful-status",
		CardNumberLastFour: 1234,
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}
	ps := repository.NewPaymentsRepository()
	ps.AddPayment(expectedPayment)
	ctrl := gomock.NewController(t)
	mockPaymentService := mocks.NewMockPaymentService(ctrl)
	defer ctrl.Finish()

	mockDomain := &domain.Domain{
		PaymentService: mockPaymentService,
	}

	payments := handlers.NewPaymentsHandler(ps, mockDomain)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())
	r.Post("/api/payments", payments.PostHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	// Arrange
	postPayment := &models.PostPaymentHandlerRequest{
		CardNumber:  2222405343248877,
		ExpiryMonth: 4,
		ExpiryYear:  2025,
		Currency:    "GBP",
		Amount:      100,
		Cvv:         123,
	}

	body, err := json.Marshal(postPayment)
	require.NoError(t, err)

	mockedError := gatewayerrors.NewBankError(
		errors.New("acquiring bank unavailble"),
		http.StatusServiceUnavailable,
	)
	mockDomain.PaymentService.(*mocks.MockPaymentService).EXPECT().PostPayment(postPayment).Return(nil, mockedError)

	// Act
	req, err := http.NewRequest("POST", "/api/payments", bytes.NewBuffer(body))
	require.NoError(t, err)

	// Create a new HTTP request recorder for recording the response
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	var response handlers.HandlerErrorResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, "The acquiring bank is currently unavailable. Please try again later.", response.Message)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestPostPaymentHandler_Integration(t *testing.T) {
	// Start Mountebank container
	ctx, cli, containerID := startMountebankContainer(t)
	defer stopMountebankContainer(ctx, cli, containerID)

	api := api.New()

	go func() {
		api.Run(ctx, ":8090")
	}()

	// Create the payment request
	postPayment := &models.PostPaymentHandlerRequest{
		CardNumber:  2222405343248877,
		ExpiryMonth: 4,
		ExpiryYear:  2025,
		Currency:    "GBP",
		Amount:      100,
		Cvv:         123,
	}

	body, err := json.Marshal(postPayment)
	require.NoError(t, err)

	// Create a new HTTP request for testing
	req, err := http.NewRequest("POST", "http://localhost:8090/api/payments", bytes.NewBuffer(body))
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	// Check the HTTP status code in the response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Decode the response body
	var response models.PostPaymentResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	reqGet, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:8090/api/payments/%s", response.Id), bytes.NewBuffer(body))
	require.NoError(t, err)

	respGet, err := http.DefaultClient.Do(reqGet)
	require.NoError(t, err)

	var getHandlerResponse models.GetPaymentHandlerResponse
	err = json.NewDecoder(respGet.Body).Decode(&getHandlerResponse)
	require.NoError(t, err)

	assert.Equal(t, getHandlerResponse.Id, response.Id)
	assert.Equal(t, "authorized", response.PaymentStatus)
	assert.Equal(t, 8877, response.CardNumberLastFour)
	assert.Equal(t, 4, response.ExpiryMonth)
	assert.Equal(t, 2025, response.ExpiryYear)
	assert.Equal(t, "GBP", response.Currency)
	assert.Equal(t, 100, response.Amount)
}

func getLastFourCharacters(t *testing.T, i int) string {
	t.Helper()

	s := strconv.Itoa(i)
	require.Equal(t, 16, len(s))
	return s[len(s)-4:]
}

func startMountebankContainer(t *testing.T) (context.Context, *client.Client, string) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "bbyars/mountebank:2.8.1",
		ExposedPorts: map[nat.Port]struct{}{
			"8085/tcp": {},
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"8080/tcp": []nat.PortBinding{
				{
					HostPort: "8080",
				},
			},
		},
	}, nil, nil, "")
	require.NoError(t, err)

	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Wait for Mountebank to be ready
	time.Sleep(5 * time.Second)

	return ctx, cli, resp.ID
}

func stopMountebankContainer(ctx context.Context, cli *client.Client, containerID string) {
	cli.ContainerStop(ctx, containerID, container.StopOptions{})
	cli.ContainerRemove(ctx, containerID, container.RemoveOptions{})
}
