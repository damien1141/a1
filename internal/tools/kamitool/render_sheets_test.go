package kamitool

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"baliance.com/gooxml/presentation"
	"baliance.com/gooxml/spreadsheet"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sheetsFixtureContent = `# Summary

Intro paragraph.

- first bullet
- second bullet

| Name | Qty | Price |
|------|-----|-------|
| bolts | 12 | 1.20 |
| nuts | 8 | 0.40 |

## Details

Body of the details section.
`

func TestRenderKamiXLSX(t *testing.T) {
	sections := parseMarkdownContent(sheetsFixtureContent)
	require.NotEmpty(t, sections)

	path := filepath.Join(t.TempDir(), "proposal.xlsx")
	require.NoError(t, renderKamiXLSX(docProposal, "Quarterly Plan", sections, sheetsFixtureContent, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0), "xlsx file is non-empty")

	// The file must open as a valid workbook.
	wb, err := spreadsheet.Open(path)
	require.NoError(t, err, "gooxml must read back its own output")
	require.NotEmpty(t, wb.Sheets())
}

func TestRenderKamiPPTX(t *testing.T) {
	sections := parseMarkdownContent(sheetsFixtureContent)
	path := filepath.Join(t.TempDir(), "proposal.pptx")
	require.NoError(t, renderKamiPPTX(docProposal, "Quarterly Plan", sections, sheetsFixtureContent, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0), "pptx file is non-empty")

	ppt, err := presentation.Open(path)
	require.NoError(t, err, "gooxml must read back its own output")
	assert.NotNil(t, ppt)
}

func TestRenderKamiCSV(t *testing.T) {
	sections := parseMarkdownContent(sheetsFixtureContent)
	path := filepath.Join(t.TempDir(), "proposal.csv")
	require.NoError(t, renderKamiCSV(docProposal, "Quarterly Plan", sections, sheetsFixtureContent, path))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	// Prose rows and table rows have different widths; the file is a
	// document, not a strict grid.
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	assert.Equal(t, []string{"PROPOSAL"}, rows[0])
	assert.Equal(t, []string{"Quarterly Plan"}, rows[1])

	// The pipe table becomes real CSV rows.
	var headerRow, boltsRow bool
	for _, r := range rows {
		if len(r) >= 3 && r[0] == "Name" && r[1] == "Qty" {
			headerRow = true
		}
		if len(r) >= 3 && r[0] == "bolts" && r[1] == "12" {
			boltsRow = true
		}
	}
	assert.True(t, headerRow, "table header is a CSV row")
	assert.True(t, boltsRow, "table data rows are CSV rows")
}

func TestParseMarkdownTables(t *testing.T) {
	tables := parseMarkdownTables(sheetsFixtureContent)
	require.Len(t, tables, 1)
	assert.Equal(t, []string{"Name", "Qty", "Price"}, tables[0].Header)
	require.Len(t, tables[0].Rows, 2)
	assert.Equal(t, []string{"bolts", "12", "1.20"}, tables[0].Rows[0])
}

func TestParseMarkdownTablesSeparatorOnly(t *testing.T) {
	assert.Empty(t, parseMarkdownTables("|---|---|\n|---|---|"))
	assert.Empty(t, parseMarkdownTables("no tables here"))
}

func TestDocumentToolFormatsDispatch(t *testing.T) {
	// The error message enumerates every format the switch knows.
	_, err := runDocument(nil, []byte(`{"type":"onepager","title":"T","content":"body","format":"rtf"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pdf, docx, pptx, xlsx, or csv")
}
