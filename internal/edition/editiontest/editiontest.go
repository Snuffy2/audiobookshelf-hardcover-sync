// Package editiontest provides Audiobookshelf and Hardcover HTTP fakes for
// tests of the edition draft and create flows. It imports no project package
// and is only meant to be imported by tests.
package editiontest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ExistingEdition is an edition already on Hardcover, found by ASIN or ISBN.
type ExistingEdition struct {
	EditionID     int
	BookID        int
	ReadingFormat int
}

func (e ExistingEdition) inFormat(variables map[string]interface{}) bool {
	format := e.ReadingFormat
	if format == 0 {
		format = 2
	}
	requested, _ := variables["format_id"].(float64)
	return format == int(requested)
}

// HardcoverFake stands in for the Hardcover GraphQL endpoint for draft and
// create tests.
type HardcoverFake struct {
	*httptest.Server

	Authors      map[string]int
	Publishers   map[string]int
	ASINs        map[string]ExistingEdition
	ISBNs        map[string]ExistingEdition
	FailWith     string
	ProbeStatus  int
	ProbeBody    string
	InsertStatus int
	InsertBody   string
	SearchStatus int
	SearchBody   string

	Entered       chan struct{}
	ReadEntered   chan struct{}
	SearchEntered chan struct{}
	ProbeEntered  chan struct{}

	mu          sync.Mutex
	mutations   []map[string]interface{}
	writes      []string
	requests    int
	probes      []map[string]interface{}
	holdInsert  chan struct{}
	holdReads   chan struct{}
	holdSearch  chan struct{}
	holdProbe   chan struct{}
	delayAll    time.Duration
	delayInsert time.Duration
}

// HardcoverRequestCounter remains the draft tests' descriptive name while
// sharing the richer fake needed by create tests.
type HardcoverRequestCounter = HardcoverFake

// NewHardcoverFake starts a Hardcover fake closed with the test.
func NewHardcoverFake(t *testing.T) *HardcoverFake {
	t.Helper()
	fake := &HardcoverFake{
		Authors:       map[string]int{},
		Publishers:    map[string]int{},
		ASINs:         map[string]ExistingEdition{},
		ISBNs:         map[string]ExistingEdition{},
		Entered:       make(chan struct{}, 8),
		ReadEntered:   make(chan struct{}, 64),
		SearchEntered: make(chan struct{}, 8),
		ProbeEntered:  make(chan struct{}, 8),
	}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

// NewHardcoverRequestCounter starts the shared fake closed with the test.
func NewHardcoverRequestCounter(t *testing.T) *HardcoverRequestCounter {
	return NewHardcoverFake(t)
}

// RecordedMutations returns variables from every insert_edition request.
func (f *HardcoverFake) RecordedMutations() []map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]interface{}(nil), f.mutations...)
}

// RecordedWrites returns every mutation document received.
func (f *HardcoverFake) RecordedWrites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

// RequestCount returns the number of requests received, of any kind.
func (f *HardcoverFake) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// ProbeCount returns the number of impossible-book edition capability probes.
func (f *HardcoverFake) ProbeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.probes)
}

// ProbeBookIDs returns book ids sent to the capability mutation.
func (f *HardcoverFake) ProbeBookIDs() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]float64, 0, len(f.probes))
	for _, variables := range f.probes {
		id, _ := variables["bookId"].(float64)
		ids = append(ids, id)
	}
	return ids
}

func (f *HardcoverFake) HoldInsert(ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdInsert = ch
}

func (f *HardcoverFake) HoldReads(ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdReads = ch
}

func (f *HardcoverFake) HoldSearches(ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdSearch = ch
}

func (f *HardcoverFake) HoldProbes(ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdProbe = ch
}

func (f *HardcoverFake) ReleaseHolds() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, hold := range []*chan struct{}{&f.holdInsert, &f.holdReads, &f.holdSearch, &f.holdProbe} {
		if *hold != nil {
			close(*hold)
			*hold = nil
		}
	}
}

func (f *HardcoverFake) SetDelays(all, insert time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delayAll, f.delayInsert = all, insert
}

