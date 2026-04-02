// Package client provides a remote HTTP client that implements the
// [service.Service] interface, allowing CLI commands to operate
// against a running rss2rm server.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rss2rm/internal/db"
	"rss2rm/internal/service"
)

// ErrLocalOnly is returned when an operation requires direct database access.
var ErrLocalOnly = errors.New("this operation requires local access (use CLI without --server flag)")

// RemoteService implements [service.Service] by forwarding operations
// to a running rss2rm HTTP server. Operations that require direct
// database access return [ErrLocalOnly].
type RemoteService struct {
	BaseURL string
	Client  *http.Client
}

// NewRemoteService returns a [RemoteService] pointing at the given server URL.
func NewRemoteService(url string) *RemoteService {
	return &RemoteService{
		BaseURL: strings.TrimSuffix(url, "/"),
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *RemoteService) AddFeed(ctx context.Context, feed db.Feed) error {
	data, err := json.Marshal(map[string]interface{}{
		"url":      feed.URL,
		"name":     feed.Name,
		"backfill": feed.Backfill,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/v1/feeds", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}
	return nil
}

func (s *RemoteService) UpdateFeed(ctx context.Context, feed db.Feed) error {
    return ErrLocalOnly
}

func (s *RemoteService) RemoveFeed(ctx context.Context, feedURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/feeds?url=%s", s.BaseURL, url.QueryEscape(feedURL)), nil)
	if err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}
	return nil
}

func (s *RemoteService) ListFeeds(ctx context.Context) ([]db.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/v1/feeds", nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var feeds []db.Feed
	if err := json.NewDecoder(resp.Body).Decode(&feeds); err != nil {
		return nil, err
	}
	return feeds, nil
}

func (s *RemoteService) PollFeeds(ctx context.Context, filterURLs []string, onEvent func(service.PollEvent)) error {
	opts := service.PollOptionsFromContext(ctx)
	// Trigger Poll
	data, _ := json.Marshal(map[string]interface{}{
		"urls":     filterURLs,
		"backfill": opts.BackfillLimit,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/v1/poll", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		errMsg := "unknown error"
		if b, err := io.ReadAll(resp.Body); err == nil && len(b) > 0 {
			errMsg = string(b)
		}
		return fmt.Errorf("failed to trigger poll, status: %d (%s)", resp.StatusCode, errMsg)
	}

	// Listen for events with a separate client (no timeout for SSE stream)
	sseClient := &http.Client{} // No timeout for SSE
	sseReq, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/api/v1/poll/events", nil)
	if err != nil {
		return err
	}

	respStream, err := sseClient.Do(sseReq)
	if err != nil {
		return err
	}
	defer respStream.Body.Close()

	// Parse SSE using bufio.Scanner for proper line handling
	scanner := bufio.NewScanner(respStream.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")
			var e service.PollEvent
			if err := json.Unmarshal([]byte(jsonStr), &e); err == nil {
				onEvent(e)
				// Exit when poll is complete
				if e.Type == service.EventPollComplete {
					return nil
				}
			}
		}
	}

	return scanner.Err()
}

func (s *RemoteService) RemoveFeedByID(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/feeds/%s", s.BaseURL, id), nil)
	if err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}
	return nil
}

func (s *RemoteService) AddDestination(ctx context.Context, destType, name string, config map[string]string, isDefault bool) (string, error) {
	return "", ErrLocalOnly
}

func (s *RemoteService) ListDestinations(ctx context.Context) ([]db.Destination, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) RemoveDestination(ctx context.Context, id string) error {
	return ErrLocalOnly
}

func (s *RemoteService) SetDefaultDestination(ctx context.Context, id string) error {
	return ErrLocalOnly
}

func (s *RemoteService) TestDestination(ctx context.Context, id string) error {
	return ErrLocalOnly
}

func (s *RemoteService) UpdateDestinationConfig(ctx context.Context, id string, config map[string]string) error {
	return ErrLocalOnly
}

func (s *RemoteService) UpdateDestination(ctx context.Context, id string, name string, config map[string]string) error {
	return ErrLocalOnly
}

func (s *RemoteService) GetDestinationByID(ctx context.Context, id string) (*db.Destination, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) GetFeedDelivery(ctx context.Context, feedID string) (*db.FeedDelivery, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) SetFeedDelivery(ctx context.Context, fd db.FeedDelivery) error {
	return ErrLocalOnly
}

func (s *RemoteService) RemoveFeedDelivery(ctx context.Context, feedID string) error {
	return ErrLocalOnly
}

func (s *RemoteService) AddDigest(ctx context.Context, digest db.Digest) (string, error) {
	return "", ErrLocalOnly
}

func (s *RemoteService) ListDigests(ctx context.Context) ([]db.Digest, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) RemoveDigest(ctx context.Context, id string) error {
	return ErrLocalOnly
}

func (s *RemoteService) UpdateDigest(ctx context.Context, digest db.Digest) error {
	return ErrLocalOnly
}

func (s *RemoteService) GetDigestByID(ctx context.Context, id string) (*db.Digest, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) GetDigestsForFeed(ctx context.Context, feedID string) ([]db.Digest, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) GetNewEntriesForDigest(ctx context.Context, digestID string, afterID int64) ([]db.Entry, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) AddFeedToDigest(ctx context.Context, digestID, feedID string, alsoIndividual bool) error {
	return ErrLocalOnly
}

func (s *RemoteService) RemoveFeedFromDigest(ctx context.Context, digestID, feedID string) error {
	return ErrLocalOnly
}

func (s *RemoteService) ListDigestFeeds(ctx context.Context, digestID string) ([]db.Feed, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) GenerateDigest(ctx context.Context, digestID string, onEvent func(service.PollEvent)) error {
	return ErrLocalOnly
}

func (s *RemoteService) ListRecentDeliveries(ctx context.Context, limit int) ([]db.DeliveryLogEntry, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) DeliverArticle(ctx context.Context, title, url, content, destID, directory, digestID string) error {
	return ErrLocalOnly
}

func (s *RemoteService) AddWebhook(ctx context.Context, webhookType, secret, config string) (string, error) {
	return "", ErrLocalOnly
}

func (s *RemoteService) ListWebhooks(ctx context.Context) ([]db.Webhook, error) {
	return nil, ErrLocalOnly
}

func (s *RemoteService) RemoveWebhook(ctx context.Context, id string) error {
	return ErrLocalOnly
}

func (s *RemoteService) GetWebhookByID(ctx context.Context, id string) (*db.Webhook, error) {
	return nil, ErrLocalOnly
}
