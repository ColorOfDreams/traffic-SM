package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/tile38"
)

type commandClient interface {
	Do(...string) (string, error)
	Close() error
}

type dialCommandClient func() (commandClient, error)

type Tile38Scope struct {
	collections []string
	dial        dialCommandClient
}

func NewTile38Scope(address string, collections []string) (*Tile38Scope, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("Tile38 address is required")
	}
	normalizedCollections := make([]string, 0, len(collections))
	for _, collection := range collections {
		if collection = strings.TrimSpace(collection); collection != "" {
			normalizedCollections = append(normalizedCollections, collection)
		}
	}
	if len(normalizedCollections) == 0 {
		return nil, errors.New("at least one Tile38 traffic collection is required")
	}

	return &Tile38Scope{
		collections: normalizedCollections,
		dial: func() (commandClient, error) {
			return tile38.Dial(address)
		},
	}, nil
}

func (scope *Tile38Scope) Filter(
	ctx context.Context,
	fragments []matching.TraversalFragment,
) ([]matching.TraversalFragment, int, error) {
	if len(fragments) == 0 {
		return []matching.TraversalFragment{}, 0, nil
	}

	client, err := scope.dial()
	if err != nil {
		return nil, 0, err
	}
	defer client.Close()

	existsBySegment := make(map[string]bool)
	filtered := make([]matching.TraversalFragment, 0, len(fragments))
	dropped := 0
	for _, fragment := range fragments {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		segmentID := strings.TrimSpace(fragment.TrafficSegmentID)
		if segmentID == "" {
			return nil, 0, errors.New("traffic segment ID is required")
		}

		exists, checked := existsBySegment[segmentID]
		if !checked {
			exists, err = scope.exists(client, segmentID)
			if err != nil {
				return nil, 0, err
			}
			existsBySegment[segmentID] = exists
		}
		if !exists {
			dropped++
			continue
		}
		filtered = append(filtered, fragment)
	}
	return filtered, dropped, nil
}

func (scope *Tile38Scope) exists(client commandClient, segmentID string) (bool, error) {
	for _, collection := range scope.collections {
		response, err := client.Do("EXISTS", collection, segmentID)
		if err != nil {
			return false, fmt.Errorf(
				"check traffic segment %s in collection %s: %w",
				segmentID,
				collection,
				err,
			)
		}
		switch response {
		case "1":
			return true, nil
		case "0":
		default:
			return false, fmt.Errorf(
				"check traffic segment %s in collection %s: unexpected EXISTS response %q",
				segmentID,
				collection,
				response,
			)
		}
	}
	return false, nil
}
