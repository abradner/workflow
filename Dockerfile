FROM golang:1-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: modernc.org/sqlite (pulled in for the embedded Temporal dev
# server) is pure Go, so this stays a static binary with no libc dependency.
RUN CGO_ENABLED=0 go build -o /out/workflow ./cmd/workflow

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/workflow /usr/local/bin/workflow

ENTRYPOINT ["workflow"]
CMD ["--help"]
