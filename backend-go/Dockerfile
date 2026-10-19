# syntax=docker/dockerfile:1
# Multi-stage build → distroless static, nonroot, no shell (OPERATIONS §7).

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Version=${VERSION} -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/posd ./cmd/posd && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Version=${VERSION} -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/posctl ./cmd/posctl

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/posd /out/posctl /usr/local/bin/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/posd"]
