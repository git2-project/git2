FROM node:22-bookworm-slim AS node

FROM golang:1.26-bookworm

# Node 22 for the Vite toolchain. Debian's own nodejs package is too old for
# Vite, so the binaries are copied from the official image instead.
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

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
