package service

import (
	"bubble/src/logger"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

// AudioConverterService 提供音频格式转换服务（通过远程 FFmpeg 服务）
type AudioConverterService struct {
	minioService *MinIOService
	ffmpegURL    string
	httpClient   *http.Client
	mu           sync.Mutex
}

// AudioConversionJob 表示一个音频转换任务
type AudioConversionJob struct {
	OriginalPath string
	Category     string
	UserID       uint
	GuildID      *uint
}

// FFmpegConvertResponse FFmpeg 服务的响应结构
type FFmpegConvertResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	FileName string `json:"file_name,omitempty"`
}

// NewAudioConverterService 创建音频转换服务实例
func NewAudioConverterService(minioService *MinIOService, ffmpegURL string) (*AudioConverterService, error) {
	if ffmpegURL == "" {
		return nil, fmt.Errorf("ffmpeg service URL is not configured")
	}

	// 创建 HTTP 客户端
	httpClient := &http.Client{
		Timeout: 30 * time.Second, // 转换可能需要一些时间
	}

	// 测试 FFmpeg 服务是否可用
	if err := checkFFmpegServiceAvailable(ffmpegURL, httpClient); err != nil {
		logger.Warnf("FFmpeg service not available: %v. Audio conversion will be disabled.", err)
		return &AudioConverterService{
			minioService: minioService,
			ffmpegURL:    ffmpegURL,
			httpClient:   httpClient,
		}, nil
	}

	logger.Infof("FFmpeg service is available at: %s", ffmpegURL)

	return &AudioConverterService{
		minioService: minioService,
		ffmpegURL:    ffmpegURL,
		httpClient:   httpClient,
	}, nil
}

