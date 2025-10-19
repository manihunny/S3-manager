// Библиотека для работы с S3-совместимыми хранилищами (например, Amazon S3, MinIO и т.д.)
package s3_manager

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Manager interface {
	GetFiles(ctx context.Context, prefix string) ([]string, error)
	PutFile(ctx context.Context, storagePath StoragePath, data *BucketFile) (string, error)
	DeleteFiles(ctx context.Context, storagePath StoragePath, fileName string) error
	GetUploadPresignedURL(ctx context.Context, storagePath StoragePath, fileName string, expireTime time.Duration) (string, error)
	GetCatalogPattern(storagePath StoragePath) string
	AddCatalog(catalogType CatalogType, pathPattern string)
	GetObjectURL(storagePath StoragePath, fileName string) (string, error)
}

func NewS3Manager(ctx context.Context, cfg *Config, isTestServer bool) (S3Manager, error) {
	// Добавляем "test" к пути каталога, если сервер работает в тестовом режиме, чтобы отделить тестовые файлы от продовских
	if isTestServer {
		cfg.RootCatalog += "test/"
	}

	bucketCfg, err := config.LoadDefaultConfig(ctx,
		config.WithBaseEndpoint(cfg.Endpoint),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("NewS3Manager/LoadDefaultConfig: %w", err)
	}

	s3Manager := s3Manager{
		client: s3.NewFromConfig(bucketCfg),
		cfg:    cfg,
	}
	s3Manager.AddCatalog(PathCustomCatalog, "%s") // Путь для кастомного каталога

	return &s3Manager, nil
}

// Метод для получения ссылок на файлы в бакете по указанному пути (префиксу)
func (r *s3Manager) GetFiles(ctx context.Context, prefix string) ([]string, error) {
	getInput := &s3.ListObjectsV2Input{
		Bucket: &r.cfg.Name,
		Prefix: &prefix,
	}

	output, err := r.client.ListObjectsV2(ctx, getInput)
	if err != nil {
		return nil, fmt.Errorf("GetFiles/ListObjectsV2: %w", err)
	}

	var fileURLs []string
	for _, obj := range output.Contents {
		if obj.Key == nil {
			continue
		}

		url := fmt.Sprintf("%s/%s/%s", r.cfg.Endpoint, r.cfg.Name, *obj.Key)
		fileURLs = append(fileURLs, url)
	}

	return fileURLs, nil
}

// Метод для загрузки файла в бакет по указанному пути
func (r *s3Manager) PutFile(ctx context.Context, storagePath StoragePath, data *BucketFile) (string, error) {
	if data == nil || data.File == nil || data.Name == "" {
		return "", fmt.Errorf("PutFile: invalid file data")
	}

	storagePath.RootCatalog = r.cfg.RootCatalog
	fullPath := r.GetCatalogPattern(storagePath) + data.Name

	putInput := &s3.PutObjectInput{
		Bucket: &r.cfg.Name,
		Key:    &fullPath,
		Body:   data.File,
		ACL:    types.ObjectCannedACLPublicRead,
	}

	_, err := r.client.PutObject(ctx, putInput)
	if err != nil {
		return "", fmt.Errorf("PutFile/PutObject: %w", err)
	}

	fileURL, err := r.GetObjectURL(storagePath, data.Name)
	if err != nil {
		return "", fmt.Errorf("PutFile/GetObjectURL: %w", err)
	}

	return fileURL, nil
}

// Метод для удаления файлов в бакете. Если fileName не указан, удаляются все файлы по префиксу (весь каталог).
func (r *s3Manager) DeleteFiles(ctx context.Context, storagePath StoragePath, fileName string) error {
	storagePath.RootCatalog = r.cfg.RootCatalog
	fullPath := r.GetCatalogPattern(storagePath) + fileName

	// Получаем список объектов по заданному пути
	getInput := &s3.ListObjectsV2Input{
		Bucket: &r.cfg.Name,
		Prefix: &fullPath,
	}
	objects, err := r.client.ListObjectsV2(ctx, getInput)
	if err != nil {
		return fmt.Errorf("DeleteFiles/ListObjectsV2: %w", err)
	}

	// Формируем список объектов для удаления
	if len(objects.Contents) == 0 {
		return nil
	}
	var objectIds = make([]types.ObjectIdentifier, 0, len(objects.Contents))
	for _, item := range objects.Contents {
		objectIds = append(objectIds, types.ObjectIdentifier{
			Key: item.Key,
		})
	}

	// Удаляем объекты папки
	deleteInput := &s3.DeleteObjectsInput{
		Bucket: &r.cfg.Name,
		Delete: &types.Delete{
			Objects: objectIds,
			Quiet:   aws.Bool(true), // Подавляем вывод списка удалённых объектов
		},
	}

	_, err = r.client.DeleteObjects(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("DeleteFiles/DeleteObjects: %w", err)
	}

	return nil
}

// Метод для получения URL-адреса для загрузки файла в бакет. Используется для генерации подписанного URL-адреса для последующией загрузки файла.
func (r *s3Manager) GetUploadPresignedURL(ctx context.Context, storagePath StoragePath, fileName string, expireTime time.Duration) (string, error) {
	if fileName == "" {
		return "", fmt.Errorf("GetUploadPresignedURL: file name is empty")
	}

	presignClient := s3.NewPresignClient(r.client)
	storagePath.RootCatalog = r.cfg.RootCatalog
	fullPath := r.GetCatalogPattern(storagePath) + fileName

	putInput := &s3.PutObjectInput{
		Bucket: &r.cfg.Name,
		Key:    &fullPath,
	}

	if expireTime == 0 {
		expireTime = r.cfg.PresignedURLExpireTime
	}

	presignedRequest, err := presignClient.PresignPutObject(ctx, putInput, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("GetUploadPresignedURL/PresignPutObject: failed to create presigned request: %w", err)
	}

	return presignedRequest.URL, nil
}

// Метод для получения полного пути к каталогу файла в бакете (без имени файла)
func (r *s3Manager) GetCatalogPattern(storagePath StoragePath) string {
	pathPattern, ok := r.storagePaths[storagePath.CatalogType]
	if !ok {
		return ""
	}

	return fmt.Sprintf(storagePath.RootCatalog+pathPattern, storagePath.EntityID)
}

// Метод для добавления нового типа каталога с паттерном пути в бакете
func (r *s3Manager) AddCatalog(catalogType CatalogType, pathPattern string) {
	if r.storagePaths == nil {
		r.storagePaths = make(map[CatalogType]string)
	}
	r.storagePaths[catalogType] = pathPattern
}

// Метод для генерации URL-адреса объекта в бакете. Как правило используется для получения URL-адреса объекта, который будет загружен позже.
func (r *s3Manager) GetObjectURL(storagePath StoragePath, fileName string) (string, error) {
	if fileName == "" {
		return "", fmt.Errorf("GetObjectURL: file name is empty")
	}

	storagePath.RootCatalog = r.cfg.RootCatalog
	fullPath := r.GetCatalogPattern(storagePath) + fileName

	var fileURL string
	if r.cfg.CDN != "" {
		fileURL = fmt.Sprintf("%s/%s", r.cfg.CDN, fullPath)
	} else {
		fileURL = fmt.Sprintf("%s/%s/%s", r.cfg.Endpoint, r.cfg.Name, fullPath)
	}

	return fileURL, nil
}
