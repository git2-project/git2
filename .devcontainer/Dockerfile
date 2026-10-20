FROM golang:1.26-bookworm

# psql for `make migrate`, redis-cli for the `make run` preflight.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    make \
    ca-certificates \
    postgresql-client \
    redis-tools \
    && rm -rf /var/lib/apt/lists/*

# Pinned: gopls v0.24+ needs Go 1.27. Bump with the base image.
RUN go install golang.org/x/tools/gopls@v0.23.0 \
    && go install golang.org/x/tools/cmd/goimports@v0.52.0

WORKDIR /workspace
