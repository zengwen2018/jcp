package paths

import (
	"os"
	"path/filepath"
)

// dataDirEnv 是覆盖数据目录的环境变量名。
//
// Web 部署到服务器时，默认路径 ~/.config/jcp 落在 root 家目录下，
// 不便于挂载持久化卷，因此支持用环境变量显式指定。
const dataDirEnv = "JCPDATA_DIR"

// GetDataDir 获取应用数据目录。
//
// 优先级：
//  1. 环境变量 JCPDATA_DIR；
//  2. 系统用户配置目录下的 jcp 子目录；
//  3. 当前工作目录下的 data 目录。
func GetDataDir() string {
	if dir := os.Getenv(dataDirEnv); dir != "" {
		return dir
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil || userConfigDir == "" {
		return filepath.Join(".", "data")
	}
	return filepath.Join(userConfigDir, "jcp")
}

// GetCacheDir 获取缓存目录
func GetCacheDir() string {
	return filepath.Join(GetDataDir(), "cache")
}

// EnsureCacheDir 确保缓存目录存在并返回路径
func EnsureCacheDir(subDir string) string {
	dir := filepath.Join(GetCacheDir(), subDir)
	os.MkdirAll(dir, 0755)
	return dir
}
