#!/usr/bin/env python3
"""Nginx 분석 결과에서 명백한 미차단 공격 IP를 임시 geo 차단한다.

자동 차단 기준:
- PATH_TRAVERSAL/FILE_INCLUSION/CMD_INJECTION/SQL_INJECTION/LOG4J_JNDI/WEBSHELL 1건 이상
- exploit 5건 이상 + 요청 50건 이상
- exploit 동반 scanner 요청 100건 이상

차단은 30일 후 만료되며, 실행할 때마다 만료된 auto-block 항목을 정리한다.
"""

import argparse
from datetime import date, datetime, timedelta
import ipaddress
import json
from pathlib import Path
import re
import subprocess
import tempfile

AUTO_EXPIRY_DAYS = 30
HIGH_RISK_TYPES = {
    "PATH_TRAVERSAL",
    "FILE_INCLUSION",
    "CMD_INJECTION",
    "SQL_INJECTION",
    "LOG4J_JNDI",
    "WEBSHELL",
}
# Cloudflare published IP ranges. CDN/edge IP는 절대 자동 차단하지 않는다.
CLOUDFLARE_NETWORKS = [
    "173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
    "141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
    "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
    "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
    "2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
    "2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
]
EXCLUDED_NETWORKS = [
    ipaddress.ip_network("100.64.0.0/10"),  # Tailscale CGNAT
    *[ipaddress.ip_network(value) for value in CLOUDFLARE_NETWORKS],
]
AUTO_LINE_RE = re.compile(
    r"^\s*(?P<ip>[0-9a-fA-F.:]+)\s+1;\s+# auto-block expires=(?P<expiry>\d{4}-\d{2}-\d{2}).*$"
)
BLOCK_LINE_RE = re.compile(r"^\s*([0-9a-fA-F.:]+(?:/\d+)?)\s+1;")


def load_networks(config: str) -> list:
    networks = []
    for line in config.splitlines():
        match = BLOCK_LINE_RE.match(line)
        if not match:
            continue
        try:
            networks.append(ipaddress.ip_network(match.group(1), strict=False))
        except ValueError:
            continue
    return networks


def excluded(ip: str) -> bool:
    try:
        addr = ipaddress.ip_address(ip)
    except ValueError:
        return True
    if addr.is_private or addr.is_loopback or addr.is_multicast or addr.is_reserved:
        return True
    return any(addr.version == network.version and addr in network for network in EXCLUDED_NETWORKS)


def block_reason(candidate: dict) -> str | None:
    ip = candidate.get("ip", "")
    requests = int(candidate.get("requests", 0))
    threats = int(candidate.get("threats", 0))
    bot_requests = int(candidate.get("bot_requests", 0))
    types = set(candidate.get("types", {}))

    if excluded(ip) or (requests > 0 and bot_requests >= requests):
        return None
    high_risk = sorted(types & HIGH_RISK_TYPES)
    if high_risk:
        return "high-risk " + "+".join(high_risk)
    if threats >= 5 and requests >= 50:
        return f"exploit {threats}건 / {requests} 요청"
    if threats >= 1 and requests >= 100:
        return f"scanner {requests} 요청 / exploit {threats}건"
    return None


def remove_expired_auto_lines(config: str, today: date) -> tuple[str, list[str]]:
    lines = []
    expired = []
    for line in config.splitlines(keepends=True):
        match = AUTO_LINE_RE.match(line)
        if match and date.fromisoformat(match.group("expiry")) < today:
            expired.append(match.group("ip"))
            continue
        lines.append(line)
    return "".join(lines), expired


