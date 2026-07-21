"""nginx 접근/에러 로그 보안 분석 스크립트.

사용법:
    python3 analyze.py [--hours N] [access.log ...]
    python3 analyze.py --hours 2 access.log error.log
    python3 analyze.py          # 기본: 같은 폴더의 access.log (전체)
"""

import re
import sys
import gzip
import ipaddress
import json
import argparse
from pathlib import Path
from collections import defaultdict, Counter
from urllib.parse import urlparse, unquote
from datetime import datetime, timezone, timedelta

# ─── 서버 판별 ────────────────────────────────────────────────────
MOODLE_PATHS = re.compile(
    r"^/(lib/|course/|my/|mod/|user/|admin/|login/|blocks/|theme/|pluginfile|enrol|auth)"
)
BLOG_PATHS = re.compile(r"^/(blog/|_next/|_vercel/|favicon|sitemap|robots\.txt|rss)")

# ─── 봇 User-Agent ────────────────────────────────────────────────
BOT_UA_RE = re.compile(
    r"(bot|crawl|spider|slurp|baiduspider|yandex|semrush|ahrefs|mj12|dotbot|"
    r"petalbot|rogerbot|seznambot|archive\.org|facebookexternalhit|twitterbot|"
    r"linkedinbot|whatsapp|applebot|googlebot|bingbot|duckduck|python-requests|"
    r"curl|wget|go-http-client|java/|okhttp|libwww|zgrab|nmap|masscan|nuclei|"
    r"netcraft|shodan|censys|scan|nikto|sqlmap|dirbuster|burpsuite|libredtail|"
    r"GenomeCrawler|ivre|zgrab|masscan|zmap|xray|nuclei|httpx|whatweb)",
    re.IGNORECASE,
)

# ─── 보안 위협 패턴 ──────────────────────────────────────────────
SECURITY_PATTERNS: list[tuple[str, re.Pattern]] = [
    # 경로 순회
    ("PATH_TRAVERSAL",   re.compile(r"\.\./|\.\.\\|%2e%2e[%/\\]", re.IGNORECASE)),
    # SQL 인젝션
    ("SQL_INJECTION",    re.compile(
        r"(union(\s|\+|%20)+select|select.+from|insert\s+into|drop\s+table|"
        r"or\s+1=1|and\s+1=1|'--|\bexec\b|\bxp_cmdshell\b|information_schema)",
        re.IGNORECASE)),
    # XSS
    ("XSS",              re.compile(
        r"(<script|javascript:|on(load|error|click|mouseover)=|alert\(|"
        r"document\.cookie|eval\(|%3cscript)", re.IGNORECASE)),
    # Log4j / JNDI
    ("LOG4J_JNDI",       re.compile(r"\$\{.*jndi:", re.IGNORECASE)),
    # 파일 인클루전
    ("FILE_INCLUSION",   re.compile(
        r"(php://|file://|zip://|data://|phar://|expect://|"
        r"/etc/passwd|/etc/shadow|/proc/self|/var/log)", re.IGNORECASE)),
    # 명령 인젝션
    ("CMD_INJECTION",    re.compile(
        r"(;|\|{1,2}|&&|`|\$\()\s*(ls|cat|id|whoami|uname|wget|curl|bash|sh|"
        r"nc|netcat|python|perl|ruby|php)", re.IGNORECASE)),
    # 알려진 취약 경로 스캔
    ("EXPLOIT_SCAN",     re.compile(
        r"(wp-admin|wp-login|phpMyAdmin|phpmyadmin|adminer|\.env|\.git/|"
        r"\.aws/|config\.php|config\.yml|\.DS_Store|backup\.|\.bak|"
        r"\.sql|actuator/|\.well-known/acme|cgi-bin|shell\.php|"
        r"webshell|c99|r57|b374k|weevely|manager/html)", re.IGNORECASE)),
    # 웹쉘 업로드 시도
    ("WEBSHELL",         re.compile(
        r"\.(php|asp|aspx|jsp|cgi|pl|py|rb|sh)\?.*=(cmd|exec|system|passthru|"
        r"shell_exec|eval|base64_decode)", re.IGNORECASE)),
    # 무차별 대입 (admin/login 경로 POST)
    ("BRUTE_FORCE_PATH", re.compile(
        r"^/(wp-login|login|signin|admin/login|user/login|auth/login|"
        r"account/login|api/auth|api/login)", re.IGNORECASE)),
]

