package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Service struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

type Document struct {
	ID      string    `json:"id"`
	Service []Service `json:"service"`
}

const pdsServiceID = "#atproto_pds"

func ResolveDID(ctx context.Context, did string) (*Document, error) {
	var url string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		url = "https://plc.directory/" + did
	case strings.HasPrefix(did, "did:web:"):
		host := strings.TrimPrefix(did, "did:web:")
		url = "https://" + host + "/.well-known/did.json"
	default:
		return nil, fmt.Errorf("unsupported did method: %s", did)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("did resolve failed: %s (status %d)", did, resp.StatusCode)
	}

	var doc Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

func (d *Document) PDSEndpoint() (string, error) {
	for _, s := range d.Service {
		if s.ID == pdsServiceID {
			return s.ServiceEndpoint, nil
		}
	}
	return "", fmt.Errorf("pds service not found in did document: %s", d.ID)
}
