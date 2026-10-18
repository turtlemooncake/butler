package crawler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>hi</html>")
	}))
	defer srv.Close()

	page, err := fetch(srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(page.Body) != 15 {
		t.Errorf("body size = %d, want 15", len(page.Body))
	}
}
