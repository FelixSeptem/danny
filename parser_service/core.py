from __future__ import annotations

import csv
import hashlib
import html
import json
import os
import re
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

CONTRACT_VERSION = "v1"
SUPPORTED_FORMATS = {"md", "markdown", "txt", "csv", "html", "htm", "docx", "pptx", "xlsx", "xls", "pdf"}


class ParserError(Exception):
    def __init__(self, code: str, message: str, retryable: bool = False):
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable


@dataclass
class Policy:
    pdf_page_threshold: int = 20
    allow_pageindex_cloud: bool = False
    timeout_seconds: int = 300
    max_bytes: int = 50 * 1024 * 1024
    max_pages: int = 1000


@dataclass
class ParseRequest:
    contract_version: str
    job_id: str
    idempotency_key: str
    staging_object_id: str
    source_fingerprint: str
    content_hash: str
    filename: str
    format_hint: str = ""
    policy: Policy = field(default_factory=Policy)

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "ParseRequest":
        policy = payload.get("policy") or {}
        return cls(
            contract_version=payload.get("contract_version", ""),
            job_id=payload.get("job_id", ""),
            idempotency_key=payload.get("idempotency_key", ""),
            staging_object_id=payload.get("staging_object_id", ""),
            source_fingerprint=payload.get("source_fingerprint", ""),
            content_hash=payload.get("content_hash", ""),
            filename=payload.get("filename", ""),
            format_hint=payload.get("format_hint", ""),
            policy=Policy(**{k: policy[k] for k in Policy.__dataclass_fields__ if k in policy}),
        )

    def validate(self) -> None:
        if self.contract_version != CONTRACT_VERSION:
            raise ParserError("unsupported_version", "unsupported parser contract version")
        required = (self.job_id, self.idempotency_key, self.staging_object_id, self.source_fingerprint, self.content_hash, self.filename)
        if any(not str(value).strip() for value in required):
            raise ParserError("invalid_request", "required parser request fields are missing")
        if "/" in self.staging_object_id or "\\" in self.staging_object_id or ".." in self.staging_object_id:
            raise ParserError("invalid_request", "staging_object_id must be opaque")
        if any(value < 0 for value in (self.policy.max_bytes, self.policy.max_pages, self.policy.pdf_page_threshold, self.policy.timeout_seconds)):
            raise ParserError("invalid_request", "parser limits must not be negative")


def _hash_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _format_for(filename: str, hint: str = "") -> str:
    value = hint.strip().lower().lstrip(".") or Path(filename).suffix.lower().lstrip(".")
    if value == "markdown":
        return "md"
    return value


def _markdown_sections(markdown: str) -> list[dict[str, Any]]:
    sections: list[dict[str, Any]] = []
    pattern = re.compile(r"^(#{1,6})[ \t]+(.+?)\s*$")
    lines = markdown.splitlines()
    for index, line in enumerate(lines, start=1):
        match = pattern.match(line)
        if match:
            sections.append({"title": match.group(2).strip(), "start_line": index, "end_line": len(lines)})
    for index, section in enumerate(sections):
        for candidate in sections[index + 1 :]:
            if candidate["title"] and candidate["start_line"] > section["start_line"]:
                section["end_line"] = candidate["start_line"] - 1
                break
    return sections


def _plain_to_markdown(data: bytes) -> str:
    text = data.decode("utf-8", errors="replace").replace("\r\n", "\n").replace("\r", "\n")
    return text if text.endswith("\n") else text + "\n"


def _csv_to_markdown(data: bytes) -> str:
    text = data.decode("utf-8", errors="replace")
    rows = list(csv.reader(text.splitlines()))
    if not rows:
        return "\n"
    width = max(len(row) for row in rows)
    rows = [row + [""] * (width - len(row)) for row in rows]
    output = ["| " + " | ".join(rows[0]) + " |", "| " + " | ".join("---" for _ in range(width)) + " |"]
    output.extend("| " + " | ".join(row) + " |" for row in rows[1:])
    return "\n".join(output) + "\n"


def _html_to_markdown(data: bytes) -> str:
    try:
        import trafilatura

        extracted = trafilatura.extract(data.decode("utf-8", errors="replace"), output_format="markdown", include_links=True)
        if extracted:
            return extracted.rstrip() + "\n"
    except ImportError:
        pass
    text = re.sub(r"<script\b[^>]*>.*?</script>", " ", data.decode("utf-8", errors="replace"), flags=re.I | re.S)
    text = re.sub(r"<style\b[^>]*>.*?</style>", " ", text, flags=re.I | re.S)
    text = re.sub(r"<[^>]+>", " ", text)
    return html.unescape(re.sub(r"\s+", " ", text)).strip() + "\n"


def _markitdown(path: Path) -> str:
    try:
        from markitdown import MarkItDown
    except ImportError as exc:
        raise ParserError("missing_parser_dependency", "markitdown extras are not installed") from exc
    try:
        result = MarkItDown().convert(str(path), keep_data_uris=True)
    except Exception as exc:  # adapter boundary: redact implementation details
        raise ParserError("conversion_failed", f"document conversion failed: {type(exc).__name__}", retryable=False) from exc
    return result.text_content.rstrip() + "\n"


