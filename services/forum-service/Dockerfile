# Compilación
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/forum-service .

# Imagen final: sólo el binario, sin el código ni el compilador
FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/forum-service /usr/local/bin/forum-service
ENV PORT=8083
EXPOSE 8083
ENTRYPOINT ["/usr/local/bin/forum-service"]
