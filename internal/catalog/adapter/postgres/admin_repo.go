package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/catalog/app"
	"github.com/uniquindio/profundiza-uq/internal/catalog/domain"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// AdminRepo implements the catalog write port.
type AdminRepo struct{ pool *pgxpool.Pool }

// NewAdminRepo builds a catalog AdminRepo.
func NewAdminRepo(pool *pgxpool.Pool) *AdminRepo { return &AdminRepo{pool: pool} }

// asDuplicate maps a unique-constraint violation (e.g. the partial unique index
// on an elective/offering prerequisite name) to the app's duplicate sentinel so
// the HTTP layer can answer 409 instead of a raw 500. Other errors pass through.
func asDuplicate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return app.ErrAdminDuplicate
	}
	return err
}

// ListElectives lists electives filtered by an optional name fragment and area.
func (r *AdminRepo) ListElectives(ctx context.Context, q, area string) ([]domain.Elective, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, area, description, status, created_at, updated_at
		   FROM electives
		  WHERE ($1='' OR name ILIKE '%'||$1||'%')
		    AND ($2='' OR area = $2)
		  ORDER BY name`, q, area)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Elective{}
	for rows.Next() {
		var e domain.Elective
		if err := rows.Scan(&e.ID, &e.Name, &e.Area, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetElective returns one elective or nil.
func (r *AdminRepo) GetElective(ctx context.Context, id string) (*domain.Elective, error) {
	var e domain.Elective
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, area, description, status, created_at, updated_at FROM electives WHERE id=$1`, id,
	).Scan(&e.ID, &e.Name, &e.Area, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateElective inserts an elective and audits it.
func (r *AdminRepo) CreateElective(ctx context.Context, in app.CreateElectiveInput) (domain.Elective, error) {
	var e domain.Elective
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO electives (name, area, description) VALUES ($1,$2,$3)
			 RETURNING id, name, area, description, status, created_at, updated_at`,
			in.Name, in.Area, in.Description,
		).Scan(&e.ID, &e.Name, &e.Area, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "ELECTIVE_CREATED", EntityType: "Elective", EntityID: e.ID, NewValue: map[string]any{"name": e.Name}})
	})
	return e, err
}

// UpdateElective updates the supplied fields (COALESCE keeps unset fields).
func (r *AdminRepo) UpdateElective(ctx context.Context, in app.UpdateElectiveInput) (domain.Elective, error) {
	var e domain.Elective
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE electives SET
			   name = COALESCE($2, name), area = COALESCE($3, area),
			   description = COALESCE($4, description), status = COALESCE($5, status), updated_at = now()
			 WHERE id = $1
			 RETURNING id, name, area, description, status, created_at, updated_at`,
			in.ID, in.Name, in.Area, in.Description, in.Status)
		if err := row.Scan(&e.ID, &e.Name, &e.Area, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "ELECTIVE_UPDATED", EntityType: "Elective", EntityID: e.ID})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Elective{}, app.ErrAdminNotFound
	}
	return e, err
}

// ListElectivePrerequisites returns an elective's base prerequisites.
func (r *AdminRepo) ListElectivePrerequisites(ctx context.Context, electiveID string) ([]domain.Prerequisite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, description, plan_type, status FROM prerequisites
		  WHERE elective_id=$1 AND status='ACTIVE' ORDER BY name`, electiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Prerequisite{}
	for rows.Next() {
		var p domain.Prerequisite
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.PlanType, &p.Status); err != nil {
			return nil, err
		}
		p.Source = "ELECTIVE_DEFAULT"
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePrerequisite adds a base prerequisite to an elective.
func (r *AdminRepo) CreatePrerequisite(ctx context.Context, in app.CreatePrerequisiteInput) (domain.Prerequisite, error) {
	var p domain.Prerequisite
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO prerequisites (elective_id, name, description, plan_type) VALUES ($1,$2,$3,$4)
			 RETURNING id, name, description, plan_type, status`,
			in.ElectiveID, in.Name, in.Description, in.PlanType,
		).Scan(&p.ID, &p.Name, &p.Description, &p.PlanType, &p.Status); err != nil {
			return err
		}
		p.Source = "ELECTIVE_DEFAULT"
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "PREREQUISITE_CREATED", EntityType: "Prerequisite", EntityID: p.ID})
	})
	return p, asDuplicate(err)
}

