package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type GetRecordResponse struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

func GetRecord(ctx context.Context, pdsEndpoint, did, collection, rkey string) (*GetRecordResponse, error) {
	endpoint, err := url.Parse(pdsEndpoint + "/xrpc/com.atproto.repo.getRecord")
	if err != nil {
		return nil, err
	}

	q := endpoint.Query()
	q.Set("repo", did)
	q.Set("collection", collection)
	q.Set("rkey", rkey)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get record failed: %s/%s/%s (status %d)", did, collection, rkey, resp.StatusCode)
	}

	var record GetRecordResponse
	if err := json.NewDecoder(resp.Body).Decode(&record); err != nil {
		return nil, err
	}

	return &record, nil
}
