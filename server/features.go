package server

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"iter"
	"slices"
)

// featureSet is a generic collection for managing tools/resources/prompts/
// resourceTemplates. Iteration order is sorted by uid, which also satisfies
// the spec's deterministic-ordering recommendation for list results.
// sortedKeys is maintained incrementally by add/remove so that reads are pure:
// the list handlers iterate under the server's read lock only.
type featureSet[T any] struct {
	uniqueID   func(T) string
	features   map[string]T
	sortedKeys []string
}

func newFeatureSet[T any](uniqueIDFunc func(T) string) *featureSet[T] {
	return &featureSet[T]{
		uniqueID: uniqueIDFunc,
		features: make(map[string]T),
	}
}

// add adds or replaces features.
func (s *featureSet[T]) add(fs ...T) {
	for _, f := range fs {
		uid := s.uniqueID(f)
		if _, replaced := s.features[uid]; !replaced {
			i, _ := slices.BinarySearch(s.sortedKeys, uid)
			s.sortedKeys = slices.Insert(s.sortedKeys, i, uid)
		}
		s.features[uid] = f
	}
}

// remove removes features by uid, returns true if any were removed.
func (s *featureSet[T]) remove(uids ...string) bool {
	changed := false
	for _, uid := range uids {
		if _, ok := s.features[uid]; ok {
			changed = true
			delete(s.features, uid)
			if i, found := slices.BinarySearch(s.sortedKeys, uid); found {
				s.sortedKeys = slices.Delete(s.sortedKeys, i, i+1)
			}
		}
	}
	return changed
}

// get retrieves a feature by uid.
func (s *featureSet[T]) get(uid string) (T, bool) {
	t, ok := s.features[uid]
	return t, ok
}

// all returns an iterator over all features sorted by uid.
func (s *featureSet[T]) all() iter.Seq[T] {
	return func(yield func(T) bool) {
		s.yieldFrom(0, yield)
	}
}

// above returns an iterator over features with uid greater than the given
// value (for cursor pagination).
func (s *featureSet[T]) above(uid string) iter.Seq[T] {
	index, found := slices.BinarySearch(s.sortedKeys, uid)
	if found {
		index++
	}
	return func(yield func(T) bool) {
		s.yieldFrom(index, yield)
	}
}

func (s *featureSet[T]) yieldFrom(index int, yield func(T) bool) {
	for i := index; i < len(s.sortedKeys); i++ {
		if !yield(s.features[s.sortedKeys[i]]) {
			return
		}
	}
}

// DefaultPageSize is the default page size for list pagination.
const DefaultPageSize = 1000

// pageToken is the internal representation of a pagination cursor.
type pageToken struct {
	LastUID string
}

// encodeCursor encodes a uid into an opaque pagination cursor string.
func encodeCursor(uid string) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(pageToken{LastUID: uid}); err != nil {
		return "", fmt.Errorf("failed to encode page token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(buf.Bytes()), nil
}

// decodeCursor decodes an opaque pagination cursor string into a pageToken.
func decodeCursor(cursor string) (*pageToken, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cursor: %w", err)
	}
	var token pageToken
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode page token: %w", err)
	}
	return &token, nil
}

// paginateList performs cursor-based pagination on a featureSet.
// A nil cursor starts from the beginning (an empty string is a cursor like any
// other, and never one this server issued). pageSize <= 0 returns all items.
// A nil nextCursor means the result set is exhausted.
func paginateList[T any](fs *featureSet[T], pageSize int, cursor *string) (items []T, nextCursor *string, err error) {
	var seq iter.Seq[T]
	if cursor == nil {
		seq = fs.all()
	} else {
		pt, err := decodeCursor(*cursor)
		if err != nil {
			return nil, nil, err
		}
		seq = fs.above(pt.LastUID)
	}

	if pageSize <= 0 {
		for f := range seq {
			items = append(items, f)
		}
		return items, nil, nil
	}

	var count int
	for f := range seq {
		count++
		if count > pageSize {
			break
		}
		items = append(items, f)
	}

	if count <= pageSize {
		return items, nil, nil
	}

	next, err := encodeCursor(fs.uniqueID(items[len(items)-1]))
	if err != nil {
		return items, nil, err
	}
	return items, &next, nil
}
