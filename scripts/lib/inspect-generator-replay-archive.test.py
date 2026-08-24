#!/usr/bin/env python3
"""Focused adversarial tests for generator replay archive inspection."""

from __future__ import annotations

import io
import json
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("inspect-generator-replay-archive.py")


def tar_entry(
    name: str,
    *,
    kind: bytes = tarfile.REGTYPE,
    body: bytes = b"",
    mode: int = 0o644,
    linkname: str = "",
) -> tuple[tarfile.TarInfo, io.BytesIO | None]:
    entry = tarfile.TarInfo(name)
    entry.type = kind
    entry.mode = mode
    entry.uid = 0
    entry.gid = 0
    entry.mtime = 0
    entry.linkname = linkname
    if kind == tarfile.REGTYPE:
        entry.size = len(body)
        return entry, io.BytesIO(body)
    return entry, None


def write_tar(path: Path, entries: list[tuple[tarfile.TarInfo, io.BytesIO | None]]) -> None:
    with tarfile.open(path, "w", format=tarfile.PAX_FORMAT) as archive:
        for entry, stream in entries:
            archive.addfile(entry, stream)


class ArchiveInspectorTest(unittest.TestCase):
    def inspect(self, profile: str, archive: Path, *, success: bool) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            [str(SCRIPT), profile, str(archive)],
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode == 0, success, result.stderr)
        return result

    def test_core_projection_reconstructs_exact_git_tree(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            subprocess.run(["/usr/bin/git", "init", "-q", str(root)], check=True)
            (root / "nested").mkdir()
            (root / "plain.txt").write_text("plain\n", encoding="utf-8")
            (root / "nested.item").write_text("tree ordering sentinel\n", encoding="utf-8")
            (root / "zeta").write_text("zeta\n", encoding="utf-8")
            (root / "é.txt").write_text("unicode byte ordering\n", encoding="utf-8")
            executable = root / "nested" / "tool.sh"
            executable.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            executable.chmod(0o755)
            subprocess.run(
                ["/usr/bin/git", "add", "plain.txt", "nested/tool.sh", "nested.item", "zeta", "é.txt"],
                cwd=root,
                check=True,
            )
            tree = subprocess.run(
                ["/usr/bin/git", "write-tree"], cwd=root, check=True, capture_output=True, text=True
            ).stdout.strip()
            archive = root / "projection.tar"
            subprocess.run(
                [
                    "/usr/bin/git",
                    "-c",
                    "tar.umask=0022",
                    "archive",
                    "--format=tar",
                    "--mtime=1970-01-01T00:00:00Z",
                    f"--output={archive}",
                    tree,
                ],
                cwd=root,
                check=True,
            )
            result = self.inspect("core-projection", archive, success=True)
            document = json.loads(result.stdout)
            self.assertEqual(document["reconstructedGitTreeSha"], tree)
            self.assertEqual(document["regularFiles"], 5)
            self.assertEqual(document["directories"], 1)
            self.assertEqual(document["unsafeEntries"], 0)

    def test_core_projection_rejects_noncanonical_and_ambiguous_archives(self) -> None:
        cases = {
            "absolute": [tar_entry("/escape", body=b"x")],
            "parent": [tar_entry("../escape", body=b"x")],
            "duplicate": [tar_entry("same", body=b"a"), tar_entry("same", body=b"b")],
            "symlink": [tar_entry("link", kind=tarfile.SYMTYPE, linkname="target")],
            "special": [tar_entry("pipe", kind=tarfile.FIFOTYPE)],
            "wrong-mode": [tar_entry("file", body=b"x", mode=0o664)],
        }
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            for name, entries in cases.items():
                with self.subTest(name=name):
                    archive = root / f"{name}.tar"
                    write_tar(archive, entries)
                    self.inspect("core-projection", archive, success=False)

    def test_rootfs_accepts_chroot_absolute_symlink_and_safe_hardlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            archive = Path(temporary_directory) / "rootfs.tar"
            write_tar(
                archive,
                [
                    tar_entry("usr", kind=tarfile.DIRTYPE, mode=0o755),
                    tar_entry("usr/bin", kind=tarfile.DIRTYPE, mode=0o755),
                    tar_entry("usr/bin/target", body=b"target"),
                    tar_entry("bin", kind=tarfile.DIRTYPE, mode=0o755),
                    tar_entry("bin/tool", kind=tarfile.SYMTYPE, linkname="/usr/bin/target"),
                    tar_entry("bin/optional", kind=tarfile.SYMTYPE, linkname="/optional/absent"),
                    tar_entry("usr/bin/hard", kind=tarfile.LNKTYPE, linkname="usr/bin/target"),
                ],
            )
            result = self.inspect("rootfs", archive, success=True)
            document = json.loads(result.stdout)
            self.assertEqual(document["symlinks"], 2)
            self.assertEqual(document["hardlinks"], 1)
            self.assertEqual(document["specialEntries"], 0)

    def test_rootfs_rejects_escape_cycle_link_prefix_and_special(self) -> None:
        cases = {
            "escape": [tar_entry("link", kind=tarfile.SYMTYPE, linkname="../../escape")],
            "cycle": [
                tar_entry("a", kind=tarfile.SYMTYPE, linkname="b"),
                tar_entry("b", kind=tarfile.SYMTYPE, linkname="a"),
            ],
            "link-prefix": [
                tar_entry("target", body=b"target"),
                tar_entry("linked", kind=tarfile.SYMTYPE, linkname="target"),
                tar_entry("linked/child", body=b"child"),
            ],
            "file-prefix": [
                tar_entry("plain", body=b"plain"),
                tar_entry("plain/child", body=b"child"),
            ],
            "hardlink-prefix": [
                tar_entry("target", body=b"target"),
                tar_entry("linked", kind=tarfile.LNKTYPE, linkname="target"),
                tar_entry("linked/child", body=b"child"),
            ],
            "special": [tar_entry("device", kind=tarfile.CHRTYPE)],
            "dangling-hardlink": [
                tar_entry("hard", kind=tarfile.LNKTYPE, linkname="absent"),
            ],
            "absolute-hardlink": [
                tar_entry("hard", kind=tarfile.LNKTYPE, linkname="/etc/passwd"),
            ],
            "hardlink-to-symlink": [
                tar_entry("hard", kind=tarfile.LNKTYPE, linkname="link"),
                tar_entry("link", kind=tarfile.SYMTYPE, linkname="target"),
                tar_entry("target", body=b"target"),
            ],
            "reserved-mountpoint-link": [
                tar_entry("input", kind=tarfile.SYMTYPE, linkname="tmp"),
            ],
            "reserved-authority": [
                tar_entry("authority", kind=tarfile.DIRTYPE),
            ],
        }
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            for name, entries in cases.items():
                with self.subTest(name=name):
                    archive = root / f"{name}.tar"
                    write_tar(archive, entries)
                    self.inspect("rootfs", archive, success=False)

    def test_authorized_ubuntu_rootfs_tar_when_explicitly_provided(self) -> None:
        requested = os.environ.get("CLOUD_AGENTS_GENERATOR_TEST_ROOTFS_TAR")
        if requested is None:
            self.skipTest("exact disposable Ubuntu rootfs tar was not explicitly provided")
        archive = Path(requested)
        self.assertEqual(archive.stat().st_size, 80669696)
        result = self.inspect("rootfs", archive, success=True)
        document = json.loads(result.stdout)
        self.assertEqual(
            {
                "regularFiles": document["regularFiles"],
                "directories": document["directories"],
                "symlinks": document["symlinks"],
                "hardlinks": document["hardlinks"],
                "manifestSha256": document["manifestSha256"],
            },
            {
                "regularFiles": 2587,
                "directories": 661,
                "symlinks": 198,
                "hardlinks": 2,
                "manifestSha256": "b2f581777b04657540dffa9b4f6ba98e6e0d310ea11b100cd84e6fcf19ec4af6",
            },
        )


if __name__ == "__main__":
    unittest.main()
