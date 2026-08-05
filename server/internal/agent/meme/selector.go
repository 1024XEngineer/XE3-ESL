package meme

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

const SelectionPolicyVersion = "meme-selection-v1"

// DeterministicSelector makes retries stable while preferring assets outside
// the recent thread window.
type DeterministicSelector struct{}

func (DeterministicSelector) Select(
	_ context.Context,
	request SelectionRequest,
) ([]Asset, error) {
	if request.RunID == "" || request.ThreadID == "" || request.Category == "" ||
		request.Maximum < 1 || request.Maximum > 4 || len(request.Candidates) == 0 ||
		request.PolicyVersion != SelectionPolicyVersion {
		return nil, ErrInvalidRequest
	}
	recent := make(map[string]struct{}, len(request.RecentMemeIDs))
	for _, id := range request.RecentMemeIDs {
		recent[id] = struct{}{}
	}
	preferred := make([]Asset, 0, len(request.Candidates))
	fallback := make([]Asset, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.Category != request.Category || candidate.Weight <= 0 {
			return nil, ErrInvalidRequest
		}
		if _, used := recent[candidate.MemeID]; used {
			fallback = append(fallback, candidate)
		} else {
			preferred = append(preferred, candidate)
		}
	}
	sortAssets := func(assets []Asset) {
		sort.SliceStable(assets, func(i, j int) bool {
			left := selectionScore(request, assets[i])
			right := selectionScore(request, assets[j])
			if left == right {
				return assets[i].MemeID < assets[j].MemeID
			}
			return left < right
		})
	}
	sortAssets(preferred)
	sortAssets(fallback)
	ordered := append(preferred, fallback...)
	maximum := min(request.Maximum, len(ordered))
	return append([]Asset(nil), ordered[:maximum]...), nil
}

func selectionScore(request SelectionRequest, asset Asset) uint64 {
	digest := sha256.Sum256([]byte(
		request.RunID + "\x00" + request.ThreadID + "\x00" +
			request.PolicyVersion + "\x00" + asset.MemeID,
	))
	return binary.BigEndian.Uint64(digest[:8]) / uint64(asset.Weight)
}

var _ Selector = DeterministicSelector{}
