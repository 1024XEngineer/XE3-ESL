// 本文件验证 Resume 运行时在对象存储关闭时保持显式禁用。
package main

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

// TestBuildResumeCompositionIsOptionalWithoutObjectStorage 验证本地禁用 OSS 时不会挂载半可用 Resume 运行时。
func TestBuildResumeCompositionIsOptionalWithoutObjectStorage(t *testing.T) {
	composition, err := buildResumeComposition(
		context.Background(),
		nil,
		config.ObjectStorageConfig{Enabled: false},
	)
	if err != nil || composition != nil {
		t.Fatalf("composition = %#v, err = %v", composition, err)
	}
}
