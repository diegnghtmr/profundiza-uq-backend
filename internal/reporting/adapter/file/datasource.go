// Package file is the driven adapter implementing the reporting Generator port.
// It queries the report data from Postgres and renders XLSX or PDF files to a
// configurable output directory.
package file

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// Table is a format-agnostic tabular dataset: a titled grid of string cells.
// The XLSX and PDF renderers turn it into the corresponding file format.
type Table struct {
	Title   string
	Columns []string
	Rows    [][]string
}

// DataSource fetches the rows backing a report export.
type DataSource interface {
	Fetch(ctx context.Context, e domain.ReportExport) (Table, error)
}

// PostgresData is a pgx-backed DataSource holding the SQL for every report type.
type PostgresData struct {
	pool *pgxpool.Pool
}

// NewPostgresData builds a DataSource over a pgx pool.
func NewPostgresData(pool *pgxpool.Pool) *PostgresData {
	return &PostgresData{pool: pool}
}

// querySpec describes the SQL and presentation for one report type.
type querySpec struct {
	title   string
	columns []string
	sql     string
}

// specFor returns the query specification for the report type, falling back to
// a generic semester listing for types without a dedicated query.
func specFor(t domain.ReportType) querySpec {
	if spec, ok := reportSpecs[t]; ok {
		return spec
	}
	return reportSpecs[domain.ReportGeneralSemester]
}

// requestColumns are shared by the request-listing reports.
var requestColumns = []string{
	"Student", "Email", "Document", "Student Shift", "Elective", "Group",
	"Group Shift", "Status", "Priority Group", "Arrival #", "Submitted At",
}

// requestSelect is the shared SELECT list / FROM for request-listing reports.
const requestSelect = `
	SELECT s.full_name, s.institutional_email, s.document_number, er.student_shift,
	       el.name, g.group_code, g.shift, er.status, er.priority_group,
	       er.arrival_sequence, er.submitted_at
	FROM enrollment_requests er
	JOIN students s            ON s.id = er.student_id
	JOIN elective_offerings eo ON eo.id = er.offering_id
	JOIN electives el          ON el.id = eo.elective_id
	JOIN offering_groups g     ON g.id = er.offering_group_id
	WHERE er.semester_id = $1`

