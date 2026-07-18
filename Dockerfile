FROM golang:1-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: modernc.org/sqlite (pulled in for the embedded Temporal dev
# server) is pure Go, so this stays a static binary with no libc dependency.
RUN CGO_ENABLED=0 go build -o /out/workflow ./cmd/workflow

# sync-1p and render-talos activities shell out to the 1Password CLI, so the
# worker image needs it installed - not just our own binary. Installed from
# 1Password's official apt repo (see https://developer.1password.com/docs/cli/get-started/)
# in this Debian-based build stage, then copied into the minimal final image below.
FROM golang:1-bookworm AS op-cli
# Architecture is resolved at build time via dpkg, not hardcoded to amd64,
# so this stage works whether it's built on/for amd64 or arm64 (e.g. Apple
# Silicon Docker Desktop) - matching 1Password's own documented install
# command (see the link above).
RUN curl -sS https://downloads.1password.com/linux/keys/1password.asc \
        | gpg --dearmor --output /usr/share/keyrings/1password-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/1password-archive-keyring.gpg] https://downloads.1password.com/linux/debian/$(dpkg --print-architecture) stable main" \
        > /etc/apt/sources.list.d/1password.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends 1password-cli \
    && rm -rf /var/lib/apt/lists/*

# distroless/base (not /static): the op CLI isn't a pure static Go binary the
# way our own workflow binary is, so this needs glibc/libssl/ca-certificates.
FROM gcr.io/distroless/base-debian12

COPY --from=build /out/workflow /usr/local/bin/workflow
COPY --from=op-cli /usr/bin/op /usr/local/bin/op

ENTRYPOINT ["workflow"]
CMD ["--help"]
