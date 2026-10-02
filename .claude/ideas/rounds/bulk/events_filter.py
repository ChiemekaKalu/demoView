"""Allow-list filter for `demo inspect --events` output before it leaves the
runner. Keeps tick + event-name lines, numeric count tables, the fixed section
titles and numeric header fields; drops the free-text header lines (file path,
server name, client, demo GUID). If any other line remains, the whole file is
rejected and nothing is written.

usage: events_filter.py <in> <out>
"""
import re
import sys

DROP = re.compile(r"^(file|server|client|demo):")
ALLOWED = [
    re.compile(r"^\s*-?\d+\s+[a-z][a-z0-9_]*(\s+x\d+)?\s*$"),                      # 150290 round_freeze_end x3
    re.compile(r"^\s*(\d+\s+)?[A-Za-z][A-Za-z0-9_]*(\(\d+\))?(\s+-?\d+)+\s*$"),       # count tables, MsgType(55) rows
    re.compile(r"^(header|fileinfo|ticks|stop|map|network|playback|packets):[\w\s=.,()\-]*$"),
    re.compile(r"^(command\s+frames\s+compressed\s+bytes\s+max_bytes|\s*id\s+message\s+count\s+bytes|\s*count\s+game event"
               r"|\s*tick\s+round-structure event \(xN: N on one tick\))\s*$"),
]


def main():
    src, dst = sys.argv[1:3]
    kept = []
    for line in open(src, encoding="utf-8", errors="replace"):
        line = line.rstrip("\n")
        if not line.strip() or DROP.match(line):
            continue
        if not any(p.match(line) for p in ALLOWED):
            sys.exit(f"rejected: unexpected line shape {re.sub(r'[A-Za-z]', 'a', line)[:60]!r}")
        kept.append(line)
    open(dst, "w", encoding="utf-8").write("\n".join(kept) + "\n")


if __name__ == "__main__":
    main()
