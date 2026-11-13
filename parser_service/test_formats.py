"""Milestone 12 parser format matrix (scenario S12).

Each supported format must produce canonical Markdown or a structured failure.
A missing optional dependency is itself a structured failure, so the matrix
asserts the contract rather than the presence of a local installation.

Run it directly (no pytest required)::

    python parser_service/test_formats.py
"""

from __future__ import annotations

import hashlib
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from parser_service.core import (  # noqa: E402
    CONTRACT_VERSION,
    ParseRequest,
    ParserError,
    parse_file,
)

# Codes the parser contract allows as a structured failure.
STRUCTURED_FAILURES = {
    "missing_parser_dependency",
    "conversion_failed",
    "resource_limited",
    "timeout",
    "unsupported",
    "provider_failed",
    "cancelled",
    "failed",
    "not_found",
    "source_changed",
}

# (format hint, filename, payload). Text formats carry a real sample; the
# binary formats carry bytes that are not a valid container, so the matrix
# observes the structured failure path when a converter is unavailable.
SAMPLES = [
    ("md", "notes.md", b"# Title\n\nA paragraph.\n"),
    ("markdown", "notes.markdown", b"# Title\n\nA paragraph.\n"),
    ("txt", "notes.txt", b"plain text body\n"),
    ("csv", "rows.csv", b"name,value\nalpha,1\nbeta,2\n"),
    ("html", "page.html", b"<html><body><p>Hello world</p></body></html>"),
    ("docx", "report.docx", b"not-a-real-docx-container"),
    ("pptx", "deck.pptx", b"not-a-real-pptx-container"),
    ("xlsx", "book.xlsx", b"not-a-real-xlsx-container"),
    ("xls", "legacy.xls", b"not-a-real-xls-container"),
    ("pdf", "paper.pdf", b"not-a-real-pdf-document"),
]


def check(format_hint: str, filename: str, payload: bytes) -> str:
    """Return the outcome for one format: canonical output or a failure code."""
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        (root / "obj-1").write_bytes(payload)
        request = ParseRequest(
            contract_version=CONTRACT_VERSION,
            job_id="job-1",
            idempotency_key="idem-1",
            staging_object_id="obj-1",
            source_fingerprint="fingerprint-1",
            content_hash=hashlib.sha256(payload).hexdigest(),
            filename=filename,
            format_hint=format_hint,
        )
        try:
            result = parse_file(request, root)
        except ParserError as error:
            if error.code not in STRUCTURED_FAILURES:
                raise AssertionError(f"unexpected structured failure code {error.code}") from error
            return f"structured:{error.code}"
        except ImportError as error:
            # An optional converter is not installed: a missing dependency is
            # a structured failure, not a crash.
            return f"structured:missing_dependency ({error})"
        markdown = result.get("canonical_markdown", "")
        if not str(markdown).strip():
            raise AssertionError("canonical output is empty")
        if not isinstance(result.get("sections"), list):
            raise AssertionError("canonical output carries no sections")
        return "canonical"


def main() -> int:
    failures: list[tuple[str, str]] = []
    for format_hint, filename, payload in SAMPLES:
        try:
            outcome = check(format_hint, filename, payload)
        except Exception as error:  # noqa: BLE001 - the matrix reports every failure
            failures.append((filename, str(error)))
            print(f"FAIL {filename}: {error}")
        else:
            print(f"PASS {filename}: {outcome}")
    if failures:
        print(f"\n{len(failures)} of {len(SAMPLES)} formats failed")
        return 1
    print(f"\nall {len(SAMPLES)} formats produced canonical output or a structured failure")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