# nginx combined log 파싱 (X-Forwarded-For 필드 선택적 지원)
LOG_RE = re.compile(
    r'(?P<ip>\S+) \S+ \S+ \[(?P<time>[^\]]+)\] '
    r'"(?P<method>\S+) (?P<path>\S+) \S+" '
    r'(?P<status>\d{3}) (?P<bytes>\d+) '
    r'"(?P<referer>[^"]*)" "(?P<ua>[^"]*)"'
    r'(?: "(?P<xff>[^"]*)")?'
)
# nginx error log 파싱
ERROR_RE = re.compile(
    r'(?P<time>\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[(?P<level>\w+)\] '
    r'\S+ (?P<msg>.+?)(?:, client: (?P<ip>[0-9.:]+))?(?:, server: (?P<server>[^,]+))?'
    r'(?:, request: "(?P<req>[^"]*)")?'
)
# Rate Limit 위반: "limiting requests, excess: X.X by zone"
RATE_LIMIT_RE = re.compile(
    r'limiting requests, excess: (?P<excess>[\d.]+) by zone "(?P<zone>[^"]+)", '
    r'client: (?P<ip>[0-9.:]+), server: (?P<server>[^,]+), request: "(?P<req>[^"]*)"'
)
# Fail2Ban 로그 파싱
F2B_BAN_RE = re.compile(
    r'(?P<time>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),\d+ '
    r'fail2ban\.actions\s+\[.*?\]: (?P<action>NOTICE|WARNING)\s+\[(?P<jail>[^\]]+)\] '
    r'(?P<op>Ban|Unban) (?P<ip>\S+)'
)
TIME_FMT = "%d/%b/%Y:%H:%M:%S %z"
ERROR_TIME_FMT = "%Y/%m/%d %H:%M:%S"
F2B_TIME_FMT = "%Y-%m-%d %H:%M:%S"

# Next.js RSC 요청 패턴 (정상 사용자 Rate Limit 오탐 제외용)
NEXTJS_RSC_RE = re.compile(r'\?_rsc=')


def detect_server(path: str, referer: str) -> str:
    ref_host = ""
    if referer and referer != "-":
        try:
            ref_host = urlparse(referer).hostname or ""
        except Exception:
            pass
    if ref_host in ("tems.bnslab.biz", "lms.theoed.org"):
        return "moodle"
    if ref_host == "feivyblog.bnslab.biz":
        return "feivyblog"
    if ref_host in ("www.theoed.org", "theoed.org"):
        return "theoed"
    if ref_host in ("www.bnslab.biz", "bnslab.biz"):
        return "bnslab"
    if MOODLE_PATHS.match(path):
        return "moodle"
    if BLOG_PATHS.match(path):
        return "feivyblog"
    return "unknown"


def check_threats(path: str, ua: str) -> list[str]:
    decoded = unquote(path)
    threats = []
    for name, pattern in SECURITY_PATTERNS:
        if pattern.search(decoded) or pattern.search(ua):
            threats.append(name)
    return threats


def open_log(path: Path):
    if path.suffix == ".gz":
        return gzip.open(path, "rt", errors="replace")
    return open(path, "r", errors="replace")


def parse_access_logs(files: list[Path], since: datetime | None) -> list[dict]:
    records = []
    for f in files:
        if "error" in f.name:
            continue
        with open_log(f) as fh:
            for line in fh:
                m = LOG_RE.match(line.strip())
                if not m:
                    continue
                d = m.groupdict()
                try:
                    t = datetime.strptime(d["time"], TIME_FMT)
                except ValueError:
                    t = None
                if since and t and t < since:
                    continue
                d["dt"] = t
                # X-Forwarded-For가 있으면 실제 클라이언트 IP로 대체
                xff = (d.get("xff") or "").strip()
                if xff and xff != "-":
                    d["ip"] = xff.split(",")[0].strip()
                d["server"] = detect_server(d["path"], d["referer"])
                d["bot"] = bool(BOT_UA_RE.search(d["ua"]))
                d["status"] = int(d["status"])
                d["bytes"] = int(d["bytes"])
                d["threats"] = check_threats(d["path"], d["ua"])
                records.append(d)
    return records


