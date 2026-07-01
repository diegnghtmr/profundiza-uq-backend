package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"

	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// DefaultBaseDir is the output directory used when an empty base path is given.
const DefaultBaseDir = "./reports-output"

// Generator implements the reporting Generator port: it fetches the report data
// and writes an XLSX or PDF file into the configured base directory.
type Generator struct {
	data    DataSource
	baseDir string
}

// NewGenerator wires a Generator with its data source and output base
// directory. An empty baseDir falls back to DefaultBaseDir.
func NewGenerator(data DataSource, baseDir string) *Generator {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = DefaultBaseDir
	}
	return &Generator{data: data, baseDir: baseDir}
}

// Generate fetches the report rows and writes the file, returning its path. The
// worker records this path on the export when it marks the job COMPLETED.
func (g *Generator) Generate(ctx context.Context, e domain.ReportExport) (string, error) {
	table, err := g.data.Fetch(ctx, e)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(g.baseDir, 0o750); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.%s",
		strings.ToLower(string(e.ReportType)), e.ID, e.FileExtension())
	path := filepath.Join(g.baseDir, filename)

	switch e.Format {
	case domain.FormatPDF:
		err = writePDF(path, table)
	default:
		err = writeXLSX(path, table)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// writeXLSX renders the table into an .xlsx workbook: a bold title row, a bold
// header row, and one row per record.
func writeXLSX(path string, table Table) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "Report"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return fmt.Errorf("create sheet: %w", err)
	}
	f.SetActiveSheet(idx)
	f.DeleteSheet("Sheet1")

	boldStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return fmt.Errorf("create style: %w", err)
	}

	// Row 1: title.
	if err := f.SetCellValue(sheet, "A1", table.Title); err != nil {
		return err
	}
	_ = f.SetCellStyle(sheet, "A1", "A1", boldStyle)

	// Row 2: header.
	for i, col := range table.Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		if err := f.SetCellValue(sheet, cell, col); err != nil {
			return err
		}
		_ = f.SetCellStyle(sheet, cell, cell, boldStyle)
	}

	// Rows 3..n: data.
	for r, row := range table.Rows {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+3)
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return err
			}
		}
	}

	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("write xlsx: %w", err)
	}
	return nil
}

// writePDF renders the table into a simple landscape PDF: a title and a bordered
// grid. Cell text is translated from UTF-8 to the core-font encoding so accented
// names render correctly.
func writePDF(path string, table Table) error {
	pdf := fpdf.New("L", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("") // UTF-8 -> cp1252
	pdf.SetMargins(10, 10, 10)
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(0, 10, tr(table.Title), "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pageW, _ := pdf.GetPageSize()
	usableW := pageW - 20 // left+right margins
	colW := usableW
	if len(table.Columns) > 0 {
		colW = usableW / float64(len(table.Columns))
	}

	// Header.
	pdf.SetFont("Arial", "B", 8)
	pdf.SetFillColor(230, 230, 230)
	for _, col := range table.Columns {
		pdf.CellFormat(colW, 7, tr(truncate(col, colW)), "1", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	// Data rows.
	pdf.SetFont("Arial", "", 7)
	for _, row := range table.Rows {
		for _, val := range row {
			pdf.CellFormat(colW, 6, tr(truncate(val, colW)), "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}

	if len(table.Rows) == 0 {
		pdf.SetFont("Arial", "I", 9)
		pdf.CellFormat(0, 8, tr("No records found."), "", 1, "L", false, 0, "")
	}

	if err := pdf.OutputFileAndClose(path); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}
	return nil
}

// truncate clips a cell's text to roughly fit the given column width (in mm),
// appending an ellipsis when shortened, so the grid stays aligned.
func truncate(s string, colWidthMM float64) string {
	// ~1.9mm per character at the 7-8pt body font.
	max := int(colWidthMM / 1.9)
	if max < 4 {
		max = 4
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
