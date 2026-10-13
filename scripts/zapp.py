#!/usr/bin/env python3
"""Stage, package, inspect and import Focus Space using the App Host v1 format.

Python standard library only. Compilation belongs to CI; import never retries a
write and credentials are read interactively, never from command-line arguments.
"""
import argparse
import getpass
import gzip
import hashlib
import http.client
import io
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import struct
import sys
import tarfile
import tempfile
import urllib.parse

REPO = Path(__file__).resolve().parents[1]
SLUG = "plaud-action-dashboard"
MAX_SOURCE = 1 << 20
MAX_PACKAGE = 512 << 20
MAX_UNPACKED = 1 << 30
MAX_GATEWAY_UPLOAD = 32 << 20
PERMISSIONS = {
    "device_meetings_read", "obsidian_read", "focus_decision_turn",
    "focus_chat_media_read", "source_gateway_read",
}


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def source_files(root):
    """Explicit production allowlist, including the legacy-to-src layout mapping."""
    root = Path(root).resolve()
    source = root / "src" if (root / "src/main.go").is_file() else root
    entries = [(p, "src/" + p.name) for p in sorted(source.glob("*.go"))
               if not p.name.endswith(("_test.go", "_dev.go"))]
    for directory in ("static", "migrations", "blueprints", "catalog"):
        base = source / directory
        if not base.exists():
            continue
        for path in sorted(base.rglob("*")):
            if path.is_symlink():
                raise ValueError("source contains symlink")
            if path.is_file() and not path.name.endswith((".test.cjs", "_test.go")):
                entries.append((path, "src/" + directory + "/" + path.relative_to(base).as_posix()))
    for path in sorted((root / "internal").rglob("*.go")):
        if not path.name.endswith("_test.go"):
            entries.append((path, path.relative_to(root).as_posix()))
    for name in ("go.mod", "go.sum", "spec.yaml", "metadata.json", "README.md", "LICENSE"):
        path = root / name
        if path.is_file():
            entries.append((path, name))
    names = {name for _, name in entries}
    for required in ("src/main.go", "src/static/index.html", "spec.yaml", "metadata.json", "go.mod"):
        if required not in names:
            raise ValueError("missing production source: " + required)
    if sum(name.startswith("src/") and name.count("/") == 1 and name.endswith(".go") for name in names) > 64:
        raise ValueError("too many application Go source files")
    total = 0
    for path, name in entries:
        if any(part.is_symlink() for part in (path, *path.parents) if part != root) or not path.resolve().is_relative_to(root):
            raise ValueError("source escapes repository")
        size = path.stat().st_size
        total += size
        if size > MAX_SOURCE or total > 32 << 20:
            raise ValueError("source budget exceeded: " + name)
    return sorted(entries, key=lambda pair: pair[1])


def stage(root, output):
    output = Path(output).resolve()
    entries = source_files(root)
    if output.exists():
        raise ValueError("staging destination must not already exist")
    output.mkdir(parents=True)
    for path, name in entries:
        target = output / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, target)
    return output