def parse_fail2ban_logs(files: list[Path], since: datetime | None) -> list[dict]:
    records = []
    for f in files:
        if "fail2ban" not in f.name:
            continue
        with open_log(f) as fh:
            for line in fh:
                m = F2B_BAN_RE.search(line.strip())
                if not m:
                    continue
                d = m.groupdict()
                try:
                    t = datetime.strptime(d["time"], F2B_TIME_FMT).replace(tzinfo=timezone.utc)
                except ValueError:
                    t = None
                if since and t and t < since:
                    continue
                d["dt"] = t
                records.append(d)
    return records


def parse_error_logs(files: list[Path], since: datetime | None) -> tuple[list[dict], list[dict]]:
    """일반 에러 레코드와 Rate Limit 위반 레코드를 분리해서 반환."""
    errors = []
    rate_limits = []
    for f in files:
        if "error" not in f.name or "fail2ban" in f.name:
            continue
        with open_log(f) as fh:
            for line in fh:
                raw = line.strip()
                # Rate Limit 위반 먼저 체크
                rm = RATE_LIMIT_RE.search(raw)
                if rm:
                    d = rm.groupdict()
                    # 시간 파싱
                    tm = re.match(r'(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})', raw)
                    t = None
                    if tm:
                        try:
                            t = datetime.strptime(tm.group(1), ERROR_TIME_FMT).replace(tzinfo=timezone.utc)
                        except ValueError:
                            pass
                    if since and t and t < since:
                        continue
                    d["dt"] = t
                    d["excess"] = float(d["excess"])
                    # Next.js RSC 정상 요청 오탐 여부
                    d["false_positive"] = bool(NEXTJS_RSC_RE.search(d.get("req", "")))
                    rate_limits.append(d)
                    continue

                m = ERROR_RE.match(raw)
                if not m:
                    continue
                d = m.groupdict()
                try:
                    t = datetime.strptime(d["time"], ERROR_TIME_FMT).replace(tzinfo=timezone.utc)
                except ValueError:
                    t = None
                if since and t and t < since:
                    continue
                d["dt"] = t
                errors.append(d)
    return errors, rate_limits


def is_blocked_ip(ip: str, blocked_networks: list) -> bool:
    """geo blocklist의 개별 IP/CIDR에 포함되는지 확인한다."""
    try:
        addr = ipaddress.ip_address(ip)
    except ValueError:
        return False
    return any(addr.version == network.version and addr in network for network in blocked_networks)


