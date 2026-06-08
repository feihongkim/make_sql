#!/bin/bash
# Nginx geo_blocklist.conf 자동 업데이트
# 공개 위협 IP 인텔리전스 소스에서 IP/CIDR를 받아 신규 항목만 추가 후 geo 파일 재생성
#
# 사용법:
#   ./update_blocklist.sh            # 업데이트 후 Nginx 재로드
#   ./update_blocklist.sh --dry-run  # 추가될 항목만 출력 (실제 적용 안 함)

set -euo pipefail

SSH_KEY="$HOME/.ssh/moodle.pem"
REMOTE_HOST="ubuntu@3.34.223.162"
BLOCKLIST_REMOTE="/etc/nginx/conf.d/geo_blocklist.conf"
BLOCKLIST_LOCAL="/tmp/blocklist_current.conf"
OUTPUT_LOCAL="/tmp/blocklist_new_entries.conf"
DRY_RUN=false

[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true

SSH="ssh -i $SSH_KEY -o ConnectTimeout=10 -o StrictHostKeyChecking=no $REMOTE_HOST"

log() { echo "[$(date '+%H:%M:%S')] $*"; }

# 현재 geo_blocklist.conf 가져오기
log "현재 geo_blocklist.conf 수신 중..."
$SSH "cat $BLOCKLIST_REMOTE" > "$BLOCKLIST_LOCAL"

# 기존 차단 IP/CIDR 파싱 (geo 형식: "1.2.3.4 1;")
python3 << 'PYEOF'
import re, ipaddress, sys, json

with open("/tmp/blocklist_current.conf") as f:
    content = f.read()

blocked_ips = set(re.findall(r'^\s*([\d.]+)\s+1;', content, re.MULTILINE))
blocked_cidrs = []
for cidr in re.findall(r'^\s*([\d.]+/\d+)\s+1;', content, re.MULTILINE):
    try:
        blocked_cidrs.append(ipaddress.ip_network(cidr, strict=False))
    except ValueError:
        pass

with open("/tmp/blocked_existing.json", "w") as f:
    json.dump({"ips": list(blocked_ips), "cidrs": [str(c) for c in blocked_cidrs]}, f)

print(f"  기존 차단: IP {len(blocked_ips)}개, CIDR {len(blocked_cidrs)}개")
PYEOF

# 위협 인텔리전스 소스 다운로드 및 파싱
log "위협 인텔리전스 다운로드 중..."

python3 << 'PYEOF'
import urllib.request, re, ipaddress, json, sys
from collections import defaultdict

with open("/tmp/blocked_existing.json") as f:
    existing = json.load(f)
blocked_ips = set(existing["ips"])
blocked_cidrs = [ipaddress.ip_network(c) for c in existing["cidrs"]]

def is_blocked(addr_str):
    if addr_str in blocked_ips:
        return True
    try:
        addr = ipaddress.ip_address(addr_str)
        return any(addr in cidr for cidr in blocked_cidrs)
    except ValueError:
        return False

def is_cidr_blocked(cidr_str):
    try:
        net = ipaddress.ip_network(cidr_str, strict=False)
        # 완전히 같은 CIDR이 있으면 차단됨
        return str(net) in {str(c) for c in blocked_cidrs}
    except ValueError:
        return False

def fetch(url, timeout=15):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "curl/7.88"})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.read().decode("utf-8", errors="ignore")
    except Exception as e:
        print(f"  [SKIP] {url}: {e}", file=sys.stderr)
        return ""

# 소스별 파싱 결과 수집
new_entries = defaultdict(list)  # {source: [(ip_or_cidr, comment)]}

# 1. IPsum (신뢰도 높은 IP, 등장 횟수 3+ 필터)
log_msg = "IPsum (stamparm/ipsum)"
print(f"  [{log_msg}]")
text = fetch("https://raw.githubusercontent.com/stamparm/ipsum/master/ipsum.txt")
count = 0
for line in text.splitlines():
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    parts = line.split("\t")
    if len(parts) >= 2:
        ip, score = parts[0], parts[1]
        if int(score) >= 5 and not is_blocked(ip):  # 신뢰도 5+ 만 적용
            new_entries["ipsum"].append((ip, f"IPsum score={score}"))
            count += 1
print(f"    신규: {count}개")

# 2. Feodo Tracker (C&C 서버)
log_msg = "Feodo Tracker C&C"
print(f"  [{log_msg}]")
text = fetch("https://feodotracker.abuse.ch/downloads/ipblocklist.txt")
count = 0
for line in text.splitlines():
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    ip = line.split(",")[0] if "," in line else line
    ip = ip.strip()
    if re.match(r'^\d+\.\d+\.\d+\.\d+$', ip) and not is_blocked(ip):
        new_entries["feodo"].append((ip, "Feodo C&C 서버"))
        count += 1
print(f"    신규: {count}개")

