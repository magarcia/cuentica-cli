import contextlib
import hashlib
import importlib.machinery
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile


loader = importlib.machinery.SourceFileLoader("release_build", str(Path(__file__).with_name("build")))
spec = importlib.util.spec_from_loader(loader.name, loader)
build = importlib.util.module_from_spec(spec)
loader.exec_module(build)
VALUES = {"Version": "v1.2.3", "Commit": "abc123", "BuildDate": "2026-10-17T00:00:00Z"}


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.enterContext(patch.object(build, "ROOT", self.root))
        self.enterContext(patch.dict(os.environ, {"SOURCE_DATE_EPOCH": "1234567890"}))
        self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        for name in build.DOCUMENTS:
            (self.root / name).write_text(f"Contents of {name}\n", encoding="utf-8")

    def release(self):
        def fake_build(destination, values, platform):
            destination.write_bytes(f"{platform[0]}/{platform[1]}".encode())

        with patch.object(build, "build", side_effect=fake_build) as command:
            build.release(VALUES)
        return command

    def checksums(self):
        destination = self.root / "dist"
        (destination / "checksums.txt").write_text(
            "".join(
                f"{hashlib.sha256((destination / name).read_bytes()).hexdigest()}  {name}\n"
                for name in sorted(build.archive_names(VALUES).values())
            ),
            encoding="utf-8",
        )

    def test_release_contents_checksums_and_ownership(self):
        command = self.release()
        self.assertEqual(command.call_count, 6)
        for platform, name in build.archive_names(VALUES).items():
            archive = self.root / "dist" / name
            self.assertEqual(build.read_binary(archive, platform[0]), f"{platform[0]}/{platform[1]}".encode())
            if name.endswith(".tar.gz"):
                with tarfile.open(archive) as source:
                    for member in source.getmembers():
                        self.assertEqual((member.uid, member.gid, member.uname, member.gname), (0, 0, "", ""))
                        self.assertEqual(member.mtime, 1234567890)
            else:
                with zipfile.ZipFile(archive) as source:
                    for member in source.infolist():
                        self.assertTrue(stat.S_ISREG(member.external_attr >> 16))
                        self.assertEqual(member.date_time, (2009, 2, 13, 23, 31, 30))
        with patch.object(build, "verify_binary") as inspect:
            build.verify(VALUES)
        self.assertEqual(inspect.call_count, 6)

    def test_release_is_reproducible_despite_source_metadata(self):
        self.release()
        before = {path.name: path.read_bytes() for path in (self.root / "dist").iterdir()}
        for name in build.DOCUMENTS:
            os.utime(self.root / name, (1, 1))
            (self.root / name).chmod(0o600)
        self.release()
        self.assertEqual(before, {path.name: path.read_bytes() for path in (self.root / "dist").iterdir()})

    def test_missing_document_fails_before_compilation(self):
        (self.root / "LICENSE").unlink()
        with patch.object(build, "build") as command:
            with self.assertRaisesRegex(ValueError, "LICENSE"):
                build.release(VALUES)
        command.assert_not_called()
        self.assertFalse((self.root / "dist").exists())

    def test_stale_assets_fail_without_removing_user_files(self):
        destination = self.root / "dist"
        destination.mkdir()
        unrelated = destination / "notes.txt"
        unrelated.write_text("keep me", encoding="utf-8")
        with patch.object(build, "build") as command:
            with self.assertRaisesRegex(ValueError, "unexpected files"):
                build.release(VALUES)
        command.assert_not_called()
        self.assertEqual(unrelated.read_text(encoding="utf-8"), "keep me")

    def test_build_failure_preserves_previous_complete_release(self):
        self.release()
        before = {path.name: path.read_bytes() for path in (self.root / "dist").iterdir()}
        with patch.object(build, "build", side_effect=subprocess.CalledProcessError(1, "go")):
            with self.assertRaises(subprocess.CalledProcessError):
                build.release(VALUES)
        self.assertEqual(before, {path.name: path.read_bytes() for path in (self.root / "dist").iterdir()})

    def test_verification_rejects_modified_archive(self):
        self.release()
        archive = self.root / "dist" / next(iter(build.archive_names(VALUES).values()))
        archive.write_bytes(archive.read_bytes() + b"tampered")
        with self.assertRaisesRegex(ValueError, "Checksum mismatch"):
            build.verify(VALUES)

    def test_verification_requires_all_archives_and_exact_checksums(self):
        self.release()
        checksums = self.root / "dist" / "checksums.txt"
        checksums.write_text(checksums.read_text(encoding="utf-8") * 2, encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "duplicate"):
            build.verify(VALUES)
        self.checksums()
        next((self.root / "dist").glob("*.zip")).unlink()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            build.verify(VALUES)

    def test_verification_rejects_path_traversal_without_extracting(self):
        self.release()
        archive = self.root / "dist" / build.archive_names(VALUES)[("windows", "amd64")]
        with zipfile.ZipFile(archive, "a") as output:
            output.writestr("../escape", "unsafe")
        self.checksums()
        with patch.object(build, "verify_binary"):
            with self.assertRaisesRegex(ValueError, "Unexpected archive members"):
                build.verify(VALUES)
        self.assertFalse((self.root / "escape").exists())

    def test_verification_rejects_links_and_nonexecutable_unix_binary(self):
        self.release()
        archive = self.root / "dist" / build.archive_names(VALUES)[("linux", "amd64")]
        for mode, member_type in ((0o644, tarfile.REGTYPE), (0o755, tarfile.SYMTYPE)):
            with self.subTest(mode=mode, member_type=member_type):
                with tarfile.open(archive, "w:gz") as output:
                    for name in ["cuentica", *build.DOCUMENTS]:
                        member = tarfile.TarInfo(name)
                        member.mode = mode if name == "cuentica" else 0o644
                        member.type = member_type if name == "cuentica" else tarfile.REGTYPE
                        output.addfile(member)
                with self.assertRaisesRegex(ValueError, "type or mode"):
                    build.read_binary(archive, "linux")

    def test_verification_rejects_zip_symlinks(self):
        archive = self.root / "unsafe.zip"
        with zipfile.ZipFile(archive, "w") as output:
            for name in ["cuentica.exe", *build.DOCUMENTS]:
                member = zipfile.ZipInfo(name)
                member.create_system = 3
                member.external_attr = (
                    stat.S_IFLNK | 0o755 if name == "cuentica.exe" else stat.S_IFREG | 0o644
                ) << 16
                output.writestr(member, b"unsafe")
        with self.assertRaisesRegex(ValueError, "type or mode"):
            build.read_binary(archive, "windows")

    def test_native_smoke_runs_only_matching_platform(self):
        self.release()
        with patch.object(build, "go_env", side_effect=["windows", "arm64"]):
            with patch.object(build, "verify_binary") as inspect:
                with patch.object(build, "smoke") as smoke:
                    build.verify(VALUES, run=True)
        self.assertEqual(inspect.call_count, 6)
        smoke.assert_called_once()
        self.assertEqual(smoke.call_args.args[0].name, "cuentica.exe")

    def test_binary_metadata_rejects_wrong_target(self):
        output = "binary: go1.25\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=amd64\n\tbuild\tCGO_ENABLED=0\n\tbuild\t-trimpath=true\n"
        with patch.object(build.subprocess, "check_output", return_value=output):
            build.verify_binary(Path("binary"), ("linux", "amd64"))
            with self.assertRaisesRegex(ValueError, "GOARCH"):
                build.verify_binary(Path("binary"), ("linux", "arm64"))

    def test_native_smoke_removes_credentials_and_checks_output(self):
        responses = [
            f"cuentica {VALUES['Version']} (commit {VALUES['Commit']}, built {VALUES['BuildDate']})\n",
            "Usage: cuentica\n",
            json.dumps([{"resource": "company", "action": "get"}]),
            json.dumps({"method": "GET", "path": "/company"}),
        ]
        def invoke(*args, **kwargs):
            self.assertNotIn("CUENTICA_API_KEY", {key.upper() for key in kwargs["env"]})
            return subprocess.CompletedProcess(args[0], 0, responses.pop(0), "")

        with patch.dict(os.environ, {"CUENTICA_API_KEY": "test-sentinel"}):
            with patch.object(build.subprocess, "run", side_effect=invoke) as command:
                build.smoke(Path("cuentica"), VALUES)
        self.assertEqual(command.call_count, 4)
        self.assertEqual(responses, [])

    def test_source_epoch_is_validated(self):
        for value in ("-1", "tomorrow", "4294967296"):
            with self.subTest(value=value), patch.dict(os.environ, {"SOURCE_DATE_EPOCH": value}):
                with self.assertRaisesRegex(ValueError, "SOURCE_DATE_EPOCH"):
                    build.source_epoch()

    def test_dist_symlink_is_rejected(self):
        if os.name == "nt":
            self.skipTest("Creating symlinks requires Windows developer mode or administrator rights")
        (self.root / "dist").symlink_to(self.root, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symlink"):
            build.release(VALUES)


if __name__ == "__main__":
    unittest.main()