def elf_arch(binary):
    with Path(binary).open("rb") as stream:
        header = stream.read(64)
    if len(header) < 64 or header[:6] != b"\x7fELF\x02\x01":
        raise ValueError("binary must be a 64-bit little-endian Linux ELF")
    machine = struct.unpack_from("<H", header, 18)[0]
    arch = {62: "amd64", 183: "arm64", 243: "riscv64"}.get(machine)
    if not arch:
        raise ValueError("unsupported ELF architecture")
    return arch


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def package(root, binary, output, revision):
    root, binary, output = Path(root).resolve(), Path(binary).resolve(), Path(output).resolve()
    arch = elf_arch(binary)
    entries = source_files(root)
    metadata = json.loads((root / "metadata.json").read_text(encoding="utf-8"))
    if metadata["name"] != SLUG:
        raise ValueError("Focus Space must retain its stable application slug")
    # Device grants and all installation identities belong to the destination.
    clean = {key: metadata[key] for key in ("schema_version", "name", "display_name", "description", "icon_emoji", "generation")}
    clean["capabilities"] = {"app_data": metadata["capabilities"]["app_data"]}
    replacements = {"app/metadata.json": json_bytes(clean)}
    files = [(path, "app/" + name, "0644") for path, name in entries]
    files.append((binary, "bin/app", "0755"))
    manifest = {
        "schema_version": 1, "kind": "zettlab.app.package",
        "app": {"slug": SLUG, **{k: metadata[k] for k in ("display_name", "description", "icon_emoji")}},
        "version": {"commit": revision, "note": "Focus Space standalone application"},
        "runtime": {"os": "linux", "arch": arch, "binary": "bin/app", "binary_sha256": digest(binary)},
        "platform": {"min_schema_version": 1, "produced_by": "focus-space/zapp-cli-v1"},
        "excluded": ["data/", "logs/", ".git/", "vendor/", "development files and credentials", "device_data grants and installation identities"],
        "files": [],
    }
    total = 0
    for path, name, mode in files:
        raw = replacements.get(name)
        size = len(raw) if raw is not None else path.stat().st_size
        total += size
        manifest["files"].append({"path": name, "size": size, "sha256": hashlib.sha256(raw).hexdigest() if raw is not None else digest(path), "mode": mode})
    if total > MAX_UNPACKED:
        raise ValueError("unpacked package exceeds limit")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=output.parent, delete=False) as temporary:
        temp = Path(temporary.name)
    try:
        with temp.open("wb") as dest, gzip.GzipFile(filename="", mode="wb", fileobj=dest, mtime=0) as gz, tarfile.open(fileobj=gz, mode="w|", format=tarfile.USTAR_FORMAT) as archive:
            def add(name, size, mode, stream):
                info = tarfile.TarInfo(name)
                info.size, info.mode, info.mtime = size, int(mode, 8), 0
                archive.addfile(info, stream)
            raw = json_bytes(manifest)
            add("manifest.json", len(raw), "0644", io.BytesIO(raw))
            for (path, name, mode), record in zip(files, manifest["files"]):
                if name in replacements:
                    add(name, record["size"], mode, io.BytesIO(replacements[name]))
                else:
                    with path.open("rb") as stream:
                        add(name, record["size"], mode, stream)
        inspect_package(temp)
        if temp.stat().st_size > MAX_PACKAGE:
            raise ValueError("compressed package exceeds limit")
        os.replace(temp, output)
    finally:
        temp.unlink(missing_ok=True)
    return manifest


def inspect_package(path):
    path = Path(path)
    if path.stat().st_size > MAX_PACKAGE:
        raise ValueError("package exceeds upload limit")
    with tarfile.open(path, "r|gz") as archive:
        first = archive.next()
        if first is None or first.name != "manifest.json" or not first.isfile() or first.size > 512 << 10:
            raise ValueError("manifest.json must be the first bounded regular entry")
        manifest = json.load(archive.extractfile(first))
        if manifest.get("kind") != "zettlab.app.package" or manifest.get("schema_version") != 1 or manifest.get("app", {}).get("slug") != SLUG:
            raise ValueError("not a Focus Space v1 application package")
        records = manifest.get("files", [])
        expected = {item["path"]: item for item in records}
        if len(expected) != len(records) or len(expected) > 50000:
            raise ValueError("duplicate or excessive manifest entries")
        seen, total = set(), 0
        while (entry := archive.next()) is not None:
            name = entry.name
            parts = PurePosixPath(name).parts
            if not entry.isfile() or name in seen or name not in expected or ".." in parts or name.startswith("/") or "\\" in name or not (name.startswith("app/") or name == "bin/app"):
                raise ValueError("unexpected or unsafe package entry")
            total += entry.size
            if total > MAX_UNPACKED or entry.size < 0:
                raise ValueError("unpacked package exceeds limit")
            item = expected[name]
            if entry.size != item["size"] or hashlib.file_digest(archive.extractfile(entry), "sha256").hexdigest() != item["sha256"]:
                raise ValueError("package content does not match manifest")
            seen.add(name)
        if seen != set(expected) or not {"bin/app", "app/metadata.json", "app/spec.yaml"} <= seen:
            raise ValueError("package is incomplete")
        if manifest.get("runtime", {}).get("binary_sha256") != expected["bin/app"]["sha256"]:
            raise ValueError("runtime binary hash mismatch")
        return manifest