# 3. Emerging Threats
log_msg = "Emerging Threats"
print(f"  [{log_msg}]")
text = fetch("https://rules.emergingthreats.net/fwrules/emerging-Block-IPs.txt")
count = 0
for line in text.splitlines():
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    if re.match(r'^\d+\.\d+\.\d+\.\d+(/\d+)?$', line):
        if "/" in line:
            if not is_cidr_blocked(line):
                new_entries["emerging_cidr"].append((line, "Emerging Threats"))
                count += 1
        else:
            if not is_blocked(line):
                new_entries["emerging"].append((line, "Emerging Threats"))
                count += 1
print(f"    신규: {count}개")

# 4. Spamhaus DROP (CIDR)
log_msg = "Spamhaus DROP"
print(f"  [{log_msg}]")
text = fetch("https://www.spamhaus.org/drop/drop.txt")
count = 0
for line in text.splitlines():
    line = line.strip()
    if not line or line.startswith(";"):
        continue
    cidr = line.split(";")[0].strip()
    if re.match(r'^\d+\.\d+\.\d+\.\d+/\d+$', cidr):
        if not is_cidr_blocked(cidr):
            new_entries["spamhaus"].append((cidr, "Spamhaus DROP"))
            count += 1
print(f"    신규: {count}개")

# 결과 저장
with open("/tmp/new_threat_entries.json", "w") as f:
    json.dump(dict(new_entries), f)

total = sum(len(v) for v in new_entries.values())
print(f"\n  총 신규 항목: {total}개")
PYEOF

# geo_blocklist.conf에 추가할 새 항목 생성 (geo 형식)
python3 << 'PYEOF'
import json
from datetime import datetime

with open("/tmp/new_threat_entries.json") as f:
    entries = json.load(f)

today = datetime.now().strftime("%Y-%m-%d")
lines = [f"\n    # --- {today} 자동 업데이트 (위협 인텔리전스) ---"]

source_labels = {
    "ipsum":         "IPsum",
    "feodo":         "Feodo Tracker (C&C)",
    "emerging":      "Emerging Threats",
    "emerging_cidr": "Emerging Threats (CIDR)",
    "spamhaus":      "Spamhaus DROP",
}

total = 0
for source, items in entries.items():
    if not items:
        continue
    label = source_labels.get(source, source)
    lines.append(f"\n    # [{label}]")
    for addr, comment in items:
        lines.append(f"    {addr} 1;  # {comment}")
        total += 1

with open("/tmp/blocklist_new_entries.conf", "w") as f:
    f.write("\n".join(lines) + "\n")

print(f"추가 예정: {total}개 항목")
PYEOF

TOTAL=$(grep -c "^\s*[0-9].*1;" /tmp/blocklist_new_entries.conf 2>/dev/null || echo 0)

if [[ "$TOTAL" -eq 0 ]]; then
    log "신규 차단 항목 없음 — blocklist.conf 최신 상태"
    exit 0
fi

log "신규 항목 $TOTAL개 발견"

if $DRY_RUN; then
    log "[DRY-RUN] 추가될 항목:"
    cat /tmp/blocklist_new_entries.conf
    exit 0
fi

# 신규 항목을 원격으로 전송 후 geo_blocklist.conf 전체 재생성
log "geo_blocklist.conf 업데이트 중..."
$SSH "sudo tee /tmp/new_geo_entries.conf" < /tmp/blocklist_new_entries.conf > /dev/null
$SSH "sudo python3 << 'PYEOF'
import re

with open('$BLOCKLIST_REMOTE') as f:
    existing = f.read()

with open('/tmp/new_geo_entries.conf') as f:
    new_entries = f.read()

# 기존 항목 수집
seen = set()
for m in re.finditer(r'^\s*([\d./]+)\s+1;', existing, re.MULTILINE):
    seen.add(m.group(1))

# 신규 항목 중 중복 제거
added = 0
new_lines = []
for line in new_entries.splitlines():
    m = re.match(r'^\s*([\d./]+)\s+1;', line)
    if m:
        if m.group(1) in seen:
            continue
        seen.add(m.group(1))
        added += 1
    new_lines.append(line)

# geo 블록 닫기 전에 삽입하여 재생성
updated = existing.rstrip().rstrip('}').rstrip() + '\n' + '\n'.join(new_lines) + '\n}\n'

with open('$BLOCKLIST_REMOTE', 'w') as f:
    f.write(updated)

print(f'신규 {added}개 추가 (중복 제거 후)')
PYEOF"

# Nginx 설정 검증 및 재로드
log "Nginx 설정 검증..."
$SSH "sudo nginx -t" && \
    $SSH "sudo systemctl reload nginx" && \
    log "Nginx 재로드 완료" || \
    { log "오류: Nginx 설정 오류 — 재로드 중단"; exit 1; }

log "완료: 신규 항목 geo_blocklist.conf 반영됨"
