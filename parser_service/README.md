# Danny parser service

The parser core follows the OpenKB conversion boundary and emits canonical Markdown with provenance metadata. The production adapter is `danny-parser-http`; the local and fixture adapter is `danny-parser`.

The service does not require Pandoc, LibreOffice, Tesseract, or `pdftotext`. Office/PDF support is supplied by pinned Python wheels in `pyproject.toml`.
