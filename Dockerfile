FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/prs ./cmd/prs
COPY internal ./internal
COPY wails.json ./
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    version="$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' wails.json)" && \
    test -n "$version" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$version" -o /out/prs ./cmd/prs && \
    mkdir -p /out/tmp && chmod 1777 /out/tmp

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/tmp /tmp
COPY --from=build /out/prs /usr/local/bin/prs
COPY examples /examples
COPY THIRD-PARTY.txt /THIRD-PARTY.txt
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/prs"]
CMD ["help"]
