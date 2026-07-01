package domain_test

import (
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/adminuser/domain"
)

func TestNewAdminUserValidate(t *testing.T) {
	base := domain.NewAdminUser{InstitutionalEmail: "a@uq.edu.co", FullName: "Admin", Role: domain.RoleAdmin}
	tests := []struct {
		name      string
		mutate    func(*domain.NewAdminUser)
		wantField string
	}{
		{"valid", func(*domain.NewAdminUser) {}, ""},
		{"empty email", func(n *domain.NewAdminUser) { n.InstitutionalEmail = "" }, "institutionalEmail"},
		{"email without at", func(n *domain.NewAdminUser) { n.InstitutionalEmail = "nope" }, "institutionalEmail"},
		{"empty name", func(n *domain.NewAdminUser) { n.FullName = " " }, "fullName"},
		{"bad role", func(n *domain.NewAdminUser) { n.Role = "ROOT" }, "role"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := base
			tt.mutate(&n)
			err := n.Validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			var ve domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}

func TestAdminUserPatchValidate(t *testing.T) {
	bad := domain.Role("ROOT")
	if err := (domain.AdminUserPatch{Role: &bad}).Validate(); err == nil {
		t.Error("invalid role should be rejected")
	}
	empty := ""
	if err := (domain.AdminUserPatch{FullName: &empty}).Validate(); err == nil {
		t.Error("empty name should be rejected")
	}
	if err := (domain.AdminUserPatch{}).Validate(); err != nil {
		t.Errorf("empty patch should be valid, got %v", err)
	}
}
