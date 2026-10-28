package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

// ---------------------------------------------------------------------------
// Model list (GET /models)
// ---------------------------------------------------------------------------

const modelsTimeout = 10 * time.Second

// modelsBodyLimit caps the model list body (mlx lists every cached model; a few KB normally).
const modelsBodyLimit = 4 << 20

// Models returns the ids the server lists at GET {base}/models, sorted, without duplicates or
// blanks. mlx_lm.server lists every model in the local cache, not only the loaded one.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, "/models", nil, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, modelsBodyLimit)).Decode(&list); err != nil {
		return nil, fmt.Errorf("model list from %s: %w", c.baseURL, err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, d := range list.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}
