// 本文件验证 Resume 稳定标识和对象键命名空间。
package identifier

import (
	"strings"
	"testing"
)

// TestGeneratorIsStableAndActorBound 验证幂等标识稳定且不同 Actor 不会碰撞。
func TestGeneratorIsStableAndActorBound(t *testing.T) {
	generator, err := NewGenerator("resume/v1")
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	first := generator.NewResumeID("10000000-0000-4000-8000-000000000001", "request-key-1")
	replay := generator.NewResumeID("10000000-0000-4000-8000-000000000001", "request-key-1")
	other := generator.NewResumeID("20000000-0000-4000-8000-000000000002", "request-key-1")
	if first != replay || first == other || len(first) != 36 || first[14] != '5' {
		t.Fatalf("unexpected IDs: %q %q %q", first, replay, other)
	}
	key := generator.NewObjectKey("10000000-0000-4000-8000-000000000001", first)
	if !strings.HasPrefix(key, "resume/v1/10000000-0000-4000-8000-000000000001/") ||
		!strings.HasSuffix(key, ".pdf") {
		t.Fatalf("unexpected object key: %q", key)
	}
}

// TestGeneratorRejectsUnsafePrefix 验证对象键不能逃逸 Resume 命名空间。
func TestGeneratorRejectsUnsafePrefix(t *testing.T) {
	if _, err := NewGenerator("../resume/v1"); err == nil {
		t.Fatal("unsafe prefix was accepted")
	}
}
