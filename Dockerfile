# Every build resolves the newest compatible dependencies at least a week old (build.sh passes a fresh
# DEPS_REFRESH, so this stage always runs): the build below only runs again when that changes
# go.mod/go.sum, or when a new golang:alpine comes with --pull.
FROM 127.0.0.1:5000/go-tools-build:latest AS deps
WORKDIR /src
COPY go.mod go.sum ./
COPY *.go ./
ARG DEPS_REFRESH
RUN go-deps update

# With go-deps vulncheck: no image with a known vulnerability.
FROM 127.0.0.1:5000/go-tools-build:latest AS builder
WORKDIR /src
COPY --from=deps /src/go.mod /src/go.sum ./
RUN go mod download
COPY *.go ./
COPY testdata ./testdata
RUN CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./... \
    && go-deps vulncheck ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /telerpc .

# Just the static binary and the CA bundle (maintain and export download the TL schemas from
# GitHub): no shell, no other programs.
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /telerpc /telerpc
USER 10110:10110
ENTRYPOINT ["/telerpc"]
