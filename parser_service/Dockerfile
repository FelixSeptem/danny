ARG PYTHON_BASE_IMAGE=docker.1ms.run/library/python:3.13-slim
FROM ${PYTHON_BASE_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    DANNY_PARSER_STAGING_ROOT=/var/lib/danny/staging \
    DANNY_PARSER_JOBS_ROOT=/var/lib/danny/parser-jobs

WORKDIR /opt/danny-parser
COPY parser_service/pyproject.toml ./
COPY parser_service ./parser_service
RUN pip install --no-cache-dir .

RUN mkdir -p /var/lib/danny/staging /var/lib/danny/parser-jobs \
    && chmod 700 /var/lib/danny/staging /var/lib/danny/parser-jobs

EXPOSE 8090
ENTRYPOINT ["danny-parser-http"]
CMD ["--host", "0.0.0.0", "--port", "8090", "--staging-root", "/var/lib/danny/staging", "--jobs-root", "/var/lib/danny/parser-jobs"]