def analyze(access: list[dict], errors: list[dict], rate_limits: list[dict], blocked_networks: list,
            f2b: list[dict], hours: int | None) -> dict:
    servers = ["feivyblog", "moodle", "theoed", "bnslab", "unknown"]
    total = len(access)
    # geo 차단뿐 아니라 default server가 444로 막은 IP도 이미 대응된 대상으로 본다.
    runtime_blocked_ips = {r["ip"] for r in access if r["status"] == 444}

    ips_per_server: dict[str, set] = defaultdict(set)
    req_per_server: Counter = Counter()
    bot_per_server: Counter = Counter()
    status_per_server: dict[str, Counter] = defaultdict(Counter)
    paths_per_server: dict[str, Counter] = defaultdict(Counter)
    threat_by_type: Counter = Counter()
    threat_by_ip: Counter = Counter()
    threat_records: list[dict] = []
    brute_force_ips: Counter = Counter()

    for r in access:
        srv = r["server"]
        ips_per_server[srv].add(r["ip"])
        req_per_server[srv] += 1
        if r["bot"]:
            bot_per_server[srv] += 1
        grp = f"{r['status'] // 100}xx"
        status_per_server[srv][grp] += 1
        paths_per_server[srv][r["path"].split("?")[0]] += 1

        blocked = (
            is_blocked_ip(r["ip"], blocked_networks)
            or r["ip"] in runtime_blocked_ips
        )

        # geo blocklist 또는 444로 이미 차단된 IP는 정기 보안 알림 대상에서 제외한다.
        if r["threats"] and not blocked:
            for t in r["threats"]:
                threat_by_type[t] += 1
                threat_by_ip[r["ip"]] += 1
            threat_records.append(r)

        # 무차별 대입: 401/403 다수 (미차단 IP만)
        if r["status"] in (401, 403) and not blocked:
            brute_force_ips[r["ip"]] += 1

    # 고빈도 IP (스캐너 의심) — 이미 차단된 IP는 제외
    active_access = [
        r for r in access
        if not is_blocked_ip(r["ip"], blocked_networks)
        and r["ip"] not in runtime_blocked_ips
    ]
    all_ip_count: Counter = Counter(r["ip"] for r in active_access)
    # 미차단 요청의 평균 5배 이상 접근 = 스캐너 의심
    avg = len(active_access) / max(len(all_ip_count), 1)
    scanner_ips = {ip: cnt for ip, cnt in all_ip_count.items() if cnt >= max(avg * 5, 20)}

    # ── Rate Limit 분석 ──────────────────────────────────────────
    rl_real = [r for r in rate_limits if not r["false_positive"]]
    rl_false = [r for r in rate_limits if r["false_positive"]]
    rl_by_ip: Counter = Counter(r["ip"] for r in rl_real)

    # ── Fail2Ban 분석 ────────────────────────────────────────────
    f2b_bans = [r for r in f2b if r["op"] == "Ban"]
    f2b_unbans = [r for r in f2b if r["op"] == "Unban"]

    # 결과 조립
    result: dict = {
        "period_hours": hours,
        "total_requests": total,
        "servers": {},
        "security": {
            "threat_total": len(threat_records),
            "threat_by_type": dict(threat_by_type.most_common()),
            "threat_top_ips": dict(threat_by_ip.most_common()),
            "threat_samples": [
                {
                    "ip": r["ip"],
                    "path": r["path"][:120],
                    "ua": r["ua"][:80],
                    "threats": r["threats"],
                    "status": r["status"],
                }
                for r in threat_records[:20]
            ],
            "brute_force_ips": dict(brute_force_ips.most_common(10)),
            "scanner_ips": dict(sorted(scanner_ips.items(), key=lambda x: -x[1])[:10]),
        },
        "rate_limit": {
            "total": len(rate_limits),
            "real_attacks": len(rl_real),
            "false_positives": len(rl_false),
            "fp_note": "Next.js RSC(_rsc=) 정상 요청이 Rate Limit에 걸린 오탐" if rl_false else "",
            "top_ips": dict(rl_by_ip.most_common(10)),
        },
        "fail2ban": {
            "bans": len(f2b_bans),
            "unbans": len(f2b_unbans),
            "banned_ips": [{"ip": r["ip"], "jail": r["jail"], "time": r["time"]} for r in f2b_bans[-10:]],
        },
        "errors": {
            "total": len(errors),
            "by_level": dict(Counter(e["level"] for e in errors).most_common()),
            "upstream_issues": [
                e["msg"][:120]
                for e in errors
                if e.get("level") in ("error", "crit") and "upstream" in (e.get("msg") or "")
            ][:10],
        },
    }

    for srv in servers:
        if req_per_server[srv] == 0:
            continue
        result["servers"][srv] = {
            "requests": req_per_server[srv],
            "unique_ips": len(ips_per_server[srv]),
            "bots": bot_per_server[srv],
            "bot_pct": round(bot_per_server[srv] / req_per_server[srv] * 100, 1),
            "status": dict(status_per_server[srv]),
            "top_paths": dict(paths_per_server[srv].most_common(5)),
        }

    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("files", nargs="*", help="로그 파일 경로")
    parser.add_argument("--hours", type=int, default=None, help="최근 N시간만 분석")
    parser.add_argument("--json", action="store_true", help="JSON 출력")
    parser.add_argument("--blocklist", help="geo_blocklist.conf path")
    args = parser.parse_args()

    if args.files:
        files = [Path(p) for p in args.files]
    else:
        log_dir = Path(__file__).parent
        files = list(log_dir.glob("access.log*")) + list(log_dir.glob("error.log*"))
        files = [f for f in files if not f.suffix == ".gz"]

    missing = [f for f in files if not f.exists()]
    if missing:
        print(f"파일 없음: {missing}", file=sys.stderr)
        sys.exit(1)

    blocked_networks = []
    if args.blocklist:
        try:
            with open(args.blocklist) as bf:
                for line in bf:
                    m = re.match(r'\s*([0-9a-fA-F.:]+(?:/[0-9]+)?)\s+1;', line)
                    if m:
                        try:
                            blocked_networks.append(ipaddress.ip_network(m.group(1), strict=False))
                        except ValueError:
                            continue
        except Exception:
            pass

    since = None
    if args.hours:
        since = datetime.now(timezone.utc) - timedelta(hours=args.hours)

    access = parse_access_logs(files, since)
    errors, rate_limits = parse_error_logs(files, since)
    f2b = parse_fail2ban_logs(files, since)

    result = analyze(access, errors, rate_limits, blocked_networks, f2b, args.hours)

    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return

    # 텍스트 출력
    t = result["total_requests"]
    h = f"최근 {args.hours}시간" if args.hours else "전체"
    print(f"\n{'='*60}")
    print(f"  nginx 보안 분석 ({h}, 총 {t:,}건)")
    print(f"{'='*60}")

    for srv, s in result["servers"].items():
        print(f"\n【 {srv.upper()} 】")
        print(f"  요청: {s['requests']:,}건 | Unique IP: {s['unique_ips']:,}")
        print(f"  봇/크롤러: {s['bots']:,}건 ({s['bot_pct']}%)")
        sc = " | ".join(f"{k}:{v}" for k, v in sorted(s["status"].items()))
        print(f"  상태코드: {sc}")
        print(f"  Top 경로: " + ", ".join(f"{p}({c})" for p, c in list(s["top_paths"].items())[:3]))

    sec = result["security"]
    print(f"\n{'─'*60}")
    print(f"  ⚠️  보안 위협 탐지: {sec['threat_total']:,}건")
    if sec["threat_by_type"]:
        for typ, cnt in sec["threat_by_type"].items():
            print(f"    {cnt:4d}  {typ}")
    if sec["threat_top_ips"]:
        print(f"\n  위협 상위 IP:")
        for ip, cnt in list(sec["threat_top_ips"].items())[:5]:
            print(f"    {cnt:4d}  {ip}")
    if sec["brute_force_ips"]:
        top_bf = {ip: c for ip, c in sec["brute_force_ips"].items() if c >= 3}
        if top_bf:
            print(f"\n  무차별 대입 의심 (401/403 다수):")
            for ip, cnt in list(top_bf.items())[:5]:
                print(f"    {cnt:4d}  {ip}")
    if sec["scanner_ips"]:
        print(f"\n  고빈도 스캐너 의심 IP:")
        for ip, cnt in list(sec["scanner_ips"].items())[:5]:
            print(f"    {cnt:4d}  {ip}")

    err = result["errors"]
    if err["total"]:
        print(f"\n  에러 로그: {err['total']}건 | {err['by_level']}")
        for msg in err["upstream_issues"][:3]:
            print(f"    → {msg}")

    if sec["threat_samples"]:
        print(f"\n  위협 샘플 (최대 5건):")
        for s in sec["threat_samples"][:5]:
            print(f"    [{'/'.join(s['threats'])}] {s['ip']} {s['path'][:80]}")

    rl = result["rate_limit"]
    print(f"\n{'─'*60}")
    print(f"  🚦 Rate Limit 위반: {rl['total']}건 "
          f"(실제공격:{rl['real_attacks']} / 오탐:{rl['false_positives']})")
    if rl["fp_note"]:
        print(f"    ⚠️  {rl['fp_note']}")
    if rl["top_ips"]:
        for ip, cnt in list(rl["top_ips"].items())[:3]:
            print(f"    {cnt:4d}  {ip}")

    f2b_r = result["fail2ban"]
    print(f"\n  🔒 Fail2Ban 자동차단: {f2b_r['bans']}건 밴 / {f2b_r['unbans']}건 해제")
    for ban in f2b_r["banned_ips"][-3:]:
        print(f"    [{ban['jail']}] {ban['ip']} ({ban['time']})")


if __name__ == "__main__":
    main()
