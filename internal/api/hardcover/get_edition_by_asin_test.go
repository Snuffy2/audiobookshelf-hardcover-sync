package hardcover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/cache"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestClient is retained for compatibility with the existing GetEdition test;
// GetEditionByASIN tests use Client directly so calls cannot bypass production code.
type TestClient struct {
	*Client
	mockEditions map[string]*models.Edition
}

func TestClient_GetEditionByASIN(t *testing.T) {
	tests := []struct {
		name            string
		asin            string
		searchResponse  map[string]interface{}
		expectedError   string
		expectedEdition *models.Edition
	}{
		{
			name: "successful retrieval",
			asin: "B01234567",
			searchResponse: map[string]interface{}{
				"data": map[string]interface{}{
					"books": []map[string]interface{}{
						{
							"id":    123,
							"title": "Test Book",
							"editions": []map[string]interface{}{
								{
									"id": 789,
								},
							},
						},
					},
				},
			},
			expectedEdition: &models.Edition{
				ID:          "789",
				BookID:      "123",
				Title:       "Test Edition",
				ISBN10:      "1234567890",
				ISBN13:      "9781234567897",
				ASIN:        "B01234567",
				ReleaseDate: "2023-01-01",
			},
		},
		{
			name: "book not found",
			asin: "B09999999",
			searchResponse: map[string]interface{}{
				"data": map[string]interface{}{
					"books": []map[string]interface{}{},
				},
			},
			expectedError: "no book found with ASIN",
		},
		{
			name: "search error",
			asin: "B08888888",
			searchResponse: map[string]interface{}{
				"data": nil,
				"errors": []map[string]interface{}{
					{
						"message": "GraphQL error: search failed",
					},
				},
			},
			expectedError: "failed to find book by ASIN",
		},
		{
			name: "malformed search response: missing editions",
			asin: "B07777777",
			searchResponse: map[string]interface{}{
				"data": map[string]interface{}{
					"books": []map[string]interface{}{
						{
							"id":    789,
							"title": "Another Book",
							"authors": []map[string]interface{}{
								{
									"name": "Another Author",
								},
							},
						},
					},
				},
			},
			expectedError: "invalid ASIN response: book 789 is missing editions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var requestBody struct {
					Query string `json:"query"`
				}

				if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}

				var response interface{}
				if strings.Contains(requestBody.Query, "BookByASIN") {
					response = tt.searchResponse
				} else if strings.Contains(requestBody.Query, "GetEdition") {
					response = map[string]interface{}{
						"data": map[string]interface{}{
							"editions": []map[string]interface{}{
								{
									"id":           789,
									"book_id":      123,
									"title":        "Test Edition",
									"isbn_10":      "1234567890",
									"isbn_13":      "9781234567897",
									"asin":         "B01234567",
									"release_date": "2023-01-01",
								},
							},
						},
					}
				} else {
					t.Fatalf("unexpected GraphQL query: %s", requestBody.Query)
				}

				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Fatalf("failed to encode mock response: %v", err)
				}
			}))
			defer server.Close()

			client := CreateTestClient(server)
			client.editionCache = cache.NewMemoryCache[int, *models.Edition](client.logger)

			edition, err := client.GetEditionByASIN(context.Background(), tt.asin)

			// Check for expected errors
			if tt.expectedError != "" {
				assert.Error(t, err)
				if err != nil {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
				return
			}

			// Check for unexpected errors
			assert.NoError(t, err)
			assert.NotNil(t, edition)

			// Verify the edition data
			if tt.expectedEdition != nil {
				assert.Equal(t, tt.expectedEdition.ID, edition.ID)
				assert.Equal(t, tt.expectedEdition.BookID, edition.BookID)
				assert.Equal(t, tt.expectedEdition.Title, edition.Title)
				assert.Equal(t, tt.expectedEdition.ISBN10, edition.ISBN10)
				assert.Equal(t, tt.expectedEdition.ISBN13, edition.ISBN13)
				assert.Equal(t, tt.expectedEdition.ASIN, edition.ASIN)
				assert.Equal(t, tt.expectedEdition.ReleaseDate, edition.ReleaseDate)
			}
		})
	}
}
