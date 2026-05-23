package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BlobCache 磁盘 blob 缓存
type BlobCache struct {
	cacheDir string
}

// GlobalBlobCache 全局 blob 缓存实例
var GlobalBlobCache *BlobCache

// InitBlobCache 初始化 blob 磁盘缓存
func InitBlobCache(cacheDir string) (*BlobCache, error) {
	absDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("解析缓存路径失败: %w", err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	return &BlobCache{cacheDir: absDir}, nil
}

// cachePath 返回 digest 对应的缓存文件路径
func (bc *BlobCache) cachePath(digest string) string {
	return filepath.Join(bc.cacheDir, digest+".blob")
}

// Exists 检查 digest 对应的缓存文件是否存在
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

// PutAndStream 从上游 reader 读取数据，同时写磁盘和返回客户端
// 先写 .tmp 临时文件，完成后再 rename，避免中断留脏数据
func (bc *BlobCache) PutAndStream(digest string, upstream io.Reader, client io.Writer) (int64, error) {
	finalPath := bc.cachePath(digest)
	tmpPath := finalPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("创建临时缓存文件失败: %w", err)
	}

	tee := io.TeeReader(upstream, f)
	written, err := io.Copy(client, tee)
	closeErr := f.Close()

	if err != nil {
		os.Remove(tmpPath)
		return written, err
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return written, closeErr
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return written, fmt.Errorf("缓存文件重命名失败: %w", err)
	}

	fmt.Printf("Blob缓存已写入: %s (%d bytes)\n", digest, written)
	return written, nil
}