func (f *HardcoverFake) serve(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	respond := func(data map[string]interface{}) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}

	f.mu.Lock()
	f.requests++
	delayAll, delayInsert := f.delayAll, f.delayInsert
	holdInsert, holdReads, holdSearch, holdProbe := f.holdInsert, f.holdReads, f.holdSearch, f.holdProbe
	isMutation := strings.HasPrefix(strings.TrimSpace(request.Query), "mutation")
	if isMutation {
		f.writes = append(f.writes, request.Query)
	}
	f.mu.Unlock()

	if !isMutation && holdReads != nil {
		f.ReadEntered <- struct{}{}
		select {
		case <-holdReads:
		case <-r.Context().Done():
			return
		}
	}
	time.Sleep(delayAll)

	switch {
	case strings.Contains(request.Query, "insert_edition"):
		bookID, _ := request.Variables["bookId"].(float64)
		if bookID == -1 {
			f.mu.Lock()
			f.probes = append(f.probes, request.Variables)
			probeStatus, probeBody := f.ProbeStatus, f.ProbeBody
			f.mu.Unlock()
			if holdProbe != nil {
				f.ProbeEntered <- struct{}{}
				select {
				case <-holdProbe:
				case <-r.Context().Done():
					return
				}
			}
			if probeStatus != 0 {
				w.WriteHeader(probeStatus)
				if probeBody != "" {
					_, _ = w.Write([]byte(probeBody))
				}
				return
			}
			respond(map[string]interface{}{"insert_edition": map[string]interface{}{"id": nil, "errors": []string{"Couldn't find Book"}}})
			return
		}
		f.mu.Lock()
		f.mutations = append(f.mutations, request.Variables)
		failWith, insertStatus, insertBody := f.FailWith, f.InsertStatus, f.InsertBody
		f.mu.Unlock()
		f.Entered <- struct{}{}
		if holdInsert != nil {
			<-holdInsert
		}
		time.Sleep(delayInsert)
		if insertStatus != 0 {
			w.WriteHeader(insertStatus)
			if insertBody != "" {
				_, _ = w.Write([]byte(insertBody))
			}
			return
		}
		if failWith != "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []map[string]string{{"message": failWith}}})
			return
		}
		respond(map[string]interface{}{"insert_edition": map[string]interface{}{"id": 777, "errors": []string{}}})
	case strings.Contains(request.Query, "insert_image"):
		respond(map[string]interface{}{"insert_image": map[string]interface{}{"id": 55}})
	case strings.Contains(request.Query, "update_edition"):
		respond(map[string]interface{}{"update_edition": map[string]interface{}{"id": 777, "errors": []string{}}})
	case strings.Contains(request.Query, "BookByISBN"):
		isbn, _ := request.Variables["isbn"].(string)
		respond(map[string]interface{}{"books": f.existingBooks(f.ISBNs[isbn], request.Variables, "isbn_13", isbn)})
	case strings.Contains(request.Query, "BookByASIN"):
		asin, _ := request.Variables["asin"].(string)
		respond(map[string]interface{}{"books": f.existingBooks(f.ASINs[asin], request.Variables, "asin", asin)})
	case strings.Contains(request.Query, "query GetEdition("):
		editionID, _ := request.Variables["editionId"].(float64)
		editions := []interface{}{}
		for asin, found := range f.ASINs {
			if float64(found.EditionID) == editionID {
				editions = append(editions, map[string]interface{}{"id": found.EditionID, "book_id": found.BookID, "asin": asin})
			}
		}
		respond(map[string]interface{}{"editions": editions})
	case strings.Contains(request.Query, "SearchPeopleDirect") || strings.Contains(request.Query, "SearchNarrators"):
		f.mu.Lock()
		searchStatus, searchBody := f.SearchStatus, f.SearchBody
		f.mu.Unlock()
		if searchStatus != 0 {
			w.WriteHeader(searchStatus)
			if searchBody != "" {
				_, _ = w.Write([]byte(searchBody))
			}
			return
		}
		name, _ := request.Variables["name"].(string)
		people := []map[string]interface{}{}
		if id, ok := f.Authors[name]; ok {
			people = append(people, map[string]interface{}{"id": id, "name": name, "books_count": 3})
		}
		respond(map[string]interface{}{"authors": people})
	case strings.Contains(request.Query, "SearchPublishers"):
		f.mu.Lock()
		searchStatus, searchBody := f.SearchStatus, f.SearchBody
		f.mu.Unlock()
		if searchStatus != 0 {
			w.WriteHeader(searchStatus)
			if searchBody != "" {
				_, _ = w.Write([]byte(searchBody))
			}
			return
		}
		name, _ := request.Variables["name"].(string)
		publishers := []map[string]interface{}{}
		if id, ok := f.Publishers[name]; ok {
			publishers = append(publishers, map[string]interface{}{"id": id, "name": name})
		}
		respond(map[string]interface{}{"publishers": publishers})
	default:
		if holdSearch != nil {
			f.SearchEntered <- struct{}{}
			select {
			case <-holdSearch:
			case <-r.Context().Done():
				return
			}
		}
		respond(map[string]interface{}{
			"search":     map[string]interface{}{"error": "", "results": map[string]interface{}{"hits": []interface{}{}}},
			"publishers": []interface{}{}, "editions": []interface{}{}, "books": []interface{}{},
		})
	}
}

