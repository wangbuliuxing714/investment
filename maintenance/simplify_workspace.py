"""Explicit, one-time workspace cleanup. Never runs during engine startup.

Dry-run is the default. --apply compares every original row inside an SQLite
write transaction before replacing anything. The supplied UTF-8 snapshot is the
complete, credential-free backup of the original workspace.
"""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import sqlite3
from urllib.request import urlopen


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", type=Path, required=True)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--api-url", default="http://127.0.0.1:8787")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--source", type=Path, help="Reviewed replacement Markdown directory")
    parser.add_argument("--flat", action="store_true", help="Keep only the three root documents, with no directories")
    parser.add_argument("--file-protocol", type=Path, help="Reviewed file protocol section for the saved Tools configuration")
    args = parser.parse_args()
    original = json.loads(args.snapshot.read_text(encoding="utf-8-sig"))
    source = args.source or Path(__file__).parent / "workspace-simple"
    desired = {p.relative_to(source).as_posix(): p.read_text(encoding="utf-8")
               for p in source.rglob("*.md")}
    # INVEST.MD is uppercase on Windows and must also work on case-sensitive FS.
    desired["INVEST.MD"] = (source / "INVEST.MD").read_text(encoding="utf-8")
    assert all(len(c.encode("utf-8")) <= 128 * 1024 for c in desired.values())
    if args.flat:
        assert set(desired) == {"INVEST.MD", "验证.md", "复盘.md"}
    else:
        assert len(desired) == 6
    fields = ("path", "directory", "content", "revision", "updatedAt")
    expected = sorted(tuple(r[k] for k in fields) for r in original)
    with urlopen(args.api_url.rstrip("/") + "/api/trader", timeout=10) as response:
        state = json.load(response)
    if state.get("running") is not False or state.get("busy") is not False:
        raise RuntimeError("Agent must be stopped and idle; no changes made")
    # mode/conversation are observed only; this script never changes them.
    connection = sqlite3.connect(f"file:{args.db.resolve().as_posix()}?mode=rw", uri=True, timeout=10)
    try:
        connection.execute("BEGIN IMMEDIATE")
        rows = connection.execute("SELECT path,directory,content,revision,updated_at FROM workspace_files ORDER BY path").fetchall()
        if rows != expected:
            raise RuntimeError("Workspace changed since backup; export and review again")
        if args.file_protocol:
            raw = connection.execute("SELECT value FROM settings WHERE key='agent_tools'").fetchone()[0]
            tools = json.loads(raw)
            instructions = tools['instructions']
            start = instructions.index('## Markdown 工作区工具')
            end = instructions.find('\n## ', start + 1)
            end = len(instructions) if end < 0 else end
            replacement = args.file_protocol.read_text(encoding='utf-8').strip()
            assert replacement.startswith('## Markdown 工作区工具')
            tools['instructions'] = instructions[:start] + replacement + '\n\n' + instructions[end:].lstrip('\n')
            if args.apply:
                args.snapshot.with_name('tools-before.json').write_text(raw, encoding='utf-8')
            connection.execute("UPDATE settings SET value=? WHERE key='agent_tools'", (json.dumps(tools, ensure_ascii=False),))
        originals = {r[0]: r for r in rows}
        now = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
        folders = () if args.flat else ("优化", "复盘")
        keep = set(desired) | set(folders)
        for folder in folders:
            assert originals[folder][1] == 1
        for path, content in desired.items():
            revision = originals[path][3] + 1 if path in originals else 1
            parent = path.rpartition("/")[0]
            connection.execute("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,?,0,?,?,?) ON CONFLICT(path) DO UPDATE SET content=excluded.content,revision=excluded.revision,updated_at=excluded.updated_at",
                               (path, parent, content, revision, now))
        for path in originals:
            if path not in keep:
                connection.execute("DELETE FROM workspace_files WHERE path=?", (path,))
        after = connection.execute("SELECT path,content FROM workspace_files WHERE directory=0 ORDER BY path").fetchall()
        assert dict(after) == desired
        report = {
            "applied": args.apply, "backup": str(args.snapshot.resolve()),
            "beforeFiles": sum(not r[1] for r in rows), "afterFiles": len(desired),
            "beforeBytes": sum(len(r[2].encode("utf-8")) for r in rows),
            "afterBytes": sum(len(c.encode("utf-8")) for c in desired.values()),
            "files": [{"path": p, "sha256": hashlib.sha256(c.encode("utf-8")).hexdigest()} for p, c in after],
            "mode": state.get("mode"), "conversationId": state.get("conversationId"),
        }
        if args.apply:
            connection.commit()
            args.snapshot.with_name("simplification-applied.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        else:
            connection.rollback()
        print(json.dumps(report, ensure_ascii=False, indent=2))
    finally:
        connection.close()


if __name__ == "__main__":
    main()
