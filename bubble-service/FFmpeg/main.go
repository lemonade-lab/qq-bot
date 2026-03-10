package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	uploadDir   = "/tmp/uploads"
	outputDir   = "/tmp/outputs"
	maxFileSize = 500 * 1024 * 1024 // 500MB
)

type ConvertRequest struct {
	Format string `json:"format" binding:"required"`
}

type ConvertResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	FileName string `json:"file_name,omitempty"`
}

func init() {
	// 创建必要的目录
	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(outputDir, 0755)

	// 启动清理任务
	go cleanupOldFiles()
}

func main() {
	r := gin.Default()

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 格式转换 API
	r.POST("/convert", convertHandler)

	// 下载转换后的文件
	r.GET("/download/:filename", downloadHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("FFmpeg API Server 启动在端口: %s", port)
	r.Run(":" + port)
}

func convertHandler(c *gin.Context) {
	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ConvertResponse{
			Success: false,
			Message: "缺少文件参数",
		})
		return
	}

	// 检查文件大小
	if file.Size > maxFileSize {
		c.JSON(http.StatusBadRequest, ConvertResponse{
			Success: false,
			Message: "文件大小超过限制 (最大 500MB)",
		})
		return
	}

	// 获取目标格式
	targetFormat := c.PostForm("format")
	if targetFormat == "" {
		c.JSON(http.StatusBadRequest, ConvertResponse{
			Success: false,
			Message: "缺少 format 参数",
		})
		return
	}

	// 清理格式参数
	targetFormat = strings.TrimPrefix(targetFormat, ".")
	targetFormat = strings.ToLower(targetFormat)

	// 保存上传的文件
	timestamp := time.Now().Unix()
	inputFileName := fmt.Sprintf("%d_%s", timestamp, file.Filename)
	inputPath := filepath.Join(uploadDir, inputFileName)

	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		c.JSON(http.StatusInternalServerError, ConvertResponse{
			Success: false,
			Message: "保存文件失败: " + err.Error(),
		})
		return
	}
	defer os.Remove(inputPath) // 清理输入文件

	// 生成输出文件名
	outputFileName := fmt.Sprintf("%d_converted.%s", timestamp, targetFormat)
	outputPath := filepath.Join(outputDir, outputFileName)

	// 执行 ffmpeg 转换
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-y", // 覆盖输出文件
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("FFmpeg 错误: %s\n%s", err.Error(), string(output))
		c.JSON(http.StatusInternalServerError, ConvertResponse{
			Success: false,
			Message: "格式转换失败: " + err.Error(),
		})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, ConvertResponse{
		Success:  true,
		Message:  "转换成功",
		FileURL:  fmt.Sprintf("/download/%s", outputFileName),
		FileName: outputFileName,
	})
}

func downloadHandler(c *gin.Context) {
	filename := c.Param("filename")

	// 防止路径遍历攻击
	filename = filepath.Base(filename)
	filePath := filepath.Join(outputDir, filename)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "文件不存在",
		})
		return
	}

	c.File(filePath)
}

// 清理超过 1 小时的文件
func cleanupOldFiles() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cleanDir(uploadDir, time.Hour)
		cleanDir(outputDir, time.Hour)
	}
}

func cleanDir(dir string, maxAge time.Duration) {
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("读取目录失败 %s: %v", dir, err)
		return
	}

	now := time.Now()
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > maxAge {
			filePath := filepath.Join(dir, file.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("删除旧文件失败 %s: %v", filePath, err)
			} else {
				log.Printf("已删除旧文件: %s", filePath)
			}
		}
	}
}
