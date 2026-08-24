#!/usr/bin/env python3
"""Structured fail-closed inspection for core replay and rootfs tar archives."""

from __future__ import annotations

import hashlib
import json
import posixpath
import sys
import tarfile
from pathlib import PurePosixPath
from typing import BinaryIO, TypedDict


class Entry(TypedDict):
    path: str
    type: str
    mode: str
    size: int
    sha256: str
    linkTarget: str


def fail(message: str) -> None:
    raise ValueError(message)


def normalized_path(raw: str, *, directory: bool) -> str:
    value = raw[:-1] if directory and raw.endswith("/") else raw
    if (
        not value
        or "\\" in value
        or "\x00" in value
        or PurePosixPath(value).is_absolute()
        or value != posixpath.normpath(value)
        or ".." in PurePosixPath(value).parts
    ):
        fail(f"unsafe or non-canonical archive path: {raw!r}")
    return value


def normalized_link(path: str, raw: str, *, hardlink: bool, rootfs: bool) -> str:
    if not raw or "\\" in raw or "\x00" in raw:
        fail(f"unsafe archive link: {path!r} -> {raw!r}")
    if raw.startswith("/"):
        if not rootfs:
            fail(f"absolute archive link is forbidden: {path!r} -> {raw!r}")
        raw = raw.lstrip("/")
        base = ""
    else:
        base = "" if hardlink else posixpath.dirname(path)
    target = posixpath.normpath(posixpath.join(base, raw))
    if target in ("", ".", "..") or target.startswith("../"):
        fail(f"escaping archive link: {path!r} -> {raw!r}")
    return target


def stream_hash(stream: BinaryIO) -> tuple[str, str, int, bytes]:
    sha256 = hashlib.sha256()
    body = bytearray()
    while chunk := stream.read(1024 * 1024):
        sha256.update(chunk)
        body.extend(chunk)
    bytes_value = bytes(body)
    git_blob = hashlib.sha1(b"blob " + str(len(bytes_value)).encode() + b"\0" + bytes_value).hexdigest()
    return sha256.hexdigest(), git_blob, len(bytes_value), bytes_value


def git_tree_sha(files: dict[str, tuple[str, str]]) -> str:
    tree: dict[str, object] = {}
    for path, (mode, blob_sha) in files.items():
        current = tree
        parts = path.split("/")
        for part in parts[:-1]:
            value = current.setdefault(part, {})
            if not isinstance(value, dict):
                fail(f"Git tree path conflicts with a file: {path!r}")
            current = value
        if parts[-1] in current:
            fail(f"duplicate Git tree path: {path!r}")
        current[parts[-1]] = (mode, blob_sha)

    def digest_tree(value: dict[str, object]) -> str:
        records: list[tuple[bytes, bytes]] = []
        for name, child in value.items():
            name_bytes = name.encode("utf-8")
            if isinstance(child, dict):
                oid = digest_tree(child)
                records.append((name_bytes + b"/", b"40000 " + name_bytes + b"\0" + bytes.fromhex(oid)))
            else:
                mode, oid = child
                records.append((name_bytes, mode.encode() + b" " + name_bytes + b"\0" + bytes.fromhex(oid)))
        body = b"".join(record for _, record in sorted(records, key=lambda item: item[0]))
        return hashlib.sha1(b"tree " + str(len(body)).encode() + b"\0" + body).hexdigest()

    return digest_tree(tree)


def validate_parent_closure(entry_by_path: dict[str, Entry]) -> None:
    """Reject every non-directory archive member used as a path parent."""
    for path in entry_by_path:
        parts = path.split("/")
        for index in range(1, len(parts)):
            parent_path = "/".join(parts[:index])
            parent = entry_by_path.get(parent_path)
            if parent is not None and parent["type"] != "directory":
                fail(f"archive member descends through a non-directory: {path!r}")


