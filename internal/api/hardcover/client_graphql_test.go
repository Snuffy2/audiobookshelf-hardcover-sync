package hardcover

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchBookByASINUsesProductionQueryWithNumericASIN(t *testing.T) {
	type graphqlRequest struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}

	requestCh := make(chan graphqlRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestCh <- request

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
			"data": {
				"books": [{
					"id": 281093,
					"title": "Permanent Record",
					"book_status_id": 1,
					"canonical_id": null,
					"editions": [{
						"id": 30404119,
						"asin": "1250622689",
						"isbn_13": "9781250622686",
						"isbn_10": "1250622689",
						"reading_format_id": 2,
						"audio_seconds": 41472,
						"book_mappings": []
					}]
				}]
			}
		}`)); err != nil {
			t.Errorf("write ASIN response: %v", err)
		}
	}))
	defer server.Close()

	client := CreateTestClient(server)
	ctx := WithAudnexRegion(WithReadingFormat(context.Background(), "audiobook"), " Us ")
	book, err := client.SearchBookByASIN(ctx, "1250622689")

	require.NoError(t, err)
	require.NotNil(t, book)
	assert.Equal(t, "281093", book.ID)
	assert.Equal(t, "Permanent Record", book.Title)
	assert.Equal(t, "30404119", book.EditionID)
	assert.Equal(t, "1250622689", book.EditionASIN)
	assert.Equal(t, "9781250622686", book.EditionISBN13)

	request := <-requestCh
	assert.Contains(t, request.Query, "query BookByASIN($asin: String!, $asin_us: String!, $format_id: Int!)")
	assert.Contains(t, request.Query, "{ editions: { asin: { _eq: $asin }, reading_format: { id: { _eq: $format_id } } } }")
	assert.Equal(t, "1250622689", request.Variables["asin"])
	assert.Equal(t, "1250622689:us", request.Variables["asin_us"])
	assert.Equal(t, float64(2), request.Variables["format_id"])
}

func TestSearchBookByASINRejectsMalformedSuccessResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{
			name:     "missing books",
			response: `{"data":{}}`,
			wantErr:  true,
		},
		{
			name:     "books has unexpected type",
			response: `{"data":{"books":{}}}`,
			wantErr:  true,
		},
		{
			name:     "books contains malformed entry",
			response: `{"data":{"books":[null]}}`,
			wantErr:  true,
		},
		{
			name:     "book has no editions",
			response: `{"data":{"books":[{"id":1,"title":"Test Book","editions":[]}]}}`,
			wantErr:  true,
		},
		{
			name:     "editions contains malformed entry",
			response: `{"data":{"books":[{"id":1,"title":"Test Book","editions":[null]}]}}`,
			wantErr:  true,
		},
		{
			name:     "empty books array is a clean miss",
			response: `{"data":{"books":[]}}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := CreateTestClient(server)
			book, err := client.SearchBookByASIN(context.Background(), "ASIN123")

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, book)
			} else {
				require.NoError(t, err)
				assert.Nil(t, book)
			}
		})
	}
}

func TestGraphQLQuery_RetriesOn429ThenSucceeds(t *testing.T) {
	logger.Setup(logger.Config{Level: "debug", Format: "json"})
	log := logger.Get()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Throttled"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"books":[{"id":1}]}}`))
	}))
	defer server.Close()

	client := CreateTestClient(server)
	client.logger = log
	client.maxRetries = 3
	client.retryDelay = 1 * time.Millisecond
	client.rateLimiter = util.NewRateLimiter(time.Nanosecond, 100, log)

	var response struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}

	err := client.GraphQLQuery(context.Background(), `query RetryTest { books { id } }`, nil, &response)
	require.NoError(t, err)
	require.Len(t, response.Books, 1)
	assert.Equal(t, 3, int(atomic.LoadInt32(&attempts)))
	assert.Equal(t, 1, response.Books[0].ID)
}

func TestGraphQLQuery_FailsFastOn400(t *testing.T) {
	logger.Setup(logger.Config{Level: "debug", Format: "json"})
	log := logger.Get()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := CreateTestClient(server)
	client.logger = log
	client.maxRetries = 3
	client.retryDelay = 1 * time.Millisecond
	client.rateLimiter = util.NewRateLimiter(time.Nanosecond, 100, log)

	var response struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}

	err := client.GraphQLQuery(context.Background(), `query FailFastTest { books { id } }`, nil, &response)
	require.Error(t, err)
	assert.Equal(t, 1, int(atomic.LoadInt32(&attempts)))
	assert.Contains(t, err.Error(), "non-retryable HTTP error")
}

