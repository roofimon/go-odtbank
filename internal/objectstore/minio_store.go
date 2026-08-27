package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIOStore(ctx context.Context, endpoint, accessKey, secretKey, bucket string, secure bool) (*MinIOStore, error) {
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	store := &MinIOStore{client: client, bucket: bucket}
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exists, err := client.BucketExists(initCtx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check MinIO bucket %q: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(initCtx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create MinIO bucket %q: %w", bucket, err)
		}
	}
	return store, nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("upload passport object: %w", err)
	}
	return nil
}

func (s *MinIOStore) Get(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get passport object: %w", err)
	}
	defer object.Close()
	if _, err := object.Stat(); err != nil {
		return nil, fmt.Errorf("stat passport object: %w", err)
	}
	body, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("read passport object: %w", err)
	}
	return body, nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete passport object: %w", err)
	}
	return nil
}