def api_request(base, token, method, route, body=None, size=None):
    parsed = urllib.parse.urlsplit(base)
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise ValueError("device URL must be an http(s) origin")
    connection = (http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection)(parsed.hostname, parsed.port, timeout=180)
    headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
    if body is not None:
        headers.update({"Content-Type": "application/octet-stream", "Content-Length": str(size)})
    try:
        connection.request(method, route, body=body, headers=headers)
        response = connection.getresponse()
        raw = response.read((1 << 20) + 1)
        if len(raw) > 1 << 20:
            raise ValueError("device response too large")
        if response.status < 200 or response.status >= 300:
            raise ValueError(f"device HTTP {response.status}; no automatic retry")
        result = json.loads(raw)
        if result.get("code") != 200:
            raise ValueError(f"device rejected request (code {result.get('code')}); no automatic retry")
        return result["data"]
    finally:
        connection.close()


def import_package(path, base, token, grants, replace=False):
    inspect_package(path)
    if Path(path).stat().st_size > MAX_GATEWAY_UPLOAD:
        raise ValueError("package exceeds the device gateway's 32 MiB import limit")
    if len(set(grants)) != len(grants) or not set(grants) <= PERMISSIONS:
        raise ValueError("invalid or duplicate permission")
    features = api_request(base, token, "GET", "/api/v1/apps/admin/import-capabilities")
    required = {"preserve_slug_v1"} | ({"explicit_device_data_v1"} if grants else set())
    if not required <= set(features.get("features", [])) or not set(grants) <= set(features.get("device_data", [])):
        raise ValueError("update the device App Host before importing this application")
    query = [("preserve_slug", "true")] + [("grant_device_data", grant) for grant in grants]
    if replace:
        query.append(("on_conflict", "replace"))
    with Path(path).open("rb") as stream:
        result = api_request(base, token, "POST", "/api/v1/apps/admin/import?" + urllib.parse.urlencode(query), stream, os.fstat(stream.fileno()).st_size)
    if result.get("slug") != SLUG or result.get("renamed"):
        raise ValueError("unexpected installed identity; inspect device state before retrying")
    report = result.get("app", {}).get("capabilities") or {}
    active = set(report.get("granted") or []) | set(report.get("already") or [])
    if not ({"app_ai"} | set(grants)) <= active:
        raise ValueError("application installed but required capabilities were not confirmed; inspect device state before retrying")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    staging = commands.add_parser("stage", help="create production source tree for CI compilation")
    staging.add_argument("--source", type=Path, default=REPO)
    staging.add_argument("--output", type=Path, required=True)
    pack = commands.add_parser("pack", help="package a CI-built Linux ELF and matching sources")
    pack.add_argument("--source", type=Path, default=REPO)
    pack.add_argument("--binary", type=Path, required=True)
    pack.add_argument("--output", type=Path, required=True)
    pack.add_argument("--revision", required=True)
    inspect = commands.add_parser("inspect")
    inspect.add_argument("package", type=Path)
    install = commands.add_parser("import", help="import into a connected device; asks for device JWT")
    install.add_argument("package", type=Path)
    install.add_argument("--device", required=True)
    install.add_argument("--grant", action="append", default=[], choices=sorted(PERMISSIONS))
    install.add_argument("--replace", action="store_true", help="explicitly upgrade the existing app, retaining its data")
    args = parser.parse_args()
    try:
        if args.command == "stage":
            result = {"source": str(stage(args.source, args.output))}
        elif args.command == "pack":
            result = package(args.source, args.binary, args.output, args.revision)
        elif args.command == "inspect":
            result = inspect_package(args.package)
        else:
            inspect_package(args.package)
            if not sys.stdin.isatty():
                raise ValueError("run import in an interactive terminal to enter the device JWT securely")
            token = getpass.getpass("Device JWT (hidden): ").strip()
            if not token or any(c.isspace() for c in token):
                raise ValueError("device JWT is required")
            result = import_package(args.package, args.device, token, args.grant, args.replace)
            token = ""
        print(json.dumps(result, ensure_ascii=False, indent=2))
    except (OSError, ValueError, KeyError, tarfile.TarError, http.client.HTTPException) as exc:
        print(f"zapp: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