// CreateOffering offers an elective in a semester and returns the offering id.
func (r *AdminRepo) CreateOffering(ctx context.Context, in app.CreateOfferingInput) (string, error) {
	var id string
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO elective_offerings (semester_id, elective_id) VALUES ($1,$2) RETURNING id`,
			in.SemesterID, in.ElectiveID,
		).Scan(&id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "OFFERING_CREATED", EntityType: "ElectiveOffering", EntityID: id,
			NewValue: map[string]any{"semesterId": in.SemesterID, "electiveId": in.ElectiveID}})
	})
	return id, err
}

// CreateOfferingPrereq adds an offering-specific prerequisite.
func (r *AdminRepo) CreateOfferingPrereq(ctx context.Context, in app.CreateOfferingPrereqInput) (domain.Prerequisite, error) {
	var p domain.Prerequisite
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO offering_prerequisites (offering_id, name, description, plan_type, source)
			 VALUES ($1,$2,$3,$4,$5)
			 RETURNING id, offering_id, name, description, plan_type, source, status`,
			in.OfferingID, in.Name, in.Description, in.PlanType, in.Source,
		).Scan(&p.ID, &p.OfferingID, &p.Name, &p.Description, &p.PlanType, &p.Source, &p.Status); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "OFFERING_PREREQUISITE_CREATED", EntityType: "OfferingPrerequisite", EntityID: p.ID})
	})
	return p, asDuplicate(err)
}

// CreateGroup creates an offering group with a mandatory audited reason.
func (r *AdminRepo) CreateGroup(ctx context.Context, in app.CreateGroupInput) (domain.Group, error) {
	var g domain.Group
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO offering_groups (offering_id, group_code, shift, teacher_name, schedule_text, capacity)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING id, offering_id, group_code, shift, teacher_name, schedule_text, capacity, status, created_at, updated_at`,
			in.OfferingID, in.GroupCode, string(in.Shift), in.TeacherName, in.ScheduleText, in.Capacity,
		).Scan(&g.ID, &g.OfferingID, &g.GroupCode, &g.Shift, &g.TeacherName, &g.ScheduleText, &g.Capacity,
			&g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "GROUP_CREATED", EntityType: "OfferingGroup", EntityID: g.ID,
			NewValue: map[string]any{"groupCode": g.GroupCode, "capacity": g.Capacity}, Reason: in.Reason})
	})
	return g, err
}

// UpdateGroup updates an offering group's editable fields.
func (r *AdminRepo) UpdateGroup(ctx context.Context, in app.UpdateGroupInput) (domain.Group, error) {
	var g domain.Group
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE offering_groups SET
			   group_code = COALESCE($2, group_code), teacher_name = COALESCE($3, teacher_name),
			   schedule_text = COALESCE($4, schedule_text), status = COALESCE($5, status), updated_at = now()
			 WHERE id = $1
			 RETURNING id, offering_id, group_code, shift, teacher_name, schedule_text, capacity, status, created_at, updated_at`,
			in.ID, in.GroupCode, in.TeacherName, in.ScheduleText, in.Status)
		if err := row.Scan(&g.ID, &g.OfferingID, &g.GroupCode, &g.Shift, &g.TeacherName, &g.ScheduleText,
			&g.Capacity, &g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "GROUP_UPDATED", EntityType: "OfferingGroup", EntityID: g.ID, Reason: in.Reason})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Group{}, app.ErrAdminNotFound
	}
	return g, err
}

// AdjustCapacity changes a group's capacity, recording the adjustment and audit.
func (r *AdminRepo) AdjustCapacity(ctx context.Context, in app.AdjustCapacityInput) (domain.Group, error) {
	var g domain.Group
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		var prev int
		if err := tx.QueryRow(ctx, `SELECT capacity FROM offering_groups WHERE id=$1 FOR UPDATE`, in.GroupID).Scan(&prev); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`UPDATE offering_groups SET capacity=$2, updated_at=now() WHERE id=$1
			 RETURNING id, offering_id, group_code, shift, teacher_name, schedule_text, capacity, status, created_at, updated_at`,
			in.GroupID, in.NewCapacity,
		).Scan(&g.ID, &g.OfferingID, &g.GroupCode, &g.Shift, &g.TeacherName, &g.ScheduleText, &g.Capacity,
			&g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_capacity_adjustments (offering_group_id, admin_user_id, previous_capacity, new_capacity, reason)
			 VALUES ($1,$2,$3,$4,$5)`, in.GroupID, in.ActorID, prev, in.NewCapacity, in.Reason); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
			Action: "GROUP_CAPACITY_ADJUSTED", EntityType: "OfferingGroup", EntityID: g.ID,
			PreviousValue: map[string]any{"capacity": prev}, NewValue: map[string]any{"capacity": in.NewCapacity}, Reason: in.Reason})
	})
	return g, err
}

// inTx runs fn inside a transaction.
func (r *AdminRepo) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
