# API, worker and datactl in one image (cgo is needed for h3-go).
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* \
 && useradd -r -u 10001 pagdandi
COPY --from=build /out/ /app/
USER pagdandi
EXPOSE 8080
CMD ["/app/api"]
