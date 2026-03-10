package service

import (
	"bubble/src/logger"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"

	"bubble/src/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOService 提供MinIO文件存储服务
type MinIOService struct {
	client   *minio.Client
	bucket   string          // 默认存储桶（向后兼容）
	buckets  map[string]bool // 所有管理的存储桶
	endpoint string
}

// NewMinIOService 创建MinIO服务实例
func NewMinIOService(cfg *config.Config) (*MinIOService, error) {
	// 初始化MinIO客户端
	client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKeyID, cfg.MinIOSecretAccessKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 收集所有需要创建的存储桶
	bucketsToCreate := make(map[string]bool)
	if len(cfg.MinIOBuckets) > 0 {
		for _, bucket := range cfg.MinIOBuckets {
			bucketsToCreate[bucket] = true
		}
	} else {
		// 向后兼容：如果没有配置多个存储桶，使用默认的
		bucketsToCreate[cfg.MinIOBucket] = true
	}

	svc := &MinIOService{
		client:   client,
		bucket:   cfg.MinIOBucket,
		buckets:  bucketsToCreate,
		endpoint: cfg.MinIOEndpoint,
	}

	// 确保所有存储桶存在
	ctx := context.Background()
	for bucketName := range bucketsToCreate {
		exists, err := client.BucketExists(ctx, bucketName)
		if err != nil {
			logger.Errorf("MinIO bucket check failed: %v (endpoint: %s, bucket: %s)", err, cfg.MinIOEndpoint, bucketName)
			return nil, fmt.Errorf("failed to check bucket existence: %w", err)
		}
		if !exists {
			logger.Infof("Creating MinIO bucket: %s", bucketName)
			err = client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
			if err != nil {
				logger.Errorf("MinIO bucket creation failed: %v", err)
				return nil, fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
			}
			logger.Infof("MinIO bucket created successfully: %s", bucketName)
		} else {
			logger.Infof("MinIO bucket already exists: %s", bucketName)
		}
	}

	return svc, nil
}

// getBucketForCategory 根据文件分类返回对应的存储桶名称
func (m *MinIOService) getBucketForCategory(category string) string {
	// 定义分类到存储桶的映射
	categoryToBucket := map[string]string{
		"avatars":            "avatars",
		"covers":             "covers",
		"emojis":             "emojis",
		"guild-chat-files":   "guild-chat-files",
		"private-chat-files": "private-chat-files",
		"temp":               "temp",
		"attachments":        "guild-chat-files", // 默认归类到频道聊天文件
		"icons":              "bubble",           // 图标归类到应用资产
	}

	bucket, ok := categoryToBucket[category]
	if !ok {
		// 如果分类不存在，检查是否直接是存储桶名称
		if m.buckets[category] {
			return category
		}
		// 默认使用 avatars 存储桶
		return "avatars"
	}

	// 验证存储桶是否存在
	if !m.buckets[bucket] {
		// 如果映射的存储桶不存在，使用默认存储桶
		logger.Warnf("bucket %s not configured, using default bucket %s", bucket, m.bucket)
		return m.bucket
	}

	return bucket
}

// UploadFile 通用文件上传方法
// category: 文件分类 (avatars, covers, emojis, guild-chat-files, private-chat-files, temp等)
// userID: 用户ID，用于组织文件路径
// file: 文件内容
// size: 文件大小
// contentType: 文件MIME类型
// 返回文件路径格式: {bucket}/{category}/{userID}/{timestamp}.{ext}
func (m *MinIOService) UploadFile(ctx context.Context, category string, userID uint, file io.Reader, size int64, contentType string) (string, error) {
	// 根据分类选择存储桶
	bucketName := m.getBucketForCategory(category)

	// 生成文件路径: {category}/{userID}/{timestamp}.{ext}
	ext := getExtensionFromContentType(contentType)
	if ext == "" {
		// 如果无法从Content-Type推断，尝试从category推断
		ext = "bin" // 默认二进制扩展名
	}
	timestamp := time.Now().Unix()
	objectName := fmt.Sprintf("%s/%d/%d.%s", category, userID, timestamp, ext)

	// 上传文件到对应的存储桶
	_, err := m.client.PutObject(ctx, bucketName, objectName, file, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to bucket %s: %w", bucketName, err)
	}

	// 返回格式: {bucket}/{objectName} 以便后续识别存储桶
	return fmt.Sprintf("%s/%s", bucketName, objectName), nil
}

// UploadGuildFile 上传文件到服务器作用域的路径
// 返回格式: {bucket}/{category}/guild/{guildID}/{timestamp}.{ext}
func (m *MinIOService) UploadGuildFile(ctx context.Context, category string, guildID uint, file io.Reader, size int64, contentType string) (string, error) {
	bucketName := m.getBucketForCategory(category)

	ext := getExtensionFromContentType(contentType)
	if ext == "" {
		ext = "bin"
	}
	timestamp := time.Now().Unix()
	objectName := fmt.Sprintf("%s/guild/%d/%d.%s", category, guildID, timestamp, ext)

	_, err := m.client.PutObject(ctx, bucketName, objectName, file, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload guild file to bucket %s: %w", bucketName, err)
	}

	return fmt.Sprintf("%s/%s", bucketName, objectName), nil
}

// UploadAvatar 上传用户头像（兼容方法，内部调用UploadFile）
// 返回文件路径(相对于bucket的路径)
func (m *MinIOService) UploadAvatar(ctx context.Context, userID uint, file io.Reader, size int64, contentType string) (string, error) {
	return m.UploadFile(ctx, "avatars", userID, file, size, contentType)
}

// DeleteFile 删除文件
// objectName 格式: {bucket}/{objectName} 或 {objectName}（向后兼容）
func (m *MinIOService) DeleteFile(ctx context.Context, objectName string) error {
	if objectName == "" {
		return nil // 如果为空，不需要删除
	}

	// 检查是否包含存储桶名称
	parts := strings.SplitN(objectName, "/", 2)
	var bucket, objName string
	if len(parts) == 2 && m.buckets[parts[0]] {
		// 包含存储桶名称
		bucket = parts[0]
		objName = parts[1]
	} else {
		// 向后兼容：只有对象名称，使用默认存储桶
		bucket = m.bucket
		objName = objectName
	}

	err := m.client.RemoveObject(ctx, bucket, objName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete file from bucket %s: %w", bucket, err)
	}
	return nil
}

// DeleteAvatar 删除用户头像（兼容方法）
func (m *MinIOService) DeleteAvatar(ctx context.Context, objectName string) error {
	return m.DeleteFile(ctx, objectName)
}

// GetFileURL 获取文件访问URL
// objectName 格式: {bucket}/{objectName} 或 {objectName}（向后兼容）
// 返回完整的访问URL
func (m *MinIOService) GetFileURL(objectName string) string {
	if objectName == "" {
		return ""
	}

	// 检查是否包含存储桶名称（格式: bucket/objectName）
	parts := strings.SplitN(objectName, "/", 2)
	var bucket, objName string
	if len(parts) == 2 && m.buckets[parts[0]] {
		// 包含存储桶名称
		bucket = parts[0]
		objName = parts[1]
	} else {
		// 向后兼容：只有对象名称，使用默认存储桶
		bucket = m.bucket
		objName = objectName
	}

	// 格式: /{bucket}/{objectName}
	return fmt.Sprintf("%s/%s", bucket, objName)
}

// GetAvatarURL 获取头像访问URL（兼容方法）
func (m *MinIOService) GetAvatarURL(objectName string) string {
	return m.GetFileURL(objectName)
}

// ListUserFiles 列出指定分类下某个用户的文件（按对象名降序）
func (m *MinIOService) ListUserFiles(ctx context.Context, category string, userID uint) ([]map[string]interface{}, error) {
	bucketName := m.getBucketForCategory(category)
	prefix := fmt.Sprintf("%s/%d/", category, userID)

	// 使用 ListObjects 来遍历对象
	ch := m.client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var results []map[string]interface{}
	for obj := range ch {
		if obj.Err != nil {
			return nil, obj.Err
		}

		// 尝试获取对象的元数据以读取 ContentType（StatObject）
		stat, err := m.client.StatObject(ctx, bucketName, obj.Key, minio.StatObjectOptions{})
		var contentType string
		if err == nil {
			contentType = stat.ContentType
		}

		// 构造 objectName 格式: {bucket}/{objectKey}
		objectName := fmt.Sprintf("%s/%s", bucketName, obj.Key)
		results = append(results, map[string]interface{}{
			"path":        objectName,
			"url":         m.GetFileURL(objectName),
			"size":        obj.Size,
			"contentType": contentType,
			"filename":    stat.Key, // stat.Key 包含对象名
		})
	}

	return results, nil
}

// ListGuildFiles 列出指定分类下某个服务器的文件（按对象名降序）
// category 例如: emojis
// guildID 用于组织对象前缀: {category}/guild/{guildID}/
func (m *MinIOService) ListGuildFiles(ctx context.Context, category string, guildID uint) ([]map[string]interface{}, error) {
	bucketName := m.getBucketForCategory(category)
	// 使用 guild 前缀组织服务器文件
	prefix := fmt.Sprintf("%s/guild/%d/", category, guildID)

	ch := m.client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var results []map[string]interface{}
	for obj := range ch {
		if obj.Err != nil {
			return nil, obj.Err
		}

		stat, err := m.client.StatObject(ctx, bucketName, obj.Key, minio.StatObjectOptions{})
		var contentType string
		if err == nil {
			contentType = stat.ContentType
		}

		objectName := fmt.Sprintf("%s/%s", bucketName, obj.Key)
		results = append(results, map[string]interface{}{
			"path":        objectName,
			"url":         m.GetFileURL(objectName),
			"size":        obj.Size,
			"contentType": contentType,
			"filename":    stat.Key,
		})
	}

	return results, nil
}

// MediaDimensions 图片/视频宽高
type MediaDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// DetectMediaDimensions 从 MinIO 下载图片并解析宽高
// 仅支持图片(JPEG/PNG/GIF/WebP)；视频不在此处理（需 ffprobe）
func (m *MinIOService) DetectMediaDimensions(ctx context.Context, objectPath, contentType string) (*MediaDimensions, error) {
	ct := strings.ToLower(contentType)
	if !strings.HasPrefix(ct, "image/") {
		return nil, nil // 非图片静默跳过
	}

	// 拆分 bucket / objectName
	bucket, objName := m.splitBucketPath(objectPath)

	obj, err := m.client.GetObject(ctx, bucket, objName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio get failed: %w", err)
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		return nil, fmt.Errorf("file stat failed: %w", err)
	}
	// 防止 OOM
	if info.Size > 30*1024*1024 {
		return nil, fmt.Errorf("file too large: %d bytes", info.Size)
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	// 1) Go 标准库解码 (JPEG, PNG, GIF)
	cfg, _, decErr := image.DecodeConfig(bytes.NewReader(data))
	if decErr == nil && cfg.Width > 0 && cfg.Height > 0 {
		return &MediaDimensions{Width: cfg.Width, Height: cfg.Height}, nil
	}

	// 2) WebP 头部手动解析
	if len(data) >= 30 {
		w, h, wpErr := parseWebPDimensions(data)
		if wpErr == nil && w > 0 && h > 0 {
			return &MediaDimensions{Width: w, Height: h}, nil
		}
	}

	return nil, fmt.Errorf("unable to decode image dimensions")
}

// splitBucketPath 从路径中拆分 bucket 和 objectName
func (m *MinIOService) splitBucketPath(path string) (string, string) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 && m.buckets[parts[0]] {
		return parts[0], parts[1]
	}
	return m.bucket, path
}

// parseWebPDimensions 从 WebP 文件头部解析宽高
func parseWebPDimensions(data []byte) (int, int, error) {
	if len(data) < 30 {
		return 0, 0, fmt.Errorf("data too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, fmt.Errorf("not a WebP file")
	}
	chunkType := string(data[12:16])
	switch chunkType {
	case "VP8 ":
		if data[23] != 0x9D || data[24] != 0x01 || data[25] != 0x2A {
			return 0, 0, fmt.Errorf("VP8 start code mismatch")
		}
		w := int(binary.LittleEndian.Uint16(data[26:28])) & 0x3FFF
		h := int(binary.LittleEndian.Uint16(data[28:30])) & 0x3FFF
		return w, h, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2F {
			return 0, 0, fmt.Errorf("VP8L signature mismatch")
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		w := int(bits&0x3FFF) + 1
		h := int((bits>>14)&0x3FFF) + 1
		return w, h, nil
	case "VP8X":
		w := int(data[24]) | int(data[25])<<8 | int(data[26])<<16 + 1
		h := int(data[27]) | int(data[28])<<8 | int(data[29])<<16 + 1
		return w, h, nil
	default:
		return 0, 0, fmt.Errorf("unknown WebP chunk: %s", chunkType)
	}
}

// getExtensionFromContentType 从Content-Type获取文件扩展名
func getExtensionFromContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg"):
		return "jpg"
	case strings.Contains(contentType, "png"):
		return "png"
	case strings.Contains(contentType, "gif"):
		return "gif"
	case strings.Contains(contentType, "webp"):
		return "webp"
	default:
		return ""
	}
}
