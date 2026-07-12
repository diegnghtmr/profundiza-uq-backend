package app

import (
	"context"
	"errors"

	"github.com/uniquindio/profundiza-uq/internal/catalog/domain"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Admin errors.
var (
	ErrAdminInvalid   = errors.New("catalog: invalid input")
	ErrAdminNotFound  = errors.New("catalog: not found")
	ErrAdminDuplicate = errors.New("catalog: prerequisite already exists")
)

// Inputs for catalog administration.
type (
	CreateElectiveInput struct {
		Name, Area, Description, ActorID string
	}
	UpdateElectiveInput struct {
		ID                         string
		Name, Area, Description    *string
		Status                     *string
		ActorID                    string
	}
	CreatePrerequisiteInput struct {
		ElectiveID, Name, Description string
		PlanType                     *string
		ActorID                      string
	}
	CreateOfferingInput struct {
		SemesterID, ElectiveID, ActorID string
	}
	CreateOfferingPrereqInput struct {
		OfferingID, Name, Description, Source string
		PlanType                             *string
		ActorID                              string
	}
	CreateGroupInput struct {
		OfferingID, GroupCode string
		Shift                 shared.AcademicShift
		TeacherName           *string
		ScheduleText          string
		Capacity              int
		Reason                string
		ActorID               string
	}
	UpdateGroupInput struct {
		ID           string
		GroupCode    *string
		TeacherName  *string
		ScheduleText *string
		Status       *string
		Reason       string
		ActorID      string
	}
	AdjustCapacityInput struct {
		GroupID     string
		NewCapacity int
		Reason      string
		ActorID     string
	}
)

// AdminRepository is the catalog write port.
type AdminRepository interface {
	ListElectives(ctx context.Context, q, area string) ([]domain.Elective, error)
	GetElective(ctx context.Context, id string) (*domain.Elective, error)
	CreateElective(ctx context.Context, in CreateElectiveInput) (domain.Elective, error)
	UpdateElective(ctx context.Context, in UpdateElectiveInput) (domain.Elective, error)
	ListElectivePrerequisites(ctx context.Context, electiveID string) ([]domain.Prerequisite, error)
	CreatePrerequisite(ctx context.Context, in CreatePrerequisiteInput) (domain.Prerequisite, error)
	CreateOffering(ctx context.Context, in CreateOfferingInput) (string, error)
	CreateOfferingPrereq(ctx context.Context, in CreateOfferingPrereqInput) (domain.Prerequisite, error)
	CreateGroup(ctx context.Context, in CreateGroupInput) (domain.Group, error)
	UpdateGroup(ctx context.Context, in UpdateGroupInput) (domain.Group, error)
	AdjustCapacity(ctx context.Context, in AdjustCapacityInput) (domain.Group, error)
}

// AdminService exposes catalog administration use cases.
type AdminService struct{ repo AdminRepository }

// NewAdminService wires the admin catalog service.
func NewAdminService(repo AdminRepository) *AdminService { return &AdminService{repo: repo} }

// ListElectives lists electives with optional text/area filters.
func (s *AdminService) ListElectives(ctx context.Context, q, area string) ([]domain.Elective, error) {
	return s.repo.ListElectives(ctx, q, area)
}

// GetElective returns one elective.
func (s *AdminService) GetElective(ctx context.Context, id string) (*domain.Elective, error) {
	e, err := s.repo.GetElective(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, ErrAdminNotFound
	}
	return e, nil
}

// CreateElective creates an elective.
func (s *AdminService) CreateElective(ctx context.Context, in CreateElectiveInput) (domain.Elective, error) {
	if in.Name == "" || in.Area == "" {
		return domain.Elective{}, ErrAdminInvalid
	}
	return s.repo.CreateElective(ctx, in)
}

// UpdateElective updates an elective.
func (s *AdminService) UpdateElective(ctx context.Context, in UpdateElectiveInput) (domain.Elective, error) {
	return s.repo.UpdateElective(ctx, in)
}

// ListElectivePrerequisites lists an elective's base prerequisites.
func (s *AdminService) ListElectivePrerequisites(ctx context.Context, electiveID string) ([]domain.Prerequisite, error) {
	return s.repo.ListElectivePrerequisites(ctx, electiveID)
}

// CreatePrerequisite adds a base prerequisite to an elective.
func (s *AdminService) CreatePrerequisite(ctx context.Context, in CreatePrerequisiteInput) (domain.Prerequisite, error) {
	if in.Name == "" {
		return domain.Prerequisite{}, ErrAdminInvalid
	}
	return s.repo.CreatePrerequisite(ctx, in)
}

// CreateOffering offers an elective in a semester, returning the offering id.
func (s *AdminService) CreateOffering(ctx context.Context, in CreateOfferingInput) (string, error) {
	if in.SemesterID == "" || in.ElectiveID == "" {
		return "", ErrAdminInvalid
	}
	return s.repo.CreateOffering(ctx, in)
}

// CreateOfferingPrereq adds an offering-specific prerequisite.
func (s *AdminService) CreateOfferingPrereq(ctx context.Context, in CreateOfferingPrereqInput) (domain.Prerequisite, error) {
	if in.Name == "" {
		return domain.Prerequisite{}, ErrAdminInvalid
	}
	if in.Source == "" {
		in.Source = "OFFERING_SPECIFIC"
	}
	return s.repo.CreateOfferingPrereq(ctx, in)
}

// CreateGroup creates an offering group (a reason is required, BR for audit).
func (s *AdminService) CreateGroup(ctx context.Context, in CreateGroupInput) (domain.Group, error) {
	if in.GroupCode == "" || in.ScheduleText == "" || in.Capacity < 0 || len(in.Reason) < 3 || !in.Shift.Valid() {
		return domain.Group{}, ErrAdminInvalid
	}
	return s.repo.CreateGroup(ctx, in)
}

// UpdateGroup updates an offering group.
func (s *AdminService) UpdateGroup(ctx context.Context, in UpdateGroupInput) (domain.Group, error) {
	if len(in.Reason) < 3 {
		return domain.Group{}, ErrAdminInvalid
	}
	return s.repo.UpdateGroup(ctx, in)
}

// AdjustCapacity changes a group's capacity with a mandatory reason and audit.
func (s *AdminService) AdjustCapacity(ctx context.Context, in AdjustCapacityInput) (domain.Group, error) {
	if in.NewCapacity < 0 || len(in.Reason) < 3 {
		return domain.Group{}, ErrAdminInvalid
	}
	return s.repo.AdjustCapacity(ctx, in)
}
