package openai

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Models (GET /models)
// ---------------------------------------------------------------------------

func TestModels_ListSortedUnique(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"m-b"},{"id":"m-a"},{"id":""},{"id":"m-b"},{"id":"m-c","extra":1}]}`)
	})
	got, err := c.Models(t.Context())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if want := []string{"m-a", "m-b", "m-c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestModels_EmptyList(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	got, err := c.Models(t.Context())
	if err != nil || len(got) != 0 {
		t.Errorf("got %q, %v; want empty, nil", got, err)
	}
}

func TestModels_ServerError(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "no such route")
	})
	_, err := c.Models(t.Context())
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no such route") {
		t.Errorf("err = %v, want the status and the body", err)
	}
}

func TestModels_BadJSON(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": [oops`)
	})
	if _, err := c.Models(t.Context()); err == nil || !strings.Contains(err.Error(), "model list") {
		t.Errorf("err = %v, want a model list error", err)
	}
}

func TestModels_Unreachable(t *testing.T) {
	c := New("http://127.0.0.1:1/v1", "", nil)
	if _, err := c.Models(t.Context()); err == nil || !strings.Contains(err.Error(), "cannot reach the model server") {
		t.Errorf("err = %v", err)
	}
}

func TestModels_SendsAPIKey(t *testing.T) {
	c := rawClient(t, "secret", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"x"}]}`)
	})
	if _, err := c.Models(t.Context()); err != nil {
		t.Fatal(err)
	}
}