def update_remote(remote: str, ssh_key: str, config: str) -> tuple[bool, str]:
    """검증 실패 시 원본을 복구하는 원격 Nginx 설정 반영."""
    remote_config = "/etc/nginx/conf.d/geo_blocklist.conf"
    with tempfile.NamedTemporaryFile("w", delete=False, encoding="utf-8") as temp:
        temp.write(config)
        local_path = temp.name
    try:
        with open(local_path, "rb") as source:
            upload = subprocess.run(
                ["ssh", "-i", ssh_key, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
                 "-o", "StrictHostKeyChecking=no", remote,
                 "sudo tee /tmp/geo_blocklist.auto.new >/dev/null"],
                stdin=source, text=False, capture_output=True,
            )
        if upload.returncode != 0:
            return False, upload.stderr.decode(errors="replace").strip()

        command = (
            f"sudo cp {remote_config} {remote_config}.bak.auto-$(date +%Y%m%d%H%M%S) && "
            f"sudo cp /tmp/geo_blocklist.auto.new {remote_config} && "
            "sudo nginx -t && sudo systemctl reload nginx"
        )
        applied = subprocess.run(
            ["ssh", "-i", ssh_key, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
             "-o", "StrictHostKeyChecking=no", remote, command],
            text=True, capture_output=True,
        )
        if applied.returncode == 0:
            return True, ""

        # 설정 반영 뒤 nginx -t/reload가 실패한 경우 직전 백업으로 복구한다.
        rollback = (
            f"sudo sh -c 'latest=$(ls -t {remote_config}.bak.auto-* | head -1); "
            f"[ -n \"$latest\" ] && cp \"$latest\" {remote_config}' && sudo nginx -t && sudo systemctl reload nginx"
        )
        subprocess.run(
            ["ssh", "-i", ssh_key, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
             "-o", "StrictHostKeyChecking=no", remote, rollback],
            text=True, capture_output=True,
        )
        return False, (applied.stdout + applied.stderr).strip()
    finally:
        Path(local_path).unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--analysis", required=True, help="analyze.py JSON 결과 파일")
    parser.add_argument("--blocklist", required=True, help="현재 geo_blocklist.conf 사본")
    parser.add_argument("--ssh-key", required=True)
    parser.add_argument("--remote", required=True)
    parser.add_argument("--history", required=True)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    analysis_path = Path(args.analysis)
    analysis = json.loads(analysis_path.read_text())
    config = Path(args.blocklist).read_text()
    today = datetime.now().date()
    expires = today + timedelta(days=AUTO_EXPIRY_DAYS)
    networks = load_networks(config)

    def already_blocked(ip: str) -> bool:
        addr = ipaddress.ip_address(ip)
        return any(addr.version == network.version and addr in network for network in networks)

    applied = []
    for candidate in analysis.get("security", {}).get("auto_block_candidates", []):
        ip = candidate.get("ip", "")
        reason = block_reason(candidate)
        if not reason or already_blocked(ip):
            continue
        applied.append({
            "ip": ip,
            "reason": reason,
            "requests": int(candidate.get("requests", 0)),
            "threats": int(candidate.get("threats", 0)),
            "expires": expires.isoformat(),
        })

    updated_config, expired = remove_expired_auto_lines(config, today)
    if applied:
        stripped = updated_config.rstrip()
        if not stripped.endswith("}"):
            raise RuntimeError("geo_blocklist.conf closing brace not found")
        lines = ["", "    # --- Automatic temporary blocks ---"]
        for item in applied:
            lines.append(
                f"    {item['ip']} 1;  # auto-block expires={item['expires']} "
                f"reason={item['reason']} threats={item['threats']} requests={item['requests']}"
            )
        updated_config = stripped[:-1].rstrip() + "\n" + "\n".join(lines) + "\n}\n"

    result = {"applied": [], "expired": expired, "error": ""}
    if updated_config != config:
        if args.dry_run:
            result["applied"] = applied
        else:
            ok, error = update_remote(args.remote, args.ssh_key, updated_config)
            if not ok:
                result["error"] = error or "Nginx blocklist update failed"
            else:
                result["applied"] = applied
                Path(args.blocklist).write_text(updated_config)
                history = Path(args.history)
                history.parent.mkdir(parents=True, exist_ok=True)
                with history.open("a", encoding="utf-8") as fh:
                    for item in applied:
                        fh.write(json.dumps({"at": datetime.now().isoformat(timespec="seconds"), **item}, ensure_ascii=False) + "\n")

    analysis["auto_block"] = result
    analysis_path.write_text(json.dumps(analysis, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