// reportSpecs maps each specialized report type to its query.
var reportSpecs = map[domain.ReportType]querySpec{
	domain.ReportGeneralSemester: {
		title:   "General Semester Report",
		columns: requestColumns,
		sql:     requestSelect + ` ORDER BY el.name, g.group_code, er.arrival_sequence`,
	},
	domain.ReportAcceptedRequests: {
		title:   "Accepted Requests",
		columns: requestColumns,
		sql:     requestSelect + ` AND er.status = 'ACCEPTED' ORDER BY el.name, g.group_code, er.arrival_sequence`,
	},
	domain.ReportRejectedRequests: {
		title:   "Rejected Requests",
		columns: requestColumns,
		sql:     requestSelect + ` AND er.status = 'REJECTED' ORDER BY el.name, g.group_code, er.arrival_sequence`,
	},
	domain.ReportCancelledRequests: {
		title:   "Cancelled Requests",
		columns: requestColumns,
		sql:     requestSelect + ` AND er.status IN ('CANCELLED_BY_STUDENT','CANCELLED_BY_ADMIN') ORDER BY el.name, g.group_code, er.arrival_sequence`,
	},
	domain.ReportWaitlist: {
		title:   "Waitlist",
		columns: requestColumns,
		// Waitlisted requests ordered by priority group, then official arrival order.
		sql: requestSelect + ` AND er.status IN ('WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT')
			ORDER BY el.name, g.group_code, er.priority_group, er.arrival_sequence`,
	},
	domain.ReportByGroup: {
		title: "Requests by Group",
		columns: []string{
			"Elective", "Group", "Shift", "Teacher", "Schedule", "Capacity",
			"Accepted", "Waitlisted", "Pending",
		},
		sql: `
			SELECT el.name, g.group_code, g.shift, COALESCE(g.teacher_name, ''), g.schedule_text, g.capacity,
			       COUNT(er.id) FILTER (WHERE er.status = 'ACCEPTED'),
			       COUNT(er.id) FILTER (WHERE er.status IN ('WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT')),
			       COUNT(er.id) FILTER (WHERE er.status IN ('SUBMITTED','PENDING_REVIEW'))
			FROM offering_groups g
			JOIN elective_offerings eo ON eo.id = g.offering_id
			JOIN electives el          ON el.id = eo.elective_id
			LEFT JOIN enrollment_requests er ON er.offering_group_id = g.id
			WHERE eo.semester_id = $1
			GROUP BY g.id, el.name, g.group_code, g.shift, g.teacher_name, g.schedule_text, g.capacity
			ORDER BY el.name, g.group_code`,
	},
	domain.ReportCapacity: {
		title: "Group Capacity",
		columns: []string{
			"Elective", "Group", "Shift", "Capacity", "Accepted", "Available", "Utilization %",
		},
		sql: `
			SELECT el.name, g.group_code, g.shift, g.capacity,
			       COUNT(er.id) FILTER (WHERE er.status = 'ACCEPTED') AS accepted,
			       g.capacity - COUNT(er.id) FILTER (WHERE er.status = 'ACCEPTED') AS available,
			       CASE WHEN g.capacity = 0 THEN 0
			            ELSE ROUND(100.0 * COUNT(er.id) FILTER (WHERE er.status = 'ACCEPTED') / g.capacity, 1)
			       END AS utilization
			FROM offering_groups g
			JOIN elective_offerings eo ON eo.id = g.offering_id
			JOIN electives el          ON el.id = eo.elective_id
			LEFT JOIN enrollment_requests er ON er.offering_group_id = g.id
			WHERE eo.semester_id = $1
			GROUP BY g.id, el.name, g.group_code, g.shift, g.capacity
			ORDER BY el.name, g.group_code`,
	},
	domain.ReportAdminReview: {
		title: "Admin Review Decisions",
		columns: []string{
			"Decided At", "Admin", "Decision", "Student", "Elective", "Group",
			"Previous Status", "New Status", "Reason",
		},
		sql: `
			SELECT d.created_at, au.full_name, d.decision_type, s.full_name, el.name, g.group_code,
			       d.previous_status, d.new_status, d.reason
			FROM enrollment_decisions d
			JOIN admin_users au        ON au.id = d.admin_user_id
			JOIN enrollment_requests er ON er.id = d.enrollment_request_id
			JOIN students s            ON s.id = er.student_id
			JOIN elective_offerings eo ON eo.id = er.offering_id
			JOIN electives el          ON el.id = eo.elective_id
			JOIN offering_groups g     ON g.id = er.offering_group_id
			WHERE er.semester_id = $1
			ORDER BY d.created_at DESC`,
	},
}

// Fetch runs the report's query for the export's semester and returns the rows
// as a format-agnostic Table.
func (d *PostgresData) Fetch(ctx context.Context, e domain.ReportExport) (Table, error) {
	spec := specFor(e.ReportType)
	semesterID := ""
	if e.SemesterID != nil {
		semesterID = *e.SemesterID
	}

	rows, err := d.pool.Query(ctx, spec.sql, semesterID)
	if err != nil {
		return Table{}, fmt.Errorf("query report data: %w", err)
	}
	defer rows.Close()

	table := Table{Title: spec.title, Columns: spec.columns}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return Table{}, fmt.Errorf("scan report row: %w", err)
		}
		cells := make([]string, len(values))
		for i, v := range values {
			cells[i] = formatCell(v)
		}
		table.Rows = append(table.Rows, cells)
	}
	if err := rows.Err(); err != nil {
		return Table{}, fmt.Errorf("iterate report rows: %w", err)
	}
	return table, nil
}

// formatCell renders a single database value as a presentation string.
// Timestamps are normalized to UTC RFC3339; nulls become empty strings.
func formatCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
