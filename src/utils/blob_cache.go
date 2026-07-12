package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChunkMeta 分片下载进度元数据
type ChunkMeta struct {
	Digest          string `json:"digest"`
	TotalSize       int64  `json:"totalSize"`
	ChunkSize       int64  `json:"chunkSize"`
	TotalChunks     int    `json:"totalChunks"`
	DownloadedChunks []int `json:"downloadedChunks"`
	UpdatedAt       string `json:"updatedAt"`
}

// BlobCache 磁盘 blob 缓存
type BlobCache struct {
	cacheDir  string
	chunkSize int64
}

// GlobalBlobCache 全局 blob 缓存实例
var GlobalBlobCache *BlobCache

// InitBlobCache 初始化 blob 磁盘缓存
func InitBlobCache(cacheDir string, chunkSizeMB int) (*BlobCache, error) {
	absDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("解析缓存路径失败: %w", err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	return &BlobCache{cacheDir: absDir, chunkSize: int64(chunkSizeMB) * 1024 * 1024}, nil
}

// ChunkSize 返回配置的分片大小
func (bc *BlobCache) ChunkSize() int64 {
	return bc.chunkSize
}

// cachePath 返回 digest 对应的最终缓存文件路径
func (bc *BlobCache) cachePath(digest string) string {
	safeName := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(bc.cacheDir, safeName+".blob")
}

// metaPath 返回分片进度文件路径
func (bc *BlobCache) metaPath(digest string) string {
	safeName := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(bc.cacheDir, safeName+".blob.meta")
}

// partPath 返回指定分片的文件路径
func (bc *BlobCache) partPath(digest string, index int) string {
	safeName := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(bc.cacheDir, fmt.Sprintf("%s.blob.part%04d", safeName, index))
}

// Exists 检查 digest 对应的完整缓存文件是否存在
func (bc *BlobCache) Exists(digest string) bool {
	info, err := os.Stat(bc.cachePath(digest))
	return err == nil && info.Size() > 0
}

// Get 打开缓存文件并返回 reader 和文件大小
func (bc *BlobCache) Get(digest string) (io.ReadCloser, int64, error) {
	path := bc.cachePath(digest)
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}

	return f, info.Size(), nil
}

// LoadMeta 读取分片进度文件，不存在返回 nil
func (bc *BlobCache) LoadMeta(digest string) (*ChunkMeta, error) {
	path := bc.metaPath(digest)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var meta ChunkMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("解析 meta 文件失败: %w", err)
	}
	return &meta, nil
}

// SaveMeta 写分片进度文件
func (bc *BlobCache) SaveMeta(meta *ChunkMeta) error {
	meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	tmpPath := bc.metaPath(meta.Digest) + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建 meta 文件失败: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(meta); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写 meta 文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭 meta 文件失败: %w", err)
	}
	return os.Rename(tmpPath, bc.metaPath(meta.Digest))
}

// GetMissingChunks 计算缺失的分片索引列表
func (bc *BlobCache) GetMissingChunks(meta *ChunkMeta) []int {
	done := make(map[int]bool)
	for _, c := range meta.DownloadedChunks {
		done[c] = true
	}
	var missing []int
	for i := 0; i < meta.TotalChunks; i++ {
		if !done[i] {
			missing = append(missing, i)
		}
	}
	return missing
}

