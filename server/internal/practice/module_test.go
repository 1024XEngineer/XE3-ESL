package practice

import (
	"errors"
	"strings"
	"testing"
)

func TestNewKeepsCompatibilityWithoutService(t *testing.T) {
	module := New()
	if got := module.Name(); got != "practice" {
		t.Fatalf("Name() = %q, want practice", got)
	}
	if module.Service() != nil {
		t.Fatal("Service() is available without dependencies")
	}
}

func TestNewWithDependenciesBuildsService(t *testing.T) {
	repository := newFakeRepository()
	module, err := NewWithDependencies(Dependencies{
		PreparationReader:  &fakePreparationReader{},
		Repository:         repository,
		TransactionManager: fakeTransactions{repo: repository},
		IDGenerator:        &fakeIDs{},
		Clock:              fakeClock{},
	})
	if err != nil {
		t.Fatalf("NewWithDependencies() error = %v", err)
	}
	if module.Name() != "practice" || module.Service() == nil {
		t.Fatalf("module = %#v", module)
	}
}

func TestNewWithDependenciesRejectsMissingDependency(t *testing.T) {
	repository := newFakeRepository()
	base := Dependencies{
		PreparationReader:  &fakePreparationReader{},
		Repository:         repository,
		TransactionManager: fakeTransactions{repo: repository},
		IDGenerator:        &fakeIDs{},
		Clock:              fakeClock{},
	}
	tests := []struct {
		name    string
		missing string
		change  func(*Dependencies)
	}{
		{name: "preparation reader", missing: "PreparationReader", change: func(d *Dependencies) { d.PreparationReader = nil }},
		{name: "repository", missing: "Repository", change: func(d *Dependencies) { d.Repository = nil }},
		{name: "transaction manager", missing: "TransactionManager", change: func(d *Dependencies) { d.TransactionManager = nil }},
		{name: "id generator", missing: "IDGenerator", change: func(d *Dependencies) { d.IDGenerator = nil }},
		{name: "clock", missing: "Clock", change: func(d *Dependencies) { d.Clock = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies := base
			tt.change(&dependencies)
			_, err := NewWithDependencies(dependencies)
			if !errors.Is(err, ErrPracticeModuleDependencyMissing) || !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("NewWithDependencies() error = %v", err)
			}
		})
	}
}
