#!/usr/bin/env python3
"""Fail-closed archive inventory and selected regular-member digest inspector."""

from __future__ import annotations

import hashlib
import json
import os
import posixpath
import stat
import sys
import tarfile
import zipfile
from pathlib import PurePosixPath
from typing import BinaryIO, TypedDict


class Entry(TypedDict):
    path: str
    type: str
    linkTarget: str | None


def fail(message: str) -> None:
    raise ValueError(message)


def normalized_path(raw: str, *, directory: bool = False) -> str:
    if not raw or "\\" in raw or "\x00" in raw or PurePosixPath(raw).is_absolute():
        fail(f"unsafe archive path: {raw!r}")
    stripped = raw[:-1] if directory and raw.endswith("/") else raw
    if not stripped or stripped != posixpath.normpath(stripped):
        fail(f"non-canonical archive path: {raw!r}")
    if ".." in PurePosixPath(stripped).parts:
        fail(f"escaping archive path: {raw!r}")
    return stripped


def normalized_link(path: str, raw_target: str, *, hardlink: bool) -> str:
    if not raw_target or "\\" in raw_target or "\x00" in raw_target:
        fail(f"unsafe archive link target: {path!r} -> {raw_target!r}")
    if PurePosixPath(raw_target).is_absolute():
        fail(f"absolute archive link target: {path!r} -> {raw_target!r}")
    base = "" if hardlink else posixpath.dirname(path)
    target = posixpath.normpath(posixpath.join(base, raw_target))
    if target in ("", ".", "..") or target.startswith("../"):
        fail(f"escaping archive link target: {path!r} -> {raw_target!r}")
    return target


def sha256_stream(stream: BinaryIO) -> tuple[str, int]:
    digest = hashlib.sha256()
    size = 0
    while chunk := stream.read(1024 * 1024):
        digest.update(chunk)
        size += len(chunk)
    return digest.hexdigest(), size


def finish(entries: list[Entry], selected: dict[str, tuple[str, int]], archive_format: str) -> dict:
    seen: set[str] = set()
    entry_paths: set[str] = set()
    entries_by_path: dict[str, Entry] = {}
    for entry in entries:
        path = entry["path"]
        if path in seen:
            fail(f"duplicate archive member: {path!r}")
        seen.add(path)
        entry_paths.add(path)
        entries_by_path[path] = entry
    for entry in entries:
        target = entry["linkTarget"]
        if target is not None and target not in entry_paths:
            fail(f"archive link target is absent: {entry['path']!r} -> {target!r}")
    link_paths = {entry["path"] for entry in entries if entry["linkTarget"] is not None}
    for path in entry_paths:
        parts = PurePosixPath(path).parts
        for length in range(1, len(parts)):
            prefix = "/".join(parts[:length])
            if prefix in link_paths:
                fail(f"archive member descends through a symlink or hardlink: {path!r}")

    def terminal_link_target(path: str, chain: set[str]) -> Entry:
        if path in chain:
            fail(f"archive link cycle detected at: {path!r}")
        entry = entries_by_path[path]
        target = entry["linkTarget"]
        if target is None:
            return entry
        return terminal_link_target(target, chain | {path})

    for path in link_paths:
        terminal = terminal_link_target(path, set())
        if terminal["type"] != "file":
            fail(f"archive link does not resolve to a regular file: {path!r}")

    inventory = hashlib.sha256()
    for entry in sorted(entries, key=lambda value: value["path"].encode("utf-8")):
        inventory.update(entry["path"].encode())
        inventory.update(b"\0")
        inventory.update(entry["type"].encode())
        inventory.update(b"\0")
        inventory.update((entry["linkTarget"] or "").encode())
        inventory.update(b"\0")
    counts = {kind: sum(entry["type"] == kind for entry in entries) for kind in ("file", "directory", "symlink", "hardlink")}
    return {
        "formatVersion": "cloud-agents-generator-supply-archive-inspection/v1",
        "archiveFormat": archive_format,
        "inventoryAlgorithm": "sorted-path-nul-type-nul-link-target-nul-v1",
        "inventorySha256": inventory.hexdigest(),
        "entries": len(entries),
        "regularFiles": counts["file"],
        "directories": counts["directory"],
        "symlinks": counts["symlink"],
        "hardlinks": counts["hardlink"],
        "unsafeEntries": 0,
        "duplicateEntries": 0,
        "specialEntries": 0,
        "linkTargetsResolveToRegularFiles": True,
        "linkCycles": 0,
        "linkPrefixDescendants": 0,
        "selectedMembers": [
            {"path": path, "sha256": digest, "sizeBytes": size}
            for path, (digest, size) in sorted(selected.items(), key=lambda item: item[0].encode("utf-8"))
        ],
    }


def inspect_tar(path: str, requested: set[str]) -> dict:
    entries: list[Entry] = []
    selected: dict[str, tuple[str, int]] = {}
    with tarfile.open(path, "r:*") as archive:
        for member in archive.getmembers():
            if member.isdir():
                kind = "directory"
            elif member.isreg():
                kind = "file"
            elif member.issym():
                kind = "symlink"
            elif member.islnk():
                kind = "hardlink"
            else:
                fail(f"special archive entry is forbidden: {member.name!r}")
            name = normalized_path(member.name, directory=member.isdir())
            link_target = None
            if kind in ("symlink", "hardlink"):
                link_target = normalized_link(name, member.linkname, hardlink=kind == "hardlink")
            entries.append({"path": name, "type": kind, "linkTarget": link_target})
            if name in requested:
                if kind != "file":
                    fail(f"selected executable member is not a regular file: {name!r}")
                stream = archive.extractfile(member)
                if stream is None:
                    fail(f"selected executable member is unreadable: {name!r}")
                with stream:
                    selected[name] = sha256_stream(stream)
    return finish(entries, selected, "tar")


def inspect_zip(path: str, requested: set[str]) -> dict:
    entries: list[Entry] = []
    selected: dict[str, tuple[str, int]] = {}
    with zipfile.ZipFile(path) as archive:
        for member in archive.infolist():
            mode = member.external_attr >> 16
            is_directory = member.is_dir()
            kind = "directory" if is_directory else "file"
            if stat.S_ISLNK(mode):
                kind = "symlink"
            elif mode and not (is_directory or stat.S_ISREG(mode)):
                fail(f"special ZIP entry is forbidden: {member.filename!r}")
            name = normalized_path(member.filename, directory=is_directory)
            link_target = None
            if kind == "symlink":
                with archive.open(member) as stream:
                    raw_target = stream.read().decode("utf-8")
                link_target = normalized_link(name, raw_target, hardlink=False)
            entries.append({"path": name, "type": kind, "linkTarget": link_target})
            if name in requested:
                if kind != "file":
                    fail(f"selected executable member is not a regular file: {name!r}")
                with archive.open(member) as stream:
                    selected[name] = sha256_stream(stream)
    return finish(entries, selected, "zip")


def main() -> None:
    if len(sys.argv) < 2:
        fail("usage: inspect-generator-supply-archive.py ARCHIVE [MEMBER ...]")
    archive_path = os.path.realpath(sys.argv[1])
    requested = {normalized_path(value) for value in sys.argv[2:]}
    if zipfile.is_zipfile(archive_path):
        result = inspect_zip(archive_path, requested)
    elif tarfile.is_tarfile(archive_path):
        result = inspect_tar(archive_path, requested)
    else:
        fail(f"unsupported archive format: {archive_path}")
    found = {entry["path"] for entry in result["selectedMembers"]}
    missing = sorted(requested - found)
    if missing:
        fail(f"selected archive members are absent: {missing!r}")
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    main()