// DownloadChunkStreamed 下载单个分片，同时写磁盘和流给客户端
func (bc *BlobCache) DownloadChunkStreamed(client *http.Client, blobURL string, chunkIndex int, meta *ChunkMeta, w io.Writer) error {
	start := int64(chunkIndex) * meta.ChunkSize
	end := start + meta.ChunkSize - 1
	if end >= meta.TotalSize {
		end = meta.TotalSize - 1
	}
	expectedSize := end - start + 1

	req, err := http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return fmt.Errorf("分片 %d 创建请求失败: %w", chunkIndex, err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("分片 %d 请求失败: %w", chunkIndex, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("上游不支持 Range (status %d)", resp.StatusCode)
	}

	tmpPath := bc.partPath(meta.Digest, chunkIndex) + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("分片 %d 创建文件失败: %w", chunkIndex, err)
	}

	// TeeReader: 上游数据同时写磁盘和客户端
	var written int64
	if w != nil {
		tee := io.TeeReader(resp.Body, f)
		written, err = io.Copy(w, tee)
	} else {
		written, err = io.Copy(f, resp.Body)
	}
	closeErr := f.Close()

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("分片 %d 写入失败: %w", chunkIndex, err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("分片 %d 关闭失败: %w", chunkIndex, closeErr)
	}
	if written != expectedSize {
		os.Remove(tmpPath)
		return fmt.Errorf("分片 %d 大小不匹配: expected %d, got %d", chunkIndex, expectedSize, written)
	}

	if err := os.Rename(tmpPath, bc.partPath(meta.Digest, chunkIndex)); err != nil {
		return fmt.Errorf("分片 %d rename 失败: %w", chunkIndex, err)
	}

	log.Printf("[BLOB CHUNK %d/%d] 下载完成 (%d bytes)\n", chunkIndex+1, meta.TotalChunks, written)
	return nil
}

// StreamParts 按顺序将已有分片文件写入 w（用于续传时先返回已缓存部分）
func (bc *BlobCache) StreamParts(digest string, meta *ChunkMeta, w io.Writer) error {
	for i := 0; i < meta.TotalChunks; i++ {
		partPath := bc.partPath(digest, i)
		if _, err := os.Stat(partPath); os.IsNotExist(err) {
			break // 遇到第一个缺失分片就停
		}
		f, err := os.Open(partPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

// AssembleBlob 拼接所有分片为最终 .blob 文件
func (bc *BlobCache) AssembleBlob(digest string, meta *ChunkMeta) error {
	finalPath := bc.cachePath(digest)
	tmpPath := finalPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建最终文件失败: %w", err)
	}

	var totalWritten int64
	for i := 0; i < meta.TotalChunks; i++ {
		partPath := bc.partPath(digest, i)
		part, err := os.Open(partPath)
		if err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("打开分片 %d 失败: %w", i, err)
		}
		written, err := io.Copy(f, part)
		part.Close()
		if err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("拼接分片 %d 失败: %w", i, err)
		}
		totalWritten += written
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭最终文件失败: %w", err)
	}
	if totalWritten != meta.TotalSize {
		os.Remove(tmpPath)
		return fmt.Errorf("拼接后大小不匹配: expected %d, got %d", meta.TotalSize, totalWritten)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename 最终文件失败: %w", err)
	}

	log.Printf("[BLOB ASSEMBLE] %s 拼接完成 (%d bytes, %d 分片)\n", digest, meta.TotalSize, meta.TotalChunks)
	return nil
}

// CleanupChunks 删除所有分片文件和 meta 文件
func (bc *BlobCache) CleanupChunks(digest string, meta *ChunkMeta) {
	for i := 0; i < meta.TotalChunks; i++ {
		os.Remove(bc.partPath(digest, i))
	}
	os.Remove(bc.metaPath(digest))
	log.Printf("[BLOB CLEANUP] 临时分片已清理: %s\n", digest)
}

// CleanPartial 删除分片残留（上游不支持 Range 等异常情况）
func (bc *BlobCache) CleanPartial(digest string) {
	// 尝试加载 meta 清理分片
	meta, err := bc.LoadMeta(digest)
	if err == nil && meta != nil {
		bc.CleanupChunks(digest, meta)
		return
	}
	// meta 损坏或不存在，用通配符清理
	os.Remove(bc.metaPath(digest))
	safeName := strings.ReplaceAll(digest, ":", "-")
	pattern := filepath.Join(bc.cacheDir, safeName+".blob.part*")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		os.Remove(m)
	}
}
