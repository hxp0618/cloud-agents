#!/usr/bin/env python3

from __future__ import annotations

import io
import json
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path


INSPECTOR = Path(__file__).with_name("inspect-generator-supply-archive.py")


class ArchiveInspectorTest(unittest.TestCase):
    def inspect(self, members: list[tarfile.TarInfo], selected: str | None = None) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as directory:
            archive_path = Path(directory) / "fixture.tar"
            with tarfile.open(archive_path, "w") as archive:
                for member in members:
                    body = io.BytesIO(b"bound-bytes") if member.isreg() else None
                    archive.addfile(member, body)
            arguments = [sys.executable, str(INSPECTOR), str(archive_path)]
            if selected is not None:
                arguments.append(selected)
            return subprocess.run(arguments, text=True, capture_output=True, check=False)

    @staticmethod
    def regular(name: str) -> tarfile.TarInfo:
        member = tarfile.TarInfo(name)
        member.size = len(b"bound-bytes")
        return member

    @staticmethod
    def symlink(name: str, target: str) -> tarfile.TarInfo:
        member = tarfile.TarInfo(name)
        member.type = tarfile.SYMTYPE
        member.linkname = target
        return member

    def test_accepts_contained_link_to_regular_file_and_selected_bytes(self) -> None:
        result = self.inspect(
            [self.regular("bin/tool.real"), self.symlink("bin/tool", "tool.real")],
            "bin/tool.real",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        evidence = json.loads(result.stdout)
        self.assertTrue(evidence["linkTargetsResolveToRegularFiles"])
        self.assertEqual(evidence["selectedMembers"][0]["sizeBytes"], len(b"bound-bytes"))

    def test_rejects_absolute_parent_and_duplicate_paths(self) -> None:
        for members in (
            [self.regular("/absolute")],
            [self.regular("../escape")],
            [self.regular("same"), self.regular("same")],
        ):
            with self.subTest(members=[member.name for member in members]):
                self.assertNotEqual(self.inspect(members).returncode, 0)

    def test_rejects_escaping_missing_cyclic_and_nonregular_link_targets(self) -> None:
        fixtures = (
            [self.regular("bin/tool"), self.symlink("bin/link", "../../escape")],
            [self.symlink("bin/link", "missing")],
            [self.symlink("a", "b"), self.symlink("b", "a")],
            [self.symlink("link", "directory"), self.directory("directory")],
        )
        for members in fixtures:
            with self.subTest(members=[member.name for member in members]):
                self.assertNotEqual(self.inspect(list(members)).returncode, 0)

    def test_rejects_descendants_under_links_and_special_files(self) -> None:
        descendant = [
            self.regular("target"),
            self.symlink("alias", "target"),
            self.regular("alias/descendant"),
        ]
        fifo = tarfile.TarInfo("fifo")
        fifo.type = tarfile.FIFOTYPE
        self.assertNotEqual(self.inspect(descendant).returncode, 0)
        self.assertNotEqual(self.inspect([fifo]).returncode, 0)

    @staticmethod
    def directory(name: str) -> tarfile.TarInfo:
        member = tarfile.TarInfo(name)
        member.type = tarfile.DIRTYPE
        return member


if __name__ == "__main__":
    unittest.main()
