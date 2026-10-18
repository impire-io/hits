// Package embedtest is an OpenAI-API-compatible embedding endpoint for
// tests: deterministic bag-of-words vectors, so similar texts get similar
// vectors with no external call. chronicle's semantic kind and the 0.7
// fleet's semantic index both speak to it.
package embedtest

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Server starts the endpoint; it closes with the test.
func Server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Input any `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var texts []string
		switch v := in.Input.(type) {
		case string:
			texts = []string{v}
		case []any:
			for _, x := range v {
				texts = append(texts, fmt.Sprint(x))
			}
		}
		data := make([]map[string]any, 0, len(texts))
		for i, text := range texts {
			data = append(data, map[string]any{"index": i, "embedding": BagOfWords(text)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// BagOfWords is the vector of a text: one hashed dimension per word,
// normalised.
func BagOfWords(text string) []float32 {
	const dims = 64
	vec := make([]float32, dims)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		vec[h.Sum32()%dims]++
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		n := float32(math.Sqrt(norm))
		for i := range vec {
			vec[i] /= n
		}
	}
	return vec
}
