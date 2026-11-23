package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"cogniz-crm-backend/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type S3Service struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	cfg           *config.Config
}

type PresignedUploadResponse struct {
	UploadURL        string `json:"upload_url"`
	ObjectKey        string `json:"object_key"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

func NewS3Service(ctx context.Context, cfg *config.Config) (*S3Service, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if cfg.S3Endpoint != "" {
			return aws.Endpoint{
				URL:               cfg.S3Endpoint,
				SigningRegion:     cfg.S3Region,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion(cfg.S3Region),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKeyID,
			cfg.S3SecretAccessKey,
			"",
		)),
		awsConfig.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true // Important for MinIO and local S3-compatible endpoints
	})
	presignClient := s3.NewPresignClient(client)

	return &S3Service{
		client:        client,
		presignClient: presignClient,
		cfg:           cfg,
	}, nil
}

// GeneratePresignedUpload creates a secure presigned PUT URL with enforced content-type and 5-min expiry
func (s *S3Service) GeneratePresignedUpload(ctx context.Context, userID, kind, ext string, maxBytes int64) (*PresignedUploadResponse, error) {
	objectUUID := uuid.New().String()
	var bucket string
	var objectKey string

	switch kind {
	case "card_image", "card_original":
		bucket = s.cfg.S3PrivateBucket
		objectKey = fmt.Sprintf("cards/%s/original/%s.%s", userID, objectUUID, ext)
	case "card_thumb":
		bucket = s.cfg.S3PrivateBucket
		objectKey = fmt.Sprintf("cards/%s/thumb/%s.%s", userID, objectUUID, ext)
	case "business_logo":
		bucket = s.cfg.S3PublicBucket
		objectKey = fmt.Sprintf("business/%s/logo/%s.%s", userID, objectUUID, ext)
	case "business_photo":
		bucket = s.cfg.S3PublicBucket
		objectKey = fmt.Sprintf("business/%s/photos/%s.%s", userID, objectUUID, ext)
	case "kyc_doc":
		bucket = s.cfg.S3PrivateBucket
		objectKey = fmt.Sprintf("kyc/%s/%s.%s", userID, objectUUID, ext)
	default:
		bucket = s.cfg.S3PrivateBucket
		objectKey = fmt.Sprintf("uploads/%s/%s.%s", userID, objectUUID, ext)
	}

	contentType := "image/webp"
	if ext == "jpg" || ext == "jpeg" {
		contentType = "image/jpeg"
	} else if ext == "png" {
		contentType = "image/png"
	} else if ext == "pdf" {
		contentType = "application/pdf"
	}

	putReq, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(objectKey),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(maxBytes),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = 5 * time.Minute
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate presigned PUT url: %w", err)
	}

	return &PresignedUploadResponse{
		UploadURL:        putReq.URL,
		ObjectKey:        objectKey,
		ExpiresInSeconds: 300,
	}, nil
}

// PutObject uploads bytes directly (used when client sends base64 card image)
func (s *S3Service) PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	bucket := s.cfg.S3PrivateBucket
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put object: %w", err)
	}
	return nil
}

// GetObject downloads private card image bytes
func (s *S3Service) GetObject(ctx context.Context, objectKey string) ([]byte, string, error) {
	bucket := s.cfg.S3PrivateBucket
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, "", fmt.Errorf("s3 get object: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", err
	}
	ct := "image/jpeg"
	if out.ContentType != nil && *out.ContentType != "" {
		ct = *out.ContentType
	}
	return data, ct, nil
}

// GetPublicURL returns the public CDN or bucket URL for a public object
func (s *S3Service) GetPublicURL(objectKey string) string {
	if s.cfg.S3PublicCDNURL != "" {
		return fmt.Sprintf("%s/%s", s.cfg.S3PublicCDNURL, objectKey)
	}
	return fmt.Sprintf("%s/%s/%s", s.cfg.S3Endpoint, s.cfg.S3PublicBucket, objectKey)
}