// checkFFmpegServiceAvailable 检查 FFmpeg 服务是否可用
func checkFFmpegServiceAvailable(ffmpegURL string, client *http.Client) error {
	// 健康检查端点
	healthURL := ffmpegURL + "/health"
	resp, err := client.Get(healthURL)
	if err != nil {
		return fmt.Errorf("ffmpeg service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("ffmpeg service error: status %d", resp.StatusCode)
	}

	return nil
}

// IsAudioFile 判断是否为音频文件（基于 MIME 类型）
func IsAudioFile(contentType string) bool {
	audioMimeTypes := []string{
		"audio/mpeg",    // mp3
		"audio/mp3",     // mp3
		"audio/ogg",     // ogg
		"audio/wav",     // wav
		"audio/webm",    // webm
		"audio/aac",     // aac
		"audio/m4a",     // m4a
		"audio/x-m4a",   // m4a
		"audio/flac",    // flac
		"audio/x-flac",  // flac
		"audio/amr",     // amr
		"audio/silk",    // silk (微信语音)
		"audio/x-wav",   // wav
		"audio/wave",    // wav
		"audio/vnd.wav", // wav
	}

	for _, mimeType := range audioMimeTypes {
		if strings.HasPrefix(strings.ToLower(contentType), mimeType) {
			return true
		}
	}

	return false
}

// NeedsConversion 判断音频是否需要转换
// 支持的通用格式：MP3 (所有平台), AAC (iOS/Android), OGG (Web/Android)
func NeedsConversion(contentType string) bool {
	// 如果已经是 MP3，则不需要转换（MP3 是最通用的格式）
	if strings.Contains(strings.ToLower(contentType), "mpeg") ||
		strings.Contains(strings.ToLower(contentType), "mp3") {
		return false
	}

	return IsAudioFile(contentType)
}

// ConvertAudioAsync 异步转换音频文件为多种格式
func (s *AudioConverterService) ConvertAudioAsync(ctx context.Context, job AudioConversionJob) {
	go func() {
		if err := s.convertAudio(context.Background(), job); err != nil {
			logger.Errorf("Audio conversion failed for %s: %v", job.OriginalPath, err)
		}
	}()
}

// convertAudio 执行音频转换（同步）
// 将音频转换为三种格式：
// - MP3: iOS/Android/Web 通用，最广泛支持
// - M4A (AAC): iOS 原生支持，高质量
// - OGG: Web/Android 支持，开源格式
func (s *AudioConverterService) convertAudio(ctx context.Context, job AudioConversionJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Infof("Starting audio conversion for: %s", job.OriginalPath)

	// 1. 从 MinIO 下载原始文件
	audioData, _, err := s.downloadFromMinIO(ctx, job.OriginalPath)
	if err != nil {
		return fmt.Errorf("failed to download original file: %w", err)
	}

	// 2. 获取文件基本信息
	baseName := filepath.Base(job.OriginalPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	// 3. 转换为多种格式
	formats := []string{"mp3", "m4a", "ogg"}

	for _, format := range formats {
		// 调用远程 FFmpeg 服务进行转换
		convertedData, err := s.convertViaHTTP(audioData, baseName, format)
		if err != nil {
			logger.Errorf("Failed to convert to %s via HTTP: %v", format, err)
			continue
		}

		// 4. 上传转换后的文件到 MinIO
		newFileName := nameWithoutExt + "." + format
		newPath := strings.Replace(job.OriginalPath, baseName, newFileName, 1)

		if err := s.uploadConvertedToMinIO(ctx, convertedData, newPath, job.Category, format); err != nil {
			logger.Errorf("Failed to upload converted file %s: %v", newFileName, err)
			continue
		}

		logger.Infof("Successfully converted and uploaded: %s", newPath)
	}

	return nil
}

// convertViaHTTP 通过 HTTP 调用远程 FFmpeg 服务进行转换
func (s *AudioConverterService) convertViaHTTP(audioData []byte, originalFileName, targetFormat string) ([]byte, error) {
	// 创建 multipart form 请求
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// 添加文件
	part, err := writer.CreateFormFile("file", originalFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}

	// 添加目标格式
	if err := writer.WriteField("format", targetFormat); err != nil {
		return nil, fmt.Errorf("failed to write format field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// 发送 HTTP 请求
	convertURL := s.ffmpegURL + "/convert"
	req, err := http.NewRequest("POST", convertURL, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("conversion failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 读取响应
	var result FFmpegConvertResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 检查转换是否成功
	if !result.Success {
		errorMsg := result.Message
		if errorMsg == "" {
			errorMsg = "conversion failed"
		}
		return nil, fmt.Errorf("conversion error: %s", errorMsg)
	}

	if result.FileURL == "" {
		return nil, fmt.Errorf("no file URL in response")
	}

	// 下载转换后的文件
	return s.downloadConvertedFile(result.FileURL)
}

// downloadConvertedFile 从 FFmpeg 服务下载转换后的文件
func (s *AudioConverterService) downloadConvertedFile(filename string) ([]byte, error) {
	// 构建下载 URL：/download/{filename}
	var downloadURL string
	if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
		// 如果已经是完整 URL，直接使用
		downloadURL = filename
	} else if strings.HasPrefix(filename, "/download/") {
		// 如果已经包含 /download/ 前缀，拼接 base URL
		downloadURL = s.ffmpegURL + filename
	} else {
		// 否则添加 /download/ 前缀
		downloadURL = s.ffmpegURL + "/download/" + filename
	}

	resp, err := s.httpClient.Get(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download converted file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file, status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	return data, nil
}

// downloadFromMinIO 从 MinIO 下载文件到内存
func (s *AudioConverterService) downloadFromMinIO(ctx context.Context, objectName string) ([]byte, string, error) {
	// 从对象名称中提取存储桶（假设格式为 bucket/path/to/file）
	parts := strings.SplitN(objectName, "/", 2)
	if len(parts) < 2 {
		return nil, "", fmt.Errorf("invalid object name format: %s", objectName)
	}

	bucket := parts[0]
	objectPath := parts[1]

	// 从 MinIO 获取对象
	object, err := s.minioService.client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get object from minio: %w", err)
	}
	defer object.Close()

	// 读取对象内容到内存
	data, err := io.ReadAll(object)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read object: %w", err)
	}

	// 获取对象信息以获取 content type
	objInfo, err := object.Stat()
	if err != nil {
		return data, "application/octet-stream", nil // 如果无法获取信息，返回默认类型
	}

	return data, objInfo.ContentType, nil
}

// uploadConvertedToMinIO 上传转换后的文件数据到 MinIO
func (s *AudioConverterService) uploadConvertedToMinIO(ctx context.Context, data []byte, objectName, category, format string) error {
	// 确定 content type
	contentType := getContentTypeByExtension("." + format)

	// 从对象名称中提取存储桶
	parts := strings.SplitN(objectName, "/", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid object name format: %s", objectName)
	}

	bucket := parts[0]
	objectPath := parts[1]

	// 上传到 MinIO
	reader := bytes.NewReader(data)
	_, err := s.minioService.client.PutObject(
		ctx,
		bucket,
		objectPath,
		reader,
		int64(len(data)),
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to upload to minio: %w", err)
	}

	return nil
}

// getContentTypeByExtension 根据文件扩展名返回 Content-Type
func getContentTypeByExtension(ext string) string {
	contentTypes := map[string]string{
		".mp3":  "audio/mpeg",
		".m4a":  "audio/mp4",
		".aac":  "audio/aac",
		".ogg":  "audio/ogg",
		".wav":  "audio/wav",
		".flac": "audio/flac",
		".webm": "audio/webm",
	}

	contentType, ok := contentTypes[strings.ToLower(ext)]
	if !ok {
		return "application/octet-stream"
	}

	return contentType
}
