package domain

import (
	"errors"
	"testing"
)

func strptr(s string) *string { return &s }

func TestReportExportValidate(t *testing.T) {
	tests := []struct {
		name    string
		export  ReportExport
		wantErr error
	}{
		{
			name: "valid request",
			export: ReportExport{
				ReportType: ReportAcceptedRequests,
				Format:     FormatXLSX,
				SemesterID: strptr("sem-1"),
			},
			wantErr: nil,
		},
		{
			name: "unknown report type",
			export: ReportExport{
				ReportType: ReportType("NOPE"),
				Format:     FormatPDF,
				SemesterID: strptr("sem-1"),
			},
			wantErr: ErrInvalidReportType,
		},
		{
			name: "unknown format",
			export: ReportExport{
				ReportType: ReportWaitlist,
				Format:     Format("CSV"),
				SemesterID: strptr("sem-1"),
			},
			wantErr: ErrInvalidFormat,
		},
		{
			name: "missing semester (nil)",
			export: ReportExport{
				ReportType: ReportCapacity,
				Format:     FormatXLSX,
				SemesterID: nil,
			},
			wantErr: ErrSemesterRequired,
		},
		{
			name: "missing semester (empty)",
			export: ReportExport{
				ReportType: ReportCapacity,
				Format:     FormatXLSX,
				SemesterID: strptr(""),
			},
			wantErr: ErrSemesterRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.export.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestFileExtensionAndContentType(t *testing.T) {
	xlsx := ReportExport{Format: FormatXLSX}
	if got := xlsx.FileExtension(); got != "xlsx" {
		t.Errorf("xlsx FileExtension = %q, want xlsx", got)
	}
	if got := xlsx.ContentType(); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("xlsx ContentType = %q", got)
	}

	pdf := ReportExport{Format: FormatPDF}
	if got := pdf.FileExtension(); got != "pdf" {
		t.Errorf("pdf FileExtension = %q, want pdf", got)
	}
	if got := pdf.ContentType(); got != "application/pdf" {
		t.Errorf("pdf ContentType = %q", got)
	}
}

func TestValidReportType(t *testing.T) {
	if !ValidReportType(ReportAudit) {
		t.Error("AUDIT should be a valid report type")
	}
	if ValidReportType(ReportType("BOGUS")) {
		t.Error("BOGUS should not be a valid report type")
	}
}
