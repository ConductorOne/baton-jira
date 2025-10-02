package session

import (
	"context"
	"encoding/json"

	"github.com/conductorone/baton-sdk/pkg/types"
)

func GetManyJSON[T any](ctx context.Context, session types.SessionStore, keys []string, opt ...types.SessionOption) (map[string]T, error) {
	// Get the raw bytes from cache
	rawMap, err := session.GetMany(ctx, keys, opt...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]T)
	// Unmarshal each item to the generic type
	for key, bytes := range rawMap {
		var item T
		err = json.Unmarshal(bytes, &item)
		if err != nil {
			return nil, err
		}
		result[key] = item
	}

	return result, nil
}

func SetManyJSON[T any](ctx context.Context, session types.SessionStore, items map[string]T, opt ...types.SessionOption) error {
	if session == nil {
		return nil
	}

	// Marshal each item to JSON bytes
	bytesMap := make(map[string][]byte)
	for key, item := range items {
		bytes, err := json.Marshal(item)
		if err != nil {
			return err
		}
		bytesMap[key] = bytes
	}

	// Store in cache
	return session.SetMany(ctx, bytesMap, opt...)
}

func GetJSON[T any](ctx context.Context, session types.SessionStore, key string, opt ...types.SessionOption) (T, bool, error) {
	var zero T
	if session == nil {
		return zero, false, nil
	}
	// Get the raw bytes from cache
	bytes, found, err := session.Get(ctx, key, opt...)
	if err != nil || !found {
		return zero, found, err
	}

	// Unmarshal to the generic type
	var item T
	err = json.Unmarshal(bytes, &item)
	if err != nil {
		return zero, false, err
	}

	return item, true, nil
}

func SetJSON[T any](ctx context.Context, session types.SessionStore, key string, item T, opt ...types.SessionOption) error {
	if session == nil {
		return nil
	}

	// Marshal the item to JSON bytes
	bytes, err := json.Marshal(item)
	if err != nil {
		return err
	}

	// Store in cache
	return session.Set(ctx, key, bytes, opt...)
}

func DeleteJSON(ctx context.Context, session types.SessionStore, key string, opt ...types.SessionOption) error {
	if session == nil {
		return nil
	}

	return session.Delete(ctx, key, opt...)
}

func ClearJSON(ctx context.Context, session types.SessionStore, opt ...types.SessionOption) error {
	if session == nil {
		return nil
	}

	return session.Clear(ctx, opt...)
}

func GetAllJSON[T any](ctx context.Context, session types.SessionStore, opt ...types.SessionOption) (map[string]T, error) {
	if session == nil {
		return nil, nil
	}

	// Get all raw bytes from cache
	rawMap, err := session.GetAll(ctx, opt...)
	if err != nil {
		return nil, err
	}

	result := make(map[string]T)
	// Unmarshal each item to the generic type
	for key, bytes := range rawMap {
		var item T
		err = json.Unmarshal(bytes, &item)
		if err != nil {
			return nil, err
		}
		result[key] = item
	}

	return result, nil
}