func (f *HardcoverFake) existingBooks(found ExistingEdition, variables map[string]interface{}, field, value string) []interface{} {
	if found.EditionID == 0 || !found.inFormat(variables) {
		return []interface{}{}
	}
	return []interface{}{map[string]interface{}{
		"id": found.BookID, "title": "Existing",
		"editions": []interface{}{map[string]interface{}{"id": found.EditionID, field: value}},
	}}
}

// AudiobookshelfFake serves /api/items/{id} for a fixed set of items and the
// endpoints a full sync of a one-book library needs (that book is sync-book).
type AudiobookshelfFake struct {
	*httptest.Server
	// Status, when non-zero, makes every item request fail with that status.
	Status int
	// ReadEntered receives once for each item request held by HoldReads.
	ReadEntered chan struct{}

	mu        sync.Mutex
	holdReads chan struct{}
	itemReads int
}

// NewAudiobookshelfFake starts an Audiobookshelf fake closed with the test.
func NewAudiobookshelfFake(t *testing.T, items map[string]map[string]interface{}) *AudiobookshelfFake {
	t.Helper()
	fake := &AudiobookshelfFake{ReadEntered: make(chan struct{}, 64)}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/me":
			_, _ = w.Write([]byte("{\"mediaProgress\":[],\"listeningSessions\":[]}"))
		case r.URL.Path == "/api/libraries":
			_, _ = w.Write([]byte("{\"libraries\":[{\"id\":\"library\",\"name\":\"Library\"}]}"))
		case r.URL.Path == "/api/libraries/library/items":
			results := []map[string]interface{}{}
			if book, ok := items["sync-book"]; ok {
				results = append(results, book)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
		case strings.HasPrefix(r.URL.Path, "/api/items/"):
			fake.mu.Lock()
			fake.itemReads++
			fake.mu.Unlock()
			if !fake.waitForReadRelease(r) {
				return
			}
			if fake.Status != 0 {
				http.Error(w, "forced failure with abs-token detail", fake.Status)
				return
			}
			item, ok := items[strings.TrimPrefix(r.URL.Path, "/api/items/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(item)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// ItemRequestCount returns the number of expanded item fetches.
func (f *AudiobookshelfFake) ItemRequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.itemReads
}

// HoldReads makes item requests wait until ch is closed; nil stops holding.
func (f *AudiobookshelfFake) HoldReads(ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdReads = ch
}

// ReleaseReads closes and clears the current hold, if any.
func (f *AudiobookshelfFake) ReleaseReads() {
	f.mu.Lock()
	ch := f.holdReads
	f.holdReads = nil
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (f *AudiobookshelfFake) waitForReadRelease(r *http.Request) bool {
	f.mu.Lock()
	ch := f.holdReads
	f.mu.Unlock()
	if ch == nil {
		return true
	}
	select {
	case f.ReadEntered <- struct{}{}:
	default:
	}
	select {
	case <-ch:
		return true
	case <-r.Context().Done():
		return false
	}
}