def _pdf(path: Path, policy: Policy) -> tuple[str, int, str, list[dict[str, Any]]]:
    try:
        import pymupdf
    except ImportError as exc:
        raise ParserError("missing_parser_dependency", "pymupdf is not installed") from exc
    try:
        document = pymupdf.open(str(path))
    except Exception as exc:
        raise ParserError("conversion_failed", "PDF could not be opened") from exc
    with document:
        pages = document.page_count
        if policy.max_pages and pages > policy.max_pages:
            raise ParserError("resource_limited", "PDF page limit exceeded")
        chunks = []
        for index, page in enumerate(document, start=1):
            text = page.get_text("text").strip()
            if text:
                chunks.append(f"<!-- page: {index} -->\n{text}")
        markdown = "\n\n".join(chunks).rstrip() + "\n"
    provider = "local"
    warnings: list[dict[str, Any]] = []
    if pages >= policy.pdf_page_threshold and policy.allow_pageindex_cloud and os.getenv("PAGEINDEX_API_KEY"):
        try:
            cloud_markdown = _pageindex_cloud(path, pages)
            if cloud_markdown:
                markdown = cloud_markdown
                provider = "pageindex_cloud"
            else:
                warnings.append({"code": "pageindex_cloud_empty", "message": "cloud provider returned no page content; local extraction was used"})
        except Exception as exc:
            warnings.append({"code": "pageindex_cloud_failed", "message": f"cloud provider failed with {type(exc).__name__}; local extraction was used"})
    return markdown, pages, provider, warnings


def _pageindex_cloud(path: Path, pages: int) -> str:
    from pageindex import PageIndexClient

    client = PageIndexClient(api_key=os.environ["PAGEINDEX_API_KEY"])
    collection = client.collection()
    document_id = collection.add(str(path))
    page_document = collection.get_document(document_id, include_text=True)
    page_count = int(page_document.get("page_count", pages) or pages)
    content = collection.get_page_content(document_id, f"1-{page_count}")
    if isinstance(content, str):
        return content.rstrip() + "\n"
    if isinstance(content, list):
        parts = []
        for page in content:
            if isinstance(page, dict):
                value = page.get("content") or page.get("text") or ""
            else:
                value = str(page)
            if value:
                parts.append(value.strip())
        return "\n\n---\n\n".join(parts).rstrip() + "\n" if parts else ""
    return ""


def parse_file(request: ParseRequest, staging_root: Path) -> dict[str, Any]:
    request.validate()
    path = (staging_root / request.staging_object_id).resolve()
    root = staging_root.resolve()
    if not path.is_file() or root not in path.parents:
        raise ParserError("not_found", "staging object was not found")
    size = path.stat().st_size
    if request.policy.max_bytes and size > request.policy.max_bytes:
        raise ParserError("resource_limited", "input byte limit exceeded")
    raw = path.read_bytes()
    actual_hash = _hash_bytes(raw)
    if request.content_hash and actual_hash != request.content_hash:
        raise ParserError("source_changed", "staging object content hash does not match request")
    detected = _format_for(request.filename, request.format_hint)
    if detected not in SUPPORTED_FORMATS:
        raise ParserError("unsupported", f"format {detected or 'unknown'} is not supported")
    pages = 0
    provider = "local"
    warnings: list[dict[str, Any]] = []
    if detected in {"md", "markdown", "txt"}:
        markdown = _plain_to_markdown(raw)
    elif detected == "csv":
        markdown = _csv_to_markdown(raw)
    elif detected in {"html", "htm"}:
        markdown = _html_to_markdown(raw)
    elif detected == "pdf":
        markdown, pages, provider, warnings = _pdf(path, request.policy)
    else:
        markdown = _markitdown(path)
    return {
        "job_id": request.job_id,
        "contract_version": CONTRACT_VERSION,
        "source_fingerprint": request.source_fingerprint,
        "content_hash": actual_hash,
        "filename": request.filename,
        "detected_format": "md" if detected == "markdown" else detected,
        "parser_version": "danny-parser-0.1.0",
        "provider_mode": provider,
        "canonical_markdown": markdown,
        "pages": pages,
        "sections": _markdown_sections(markdown),
        "images": [],
        "warnings": warnings,
        "untrusted_source": True,
    }


def fetch_url(url: str, policy: Policy) -> tuple[bytes, str, str]:
    parsed = urlparse(url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ParserError("invalid_url", "only http and https URLs are supported")
    request = urllib.request.Request(url, headers={"User-Agent": "danny-parser/0.1"})
    try:
        response = urllib.request.urlopen(request, timeout=max(policy.timeout_seconds, 1))
    except (urllib.error.URLError, urllib.error.HTTPError) as exc:
        raise ParserError("url_fetch_failed", "URL fetch failed", retryable=True) from exc
    with response:
        declared = response.headers.get_content_type().lower()
        content_length = response.headers.get("Content-Length")
        if content_length and policy.max_bytes and int(content_length) > policy.max_bytes:
            raise ParserError("resource_limited", "URL response byte limit exceeded")
        chunks: list[bytes] = []
        total = 0
        while True:
            chunk = response.read(64 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if policy.max_bytes and total > policy.max_bytes:
                raise ParserError("resource_limited", "URL response byte limit exceeded")
            chunks.append(chunk)
    content = b"".join(chunks)
    if content.startswith(b"%PDF-") or declared == "application/pdf":
        suffix = ".pdf"
    elif content.lstrip().startswith(b"<") or declared in {"text/html", "application/xhtml+xml"}:
        suffix = ".html"
    else:
        raise ParserError("unsupported", "URL content type is not PDF or HTML")
    filename = Path(parsed.path).name or "document"
    if not filename.lower().endswith(suffix):
        filename += suffix
    return content, filename, suffix.lstrip(".")


def status_payload(job_id: str, status: str, result: dict[str, Any] | None = None, failure: dict[str, Any] | None = None) -> dict[str, Any]:
    return {"job_id": job_id, "contract_version": CONTRACT_VERSION, "status": status, "result": result, "failure": failure}


def safe_failure(exc: Exception) -> dict[str, Any]:
    if isinstance(exc, ParserError):
        return {"code": exc.code, "message": exc.message, "retryable": exc.retryable}
    return {"code": "failed", "message": type(exc).__name__, "retryable": False}
