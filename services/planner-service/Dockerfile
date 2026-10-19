FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /planner-service ./cmd/planner-service

FROM alpine:3.22
RUN apk add --no-cache tzdata ca-certificates wget && adduser -D -u 10001 app
USER app
COPY --from=build /planner-service /planner-service
EXPOSE 8082
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://localhost:8082/health || exit 1
ENTRYPOINT ["/planner-service"]
