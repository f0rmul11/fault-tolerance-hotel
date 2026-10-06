FROM golang:1.26.1-alpine AS builder
WORKDIR /app
ENV GOWORK=off
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /search ./cmd

FROM scratch AS release
WORKDIR /app
COPY --from=builder /search /search
COPY config/config.yaml /app/config/config.yaml
USER 65532:65532
EXPOSE 8081 9090
ENTRYPOINT ["/search"]