def inspect(archive_path: str, profile: str) -> dict:
    if profile not in ("core-projection", "rootfs"):
        fail("profile must be core-projection or rootfs")
    rootfs = profile == "rootfs"
    entries: list[Entry] = []
    entry_by_path: dict[str, Entry] = {}
    git_files: dict[str, tuple[str, str]] = {}
    explicit_directories: set[str] = set()
    implied_directories: set[str] = set()
    with tarfile.open(archive_path, "r:") as archive:
        for member in archive:
            if member.isdir():
                kind = "directory"
            elif member.isreg():
                kind = "file"
            elif member.issym() and rootfs:
                kind = "symlink"
            elif member.islnk() and rootfs:
                kind = "hardlink"
            else:
                fail(f"forbidden archive member type: {member.name!r}")
            path = normalized_path(member.name, directory=member.isdir())
            if path in entry_by_path:
                fail(f"duplicate archive member: {path!r}")
            if rootfs and path in ("input", "projection", "work", "authority"):
                fail(f"rootfs archive pre-populates a reserved replay mountpoint: {path!r}")
            if not rootfs and (
                path == ".git"
                or path.startswith(".git/")
                or path == "node_modules"
                or path.startswith("node_modules/")
            ):
                fail(f"core projection archive contains forbidden authority path: {path!r}")
            link_target = ""
            sha256 = ""
            size = 0
            if kind == "file":
                stream = archive.extractfile(member)
                if stream is None:
                    fail(f"unreadable regular archive member: {path!r}")
                with stream:
                    sha256, blob_sha, size, _ = stream_hash(stream)
                git_mode = "100755" if member.mode & 0o111 else "100644"
                if not rootfs:
                    expected_tar_mode = 0o755 if git_mode == "100755" else 0o644
                    if member.mode != expected_tar_mode or member.uid != 0 or member.gid != 0 or member.mtime != 0:
                        fail(f"core projection tar metadata drifted: {path!r}")
                    git_files[path] = (git_mode, blob_sha)
                mode = git_mode if not rootfs else f"{member.mode & 0o7777:04o}"
            elif kind == "directory":
                explicit_directories.add(path)
                mode = f"{member.mode & 0o7777:04o}"
                if not rootfs and (member.mode != 0o755 or member.uid != 0 or member.gid != 0 or member.mtime != 0):
                    fail(f"core projection directory metadata drifted: {path!r}")
            else:
                if kind == "hardlink" and member.linkname.startswith("/"):
                    fail(f"absolute hardlink is forbidden: {path!r} -> {member.linkname!r}")
                mode = f"{member.mode & 0o7777:04o}"
                link_target = normalized_link(
                    path,
                    member.linkname,
                    hardlink=kind == "hardlink",
                    rootfs=rootfs,
                )
            entry: Entry = {
                "path": path,
                "type": kind,
                "mode": mode,
                "size": size,
                "sha256": sha256,
                "linkTarget": link_target,
            }
            entries.append(entry)
            entry_by_path[path] = entry
            parts = path.split("/")
            implied_directories.update("/".join(parts[:index]) for index in range(1, len(parts)))

    if not rootfs and explicit_directories != implied_directories:
        fail("core projection explicit directory closure differs from file parents")
    link_paths = {entry["path"] for entry in entries if entry["type"] in ("symlink", "hardlink")}
    validate_parent_closure(entry_by_path)

    def terminal(path: str, chain: set[str]) -> Entry | None:
        if path in chain:
            fail(f"archive link cycle: {path!r}")
        entry = entry_by_path.get(path)
        if entry is None:
            return None
        if entry["type"] not in ("symlink", "hardlink"):
            return entry
        return terminal(entry["linkTarget"], chain | {path})

    for path in link_paths:
        entry = entry_by_path[path]
        if entry["type"] == "hardlink":
            # A hardlink is an inode alias, not a link chain.  Requiring its
            # target to be a direct regular member prevents a later symlink or
            # hardlink from changing the meaning during extraction.
            target_entry = entry_by_path.get(entry["linkTarget"])
            if target_entry is None or target_entry["type"] != "file":
                fail(f"archive hardlink does not target a direct regular file: {path!r}")
        else:
            terminal(path, set())

    sorted_entries = sorted(entries, key=lambda entry: entry["path"].encode("utf-8"))
    manifest = hashlib.sha256()
    for entry in sorted_entries:
        for value in (
            entry["path"],
            entry["type"],
            entry["mode"],
            str(entry["size"]),
            entry["sha256"],
            entry["linkTarget"],
        ):
            manifest.update(value.encode("utf-8"))
            manifest.update(b"\0")
    counts = {
        kind: sum(entry["type"] == kind for entry in entries)
        for kind in ("file", "directory", "symlink", "hardlink")
    }
    result = {
        "formatVersion": "cloud-agents-generator-replay-archive-inspection/v1",
        "profile": profile,
        "manifestAlgorithm": "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
        "manifestSha256": manifest.hexdigest(),
        "entries": len(entries),
        "regularFiles": counts["file"],
        "directories": counts["directory"],
        "symlinks": counts["symlink"],
        "hardlinks": counts["hardlink"],
        "unsafeEntries": 0,
        "duplicateEntries": 0,
        "specialEntries": 0,
        "linkPrefixDescendants": 0,
        "linkCycles": 0,
    }
    if not rootfs:
        regular_manifest = hashlib.sha256()
        for entry in sorted_entries:
            if entry["type"] != "file":
                continue
            for value in (entry["path"], entry["mode"], str(entry["size"]), entry["sha256"]):
                regular_manifest.update(value.encode("utf-8"))
                regular_manifest.update(b"\0")
        result.update(
            {
                "regularFileManifestAlgorithm": "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
                "regularFileManifestSha256": regular_manifest.hexdigest(),
                "reconstructedGitTreeSha": git_tree_sha(git_files),
            }
        )
    return result


def main() -> None:
    if len(sys.argv) != 3:
        fail("usage: inspect-generator-replay-archive.py core-projection|rootfs ARCHIVE")
    print(json.dumps(inspect(sys.argv[2], sys.argv[1]), sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    main()