func TestGraphQLQuery_BoundsRetryAfterPause(t *testing.T) {
	logger.Setup(logger.Config{Level: "error", Format: "json"})
	log := logger.Get()

	previousMaxBackoff := util.DefaultMaxBackoff
	util.DefaultMaxBackoff = 25 * time.Millisecond
	t.Cleanup(func() { util.DefaultMaxBackoff = previousMaxBackoff })

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"books":[{"id":1}]}}`))
	}))
	defer server.Close()

	client := CreateTestClient(server)
	client.logger = log
	client.maxRetries = 1
	client.retryDelay = time.Millisecond
	client.rateLimiter = util.NewRateLimiter(time.Nanosecond, 1, log)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var response struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}

	err := client.GraphQLQuery(ctx, `query RetryAfterTest { books { id } }`, nil, &response)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
	assert.Equal(t, 1, response.Books[0].ID)
}

func TestGraphQLQuery_DailyResetOverridesRetryAfter(t *testing.T) {
	logger.Setup(logger.Config{Level: "error", Format: "json"})
	log := logger.Get()

	previousMaxBackoff := util.DefaultMaxBackoff
	util.DefaultMaxBackoff = 25 * time.Millisecond
	t.Cleanup(func() { util.DefaultMaxBackoff = previousMaxBackoff })

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("RateLimit", `"Free";r=59;t=1, "daily";r=0;t=1`)
			w.Header().Set("RateLimit-Policy", `"Free";q=60;w=60, "daily";q=5000;w=86400`)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"books":[{"id":1}]}}`))
	}))
	defer server.Close()

	client := CreateTestClient(server)
	client.logger = log
	client.maxRetries = 1
	client.retryDelay = time.Millisecond
	client.rateLimiter = util.NewRateLimiter(time.Nanosecond, 1, log)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var response struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}

	err := client.GraphQLQuery(ctx, `query DailyResetTest { books { id } }`, nil, &response)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}

func TestGraphQLQuery_MaxConcurrentLimitsActiveRequests(t *testing.T) {
	logger.Setup(logger.Config{Level: "error", Format: "json"})
	log := logger.Get()

	var active, maxActive int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		atomic.AddInt32(&active, -1)
		_, _ = w.Write([]byte(`{"data":{"books":[{"id":1}]}}`))
	}))
	defer server.Close()

	client := CreateTestClient(server)
	client.logger = log
	client.rateLimiter = util.NewRateLimiter(time.Nanosecond, 1, log)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var response1, response2 struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}
	errs := make(chan error, 2)
	go func() {
		errs <- client.GraphQLQuery(ctx, `query ConcurrentTest { books { id } }`, nil, &response1)
	}()
	go func() {
		errs <- client.GraphQLQuery(ctx, `query ConcurrentTest { books { id } }`, nil, &response2)
	}()

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first request did not reach the server")
	}
	select {
	case <-entered:
		t.Fatal("second request reached the server while the first was active")
	case <-time.After(50 * time.Millisecond):
	}

	release <- struct{}{}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("second request did not reach the server after the first completed")
	}
	release <- struct{}{}

	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	assert.Equal(t, int32(1), atomic.LoadInt32(&maxActive))
}

func TestGraphQLQuery_UsesConfiguredTimeoutAndReleasesPermit(t *testing.T) {
	logger.Setup(logger.Config{Level: "error", Format: "json"})
	log := logger.Get()

	var requests int32
	firstStarted := make(chan struct{})
	firstFinished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&requests, 1) {
		case 1:
			close(firstStarted)
			// Keep the handler stalled longer than the configured client timeout.
			// Do not rely on the server observing the client-side connection close.
			time.Sleep(200 * time.Millisecond)
			close(firstFinished)
		case 2:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"books":[{"id":1}]}}`)
		default:
			t.Errorf("unexpected request count: %d", atomic.LoadInt32(&requests))
		}
	}))
	defer server.Close()

	client := NewClientWithConfig(&ClientConfig{
		BaseURL:       server.URL,
		Timeout:       25 * time.Millisecond,
		MaxRetries:    0,
		RetryDelay:    time.Millisecond,
		RateLimit:     time.Nanosecond,
		MaxConcurrent: 1,
	}, "test-token", log)

	var firstResponse struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}
	firstErr := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		firstErr <- client.GraphQLQuery(context.Background(), `query TimeoutTest { books { id } }`, nil, &firstResponse)
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("stalled request did not reach the server")
	}

	select {
	case err := <-firstErr:
		require.Error(t, err)
		assert.Less(t, time.Since(startedAt), 150*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("configured HTTP timeout did not end the stalled request")
	}
	var secondResponse struct {
		Books []struct {
			ID int `json:"id"`
		} `json:"books"`
	}
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- client.GraphQLQuery(context.Background(), `query ReusePermit { books { id } }`, nil, &secondResponse)
	}()
	select {
	case err := <-secondErr:
		require.NoError(t, err)
		assert.Equal(t, 1, secondResponse.Books[0].ID)
	case <-time.After(time.Second):
		t.Fatal("rate-limiter permit was not released after the timed-out request")
	}
	select {
	case <-firstFinished:
	case <-time.After(time.Second):
		t.Fatal("stalled test handler did not finish")
	}
}
