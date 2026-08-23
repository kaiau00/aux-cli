package provider

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/models"
)

func TestNewProviderMock(t *testing.T) {
	model := models.Model{ID: "mock-model", Name: "Mock", Provider: models.ProviderMock}

	p, err := NewProvider(models.ProviderMock, WithModel(model))
	if err != nil {
		t.Fatalf("NewProvider(ProviderMock) returned error: %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider(ProviderMock) returned nil provider")
	}
	if got := p.Model(); got.ID != model.ID {
		t.Fatalf("Model() = %+v, want %+v", got, model)
	}

	resp, err := p.SendMessages(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("SendMessages returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("SendMessages returned nil response")
	}
}
