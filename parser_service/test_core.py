import hashlib
import unittest
import uuid
from pathlib import Path

from parser_service.core import ParseRequest, ParserError, Policy, fetch_url, parse_file


class ParserCoreTest(unittest.TestCase):
    def temporary_directory(self):
        directory = Path(__file__).parent / ".test-work" / uuid.uuid4().hex
        directory.mkdir(parents=True, exist_ok=True)
        return _DirectoryContext(directory)

    def make_request(self, root: Path, object_id: str, filename: str, content: bytes) -> ParseRequest:
        (root / object_id).write_bytes(content)
        digest = hashlib.sha256(content).hexdigest()
        return ParseRequest("v1", "job-1", "key-1", object_id, "source-1", digest, filename)

    def test_markdown_result_is_canonical_and_untrusted(self):
        with self.temporary_directory() as directory:
            root = Path(directory)
            request = self.make_request(root, "object-1", "note.md", b"# Note\r\n\r\nBody")
            result = parse_file(request, root)
            self.assertEqual(result["detected_format"], "md")
            self.assertTrue(result["canonical_markdown"].endswith("\n"))
            self.assertTrue(result["untrusted_source"])

    def test_csv_becomes_markdown_table(self):
        with self.temporary_directory() as directory:
            root = Path(directory)
            request = self.make_request(root, "object-1", "table.csv", b"name,value\na,1\n")
            result = parse_file(request, root)
            self.assertIn("| name | value |", result["canonical_markdown"])

    def test_staging_hash_mismatch_is_rejected(self):
        with self.temporary_directory() as directory:
            root = Path(directory)
            request = self.make_request(root, "object-1", "note.md", b"hello")
            request.content_hash = "wrong"
            with self.assertRaises(ParserError) as raised:
                parse_file(request, root)
            self.assertEqual(raised.exception.code, "source_changed")

    def test_url_scheme_is_bounded(self):
        with self.assertRaises(ParserError) as raised:
            fetch_url("file:///etc/passwd", Policy())
        self.assertEqual(raised.exception.code, "invalid_url")


if __name__ == "__main__":
    unittest.main()


class _DirectoryContext:
    def __init__(self, path: Path):
        self.path = path

    def __enter__(self):
        return str(self.path)

    def __exit__(self, exc_type, exc_value, traceback):
        for child in self.path.glob("*"):
            child.unlink(missing_ok=True)
        self.path.rmdir()
        return False
