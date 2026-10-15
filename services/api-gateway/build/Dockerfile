FROM golang:1.26.1-alpine AS builder
WORKDIR /app
ENV GOWORK=off
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /api-gateway ./cmd

FROM scratch AS release
WORKDIR /app
COPY --from=builder /api-gateway /api-gateway
COPY config/config.yaml /app/config/config.yaml
COPY config/rtconfig.yaml /app/config/rtconfig.yaml
USER 65532:65532
EXPOSE 8080 8081 9090
ENTRYPOINT ["/api-gateway"]
