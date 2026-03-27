package keystore

import "context"

type KeyStore interface {
	Get(ctx context.Context, serverID string) ([]byte, error)
	Put(ctx context.Context, serverID string, key []byte) error
	Delete(ctx context.Context, serverID string) error
}
