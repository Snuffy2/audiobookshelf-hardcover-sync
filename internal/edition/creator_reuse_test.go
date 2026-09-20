package edition_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
)

// reuseClient is a Hardcover client whose lookups return canned editions and
// whose insert_edition reports a duplicate when insertErrors is set. It records
// every mutation. Embedding the interface leaves any other method nil, so an
// unexpected call fails loudly.
type reuseClient struct {
	edition.HardcoverClient

	byASIN, byISBN13 *models.Edition
	// asinAfterInsert hides byASIN until an insert_edition was attempted, so the
	// ASIN is found only by the lookup that follows a duplicate error.
	asinAfterInsert bool
	insertErrors    []string

	mu        sync.Mutex
	mutations []string
}

func (c *reuseClient) GetEditionByASIN(context.Context, string) (*models.Edition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byASIN == nil || (c.asinAfterInsert && len(c.mutations) == 0) {
		return nil, errors.New("not found")
	}
	return c.byASIN, nil
}

func (c *reuseClient) GetEditionByISBN13(context.Context, string) (*models.Edition, error) {
	if c.byISBN13 == nil {
		return nil, errors.New("not found")
	}
	return c.byISBN13, nil
}

func (c *reuseClient) GraphQLMutation(_ context.Context, mutation string, _ map[string]interface{}, result interface{}) error {
	c.mu.Lock()
	c.mutations = append(c.mutations, mutation)
	c.mu.Unlock()
	if !strings.Contains(mutation, "insert_edition") {
		return errors.New("unexpected mutation")
	}
	raw, err := json.Marshal(map[string]interface{}{"insert_edition": map[string]interface{}{"id": nil, "errors": c.insertErrors}})
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no network in this test")
}

// TestCreateEdition_ReusesOnlyAnEditionOfTheSameBook covers every path that
// adopts an existing edition. The input carries a cover, so any image step
// would show up as an extra mutation, an image error, or a missing Existing.
func TestCreateEdition_ReusesOnlyAnEditionOfTheSameBook(t *testing.T) {
	const bookID = 123
	sameBook := &models.Edition{ID: "555", BookID: "123"}
	otherBook := &models.Edition{ID: "555", BookID: "999"}
	duplicate := []string{"already exists"}
	tests := []struct {
		name         string
		asin, isbn13 string
		client       *reuseClient
		wantExisting bool
		wantConflict bool
		wantInserts  int // insert_edition attempts; no other mutation may ever be sent
	}{
		{name: "ASIN match on the same book", asin: "B0EXISTING1", client: &reuseClient{byASIN: sameBook}, wantExisting: true},
		{name: "ASIN match on another book", asin: "B0EXISTING1", client: &reuseClient{byASIN: otherBook}, wantConflict: true},
		{name: "ASIN match without a book ID", asin: "B0EXISTING1", client: &reuseClient{byASIN: &models.Edition{ID: "555"}}, wantConflict: true},
		{name: "ASIN match with an unknown book", asin: "B0EXISTING1", client: &reuseClient{byASIN: &models.Edition{ID: "555", BookID: "0"}}, wantConflict: true},
		{
			name: "duplicate reported, ISBN-13 match on the same book", isbn13: "9781234567890",
			client: &reuseClient{insertErrors: duplicate, byISBN13: sameBook}, wantExisting: true, wantInserts: 1,
		},
		{
			name: "duplicate reported, ISBN-13 match on another book", isbn13: "9781234567890",
			client: &reuseClient{insertErrors: duplicate, byISBN13: otherBook}, wantConflict: true, wantInserts: 1,
		},
		{
			name: "duplicate reported, ASIN match on the same book", asin: "B0EXISTING1",
			client: &reuseClient{insertErrors: duplicate, byASIN: sameBook, asinAfterInsert: true}, wantExisting: true, wantInserts: 1,
		},
		{
			name: "duplicate reported, ASIN match on another book", asin: "B0EXISTING1",
			client: &reuseClient{insertErrors: duplicate, byASIN: otherBook, asinAfterInsert: true}, wantConflict: true, wantInserts: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &edition.EditionInput{
				BookID: bookID, Title: "A Title", ASIN: tt.asin, ISBN13: tt.isbn13, AuthorIDs: []int{1},
				ImageURL: "https://audiobookshelf.example.test/api/items/x/cover",
			}
			creator := edition.NewCreatorWithHTTPClient(tt.client, logger.Get(), false, "token", &http.Client{Transport: failingTransport{}})

			result, err := creator.CreateEdition(context.Background(), input)

			switch {
			case tt.wantConflict:
				if !errors.Is(err, edition.ErrEditionBelongsToOtherBook) {
					t.Fatalf("CreateEdition() error = %v, want ErrEditionBelongsToOtherBook", err)
				}
			case err != nil:
				t.Fatalf("CreateEdition() error = %v", err)
			default:
				if result.EditionID != 555 || result.Existing != tt.wantExisting || result.ImageError != "" || result.ImageID != 0 {
					t.Errorf("CreateEdition() = %+v, want the untouched existing edition 555", result)
				}
			}
			if got := len(tt.client.mutations); got != tt.wantInserts {
				t.Errorf("mutations sent = %d (%v), want only %d insert_edition", got, tt.client.mutations, tt.wantInserts)
			}
		})
	}
}
