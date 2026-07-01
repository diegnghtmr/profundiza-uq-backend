package domain_test

import (
	"errors"
	"testing"

	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
	"github.com/uniquindio/profundiza-uq/internal/student/domain"
)

func TestNewStudentValidate(t *testing.T) {
	base := domain.NewStudent{
		InstitutionalEmail:                  "alumno@uq.edu.co",
		DocumentNumber:                      "1090",
		FullName:                            "Ana",
		AcademicShift:                       shared.ShiftDay,
		CompletedProfessionalElectivesCount: 0,
	}
	tests := []struct {
		name      string
		mutate    func(*domain.NewStudent)
		wantField string
	}{
		{"valid", func(*domain.NewStudent) {}, ""},
		{"empty email", func(n *domain.NewStudent) { n.InstitutionalEmail = "" }, "institutionalEmail"},
		{"email without at", func(n *domain.NewStudent) { n.InstitutionalEmail = "nope" }, "institutionalEmail"},
		{"empty document", func(n *domain.NewStudent) { n.DocumentNumber = "  " }, "documentNumber"},
		{"empty name", func(n *domain.NewStudent) { n.FullName = "" }, "fullName"},
		{"bad shift", func(n *domain.NewStudent) { n.AcademicShift = "EVENING" }, "academicShift"},
		{"count too high", func(n *domain.NewStudent) { n.CompletedProfessionalElectivesCount = 5 }, "completedProfessionalElectivesCount"},
		{"count negative", func(n *domain.NewStudent) { n.CompletedProfessionalElectivesCount = -1 }, "completedProfessionalElectivesCount"},
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

func TestStudentPatchValidate(t *testing.T) {
	empty := ""
	if err := (domain.StudentPatch{FullName: &empty}).Validate(); err == nil {
		t.Error("empty full name should be rejected")
	}
	bad := domain.Status("PENDING")
	if err := (domain.StudentPatch{Status: &bad}).Validate(); err == nil {
		t.Error("invalid status should be rejected")
	}
	high := 9
	if err := (domain.StudentPatch{CompletedProfessionalElectivesCount: &high}).Validate(); err == nil {
		t.Error("count above 4 should be rejected")
	}
	if !(domain.StudentPatch{}).IsEmpty() {
		t.Error("zero-value patch should be empty")
	}
}

func TestNewAcademicRecordValidate(t *testing.T) {
	ok := domain.NewAcademicRecord{SemesterID: "s1", Notes: "n", Source: "MANUAL"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if err := (domain.NewAcademicRecord{Notes: "n", Source: "MANUAL"}).Validate(); err == nil {
		t.Error("missing semesterId should be rejected")
	}
}
