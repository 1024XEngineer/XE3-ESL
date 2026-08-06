// Package identifier 为 Resume 模块生成可重放资源标识和隔离对象键。
package identifier

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
)

const resumeNamespace = "XE3-ESL/resume/v1"

// Generator 使用用户和幂等键生成稳定 UUID，并把对象限制在 Resume 前缀内。
type Generator struct {
	prefix string
}

// NewGenerator 创建 Resume 标识生成器。
func NewGenerator(prefix string) (*Generator, error) {
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "/")
	if prefix == "" || strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "\\") ||
		path.Clean(prefix) != prefix || strings.Contains(prefix, "..") {
		return nil, errors.New("resume: invalid object prefix")
	}
	return &Generator{prefix: prefix}, nil
}

// NewResumeID 根据 Actor 和幂等键生成稳定的名称型 UUID。
func (g *Generator) NewResumeID(ownerUserID string, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(resumeNamespace + "\x00" + ownerUserID + "\x00" + idempotencyKey))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]),
	)
}

// NewObjectKey 为一份简历生成不暴露文件名的稳定 PDF 对象键。
func (g *Generator) NewObjectKey(ownerUserID string, seed string) string {
	if g == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(resumeNamespace + "\x00" + ownerUserID + "\x00" + seed))
	return g.prefix + "/" + ownerUserID + "/" + hex.EncodeToString(sum[:]) + ".pdf"
}
